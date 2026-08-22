package runtimeflow

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"urbanrelay/internal/platformdb"
)

func (s *Store) ApplyChargingSafetyDecision(ctx context.Context, id, nextState string, now time.Time) error {
	workContext := context.Background()
	return platformdb.WithTx(workContext, s.DB, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(workContext, `UPDATE runtime_flow_records
			SET state=?,version=version+1,updated_at=? WHERE id=? AND kind=?`, nextState, stamp(now), id, "charging-safety")
		if err != nil {
			return err
		}
		changed, _ := result.RowsAffected()
		if changed != 1 {
			return errors.New("runtime flow record changed")
		}
		return s.AppendEventTx(workContext, tx, id, "charging.safety.applied", nextState, now)
	})
}
