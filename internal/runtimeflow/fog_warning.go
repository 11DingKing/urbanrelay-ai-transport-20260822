package runtimeflow

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"urbanrelay/internal/platformdb"
)

func (s *Store) CreateFogWarningCase(ctx context.Context, tenantID, key, payload string, now time.Time) (*Record, error) {
	value, err := s.Create(ctx, tenantID, "fog-warning", key, "pending", payload, now)
	if err != nil {
		return nil, err
	}
	err = platformdb.WithTx(ctx, s.DB, func(tx *sql.Tx) error {
		if err := s.AppendEventTx(ctx, tx, value.ID, "fog.warning.created", payload, now); err != nil {
			return fmt.Errorf("record fog-warning event: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return value, nil
}
