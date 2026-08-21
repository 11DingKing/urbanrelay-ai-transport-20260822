package idempotency

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
	"urbanrelay/internal/apperr"
	"urbanrelay/internal/platformdb"
)

type Record struct {
	Scope, Key, RequestHash, ResponseJSON string
	StatusCode                            int
	ExpiresAt, CreatedAt                  time.Time
}
type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) Lookup(ctx context.Context, scope, key, requestHash string, now time.Time) (string, bool, error) {
	var storedHash, response, expires string
	err := r.db.QueryRowContext(ctx, `SELECT request_hash,response_json,expires_at FROM idempotency_records WHERE scope=? AND idempotency_key=?`, scope, key).Scan(&storedHash, &response, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("lookup idempotency record: %w", err)
	}
	expiry, err := time.Parse(time.RFC3339Nano, expires)
	if err != nil {
		return "", false, fmt.Errorf("parse idempotency expiry: %w", err)
	}
	if !expiry.After(now) {
		return "", false, nil
	}
	if storedHash != requestHash {
		return "", false, apperr.New(apperr.CodeConflict, "idempotency key was used with another request")
	}
	return response, true, nil
}

func (r *Repository) SaveTx(ctx context.Context, tx *sql.Tx, record Record) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO idempotency_records(scope,idempotency_key,request_hash,response_json,status_code,expires_at,created_at) VALUES(?,?,?,?,?,?,?)`, record.Scope, record.Key, record.RequestHash, record.ResponseJSON, record.StatusCode, platformdb.Timestamp(record.ExpiresAt), platformdb.Timestamp(record.CreatedAt))
	if err != nil {
		return fmt.Errorf("save idempotency record: %w", err)
	}
	return nil
}
func (r *Repository) Prune(ctx context.Context, now time.Time) (int64, error) {
	result, err := r.db.ExecContext(ctx, `DELETE FROM idempotency_records WHERE expires_at<=?`, platformdb.Timestamp(now))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
