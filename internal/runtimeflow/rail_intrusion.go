package runtimeflow

import (
	"context"
	"errors"
	"time"
)

type PrepareRailIntrusionClaimTicket struct {
	RecordID string
	Version  int
}

func (s *Store) PrepareRailIntrusionClaim(ctx context.Context, id string, now time.Time) (PrepareRailIntrusionClaimTicket, error) {
	value, err := s.Get(ctx, id)
	if err != nil {
		return PrepareRailIntrusionClaimTicket{}, err
	}
	if value.Kind != "rail-intrusion" || (value.State == "running" && value.LeaseUntil != nil && value.LeaseUntil.After(now)) {
		return PrepareRailIntrusionClaimTicket{}, errors.New("record is not claimable")
	}
	return PrepareRailIntrusionClaimTicket{RecordID: value.ID, Version: value.Version}, nil
}

func (s *Store) CommitPrepareRailIntrusionClaim(ctx context.Context, ticket PrepareRailIntrusionClaimTicket, owner string, now time.Time, lease time.Duration) error {
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
