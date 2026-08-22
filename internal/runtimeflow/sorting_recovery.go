package runtimeflow

import (
	"context"
	"errors"
	"time"
)

type PrepareSortingRecoveryClaimTicket struct {
	RecordID string
	Version  int
}

func (s *Store) PrepareSortingRecoveryClaim(ctx context.Context, id string, now time.Time) (PrepareSortingRecoveryClaimTicket, error) {
	value, err := s.Get(ctx, id)
	if err != nil {
		return PrepareSortingRecoveryClaimTicket{}, err
	}
	if value.Kind != "sorting-recovery" || (value.State == "running" && value.LeaseUntil != nil && value.LeaseUntil.After(now)) {
		return PrepareSortingRecoveryClaimTicket{}, errors.New("record is not claimable")
	}
	return PrepareSortingRecoveryClaimTicket{RecordID: value.ID, Version: value.Version}, nil
}

func (s *Store) CommitPrepareSortingRecoveryClaim(ctx context.Context, ticket PrepareSortingRecoveryClaimTicket, owner string, now time.Time, lease time.Duration) error {
	result, err := s.DB.ExecContext(ctx, `UPDATE runtime_flow_records
		SET state='running',owner=?,lease_until=?,version=version+1,updated_at=?
		WHERE id=?`, owner, stamp(now.Add(lease)), stamp(now), ticket.RecordID)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return errors.New("claim conflict")
	}
	return nil
}
