package runtimeflow

import (
	"context"
	"errors"
	"time"
)

type PrepareAirspaceInspectionClaimTicket struct {
	RecordID string
	Version  int
}

func (s *Store) PrepareAirspaceInspectionClaim(ctx context.Context, id string, now time.Time) (PrepareAirspaceInspectionClaimTicket, error) {
	value, err := s.Get(ctx, id)
	if err != nil {
		return PrepareAirspaceInspectionClaimTicket{}, err
	}
	if value.Kind != "airspace-inspection" || (value.State == "running" && value.LeaseUntil != nil && value.LeaseUntil.After(now)) {
		return PrepareAirspaceInspectionClaimTicket{}, errors.New("record is not claimable")
	}
	return PrepareAirspaceInspectionClaimTicket{RecordID: value.ID, Version: value.Version}, nil
}

func (s *Store) CommitPrepareAirspaceInspectionClaim(ctx context.Context, ticket PrepareAirspaceInspectionClaimTicket, owner string, now time.Time, lease time.Duration) error {
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
