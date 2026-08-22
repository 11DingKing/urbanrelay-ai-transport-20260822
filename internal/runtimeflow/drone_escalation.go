package runtimeflow

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"urbanrelay/internal/platformdb"
)

func (s *Store) OpenDroneInspectionEscalation(ctx context.Context, tenantID, key, payload string, now time.Time) (*Record, error) {
	value, err := s.Create(ctx, tenantID, "drone-escalation", key, "pending", payload, now)
	if err != nil {
		return nil, err
	}
	err = platformdb.WithTx(ctx, s.DB, func(tx *sql.Tx) error {
		if err := s.AppendEventTx(ctx, tx, value.ID, "inspection.escalated", payload, now); err != nil {
			return fmt.Errorf("record drone-escalation event: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return value, nil
}
