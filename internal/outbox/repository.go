package outbox

import (
	"context"
	"database/sql"
	"fmt"
	"time"
	"urbanrelay/internal/platformdb"
)

type Event struct {
	ID                                                 int64
	Topic, AggregateType, AggregateID, Payload, Status string
	Attempts                                           int
	NextAttemptAt, CreatedAt                           time.Time
	PublishedAt                                        *time.Time
}
type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }
func (r *Repository) AppendTx(ctx context.Context, tx *sql.Tx, e Event) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO outbox_events(topic,aggregate_type,aggregate_id,payload,status,attempts,next_attempt_at,created_at) VALUES(?,?,?,?,?,?,?,?)`, e.Topic, e.AggregateType, e.AggregateID, e.Payload, "pending", 0, platformdb.Timestamp(e.CreatedAt), platformdb.Timestamp(e.CreatedAt))
	if err != nil {
		return fmt.Errorf("append outbox event: %w", err)
	}
	return nil
}
func (r *Repository) Due(ctx context.Context, now time.Time, limit int) ([]Event, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id,topic,aggregate_type,aggregate_id,payload,status,attempts,next_attempt_at,created_at,published_at FROM outbox_events WHERE status IN ('pending','retrying') AND next_attempt_at<=? ORDER BY id LIMIT ?`, platformdb.Timestamp(now), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Event{}
	for rows.Next() {
		var e Event
		var next, created string
		var published sql.NullString
		if err := rows.Scan(&e.ID, &e.Topic, &e.AggregateType, &e.AggregateID, &e.Payload, &e.Status, &e.Attempts, &next, &created, &published); err != nil {
			return nil, err
		}
		e.NextAttemptAt, _ = time.Parse(time.RFC3339Nano, next)
		e.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		if published.Valid {
			value, _ := time.Parse(time.RFC3339Nano, published.String)
			e.PublishedAt = &value
		}
		result = append(result, e)
	}
	return result, rows.Err()
}
func (r *Repository) MarkPublished(ctx context.Context, id int64, now time.Time) error {
	result, err := r.db.ExecContext(ctx, `UPDATE outbox_events SET status='published',published_at=? WHERE id=? AND status IN ('pending','retrying')`, platformdb.Timestamp(now), id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return fmt.Errorf("outbox event %d is not publishable", id)
	}
	return nil
}
func (r *Repository) MarkRetry(ctx context.Context, id int64, attempts int, next time.Time) error {
	result, err := r.db.ExecContext(ctx, `UPDATE outbox_events SET status='retrying',attempts=?,next_attempt_at=? WHERE id=? AND status IN ('pending','retrying','published')`, attempts, platformdb.Timestamp(next), id)
	if err != nil {
		return fmt.Errorf("mark outbox event for retry: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read outbox retry result: %w", err)
	}
	if changed != 1 {
		return fmt.Errorf("outbox event %d is not retryable", id)
	}
	return nil
}
