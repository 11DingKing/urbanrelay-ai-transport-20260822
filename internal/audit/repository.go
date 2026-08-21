package audit

import (
	"context"
	"database/sql"
	"fmt"
	"time"
	"urbanrelay/internal/platformdb"
	"urbanrelay/internal/requestmeta"
)

type Event struct {
	ID                                                                int64
	ActorID, Action, ObjectType, ObjectID, Outcome, RequestID, Detail string
	CreatedAt                                                         time.Time
}
type Query struct {
	ObjectType, ObjectID, ActorID string
	Limit, Offset                 int
}
type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) AppendTx(ctx context.Context, tx *sql.Tx, event Event) error {
	if event.RequestID == "" {
		event.RequestID = requestmeta.RequestID(ctx)
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO audit_events(actor_id,action,object_type,object_id,outcome,request_id,detail,created_at) VALUES(?,?,?,?,?,?,?,?)`, event.ActorID, event.Action, event.ObjectType, event.ObjectID, event.Outcome, event.RequestID, event.Detail, platformdb.Timestamp(event.CreatedAt))
	if err != nil {
		return fmt.Errorf("append audit event: %w", err)
	}
	return nil
}

func (r *Repository) List(ctx context.Context, q Query) ([]Event, int, error) {
	if q.Limit <= 0 || q.Limit > 200 {
		q.Limit = 50
	}
	where, args := " WHERE 1=1", []any{}
	if q.ObjectType != "" {
		where += " AND object_type=?"
		args = append(args, q.ObjectType)
	}
	if q.ObjectID != "" {
		where += " AND object_id=?"
		args = append(args, q.ObjectID)
	}
	if q.ActorID != "" {
		where += " AND actor_id=?"
		args = append(args, q.ActorID)
	}
	var total int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM audit_events"+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.QueryContext(ctx, "SELECT id,actor_id,action,object_type,object_id,outcome,request_id,detail,created_at FROM audit_events"+where+" ORDER BY id DESC LIMIT ? OFFSET ?", append(args, q.Limit, q.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	events := []Event{}
	for rows.Next() {
		var e Event
		var stamp string
		if err := rows.Scan(&e.ID, &e.ActorID, &e.Action, &e.ObjectType, &e.ObjectID, &e.Outcome, &e.RequestID, &e.Detail, &stamp); err != nil {
			return nil, 0, err
		}
		e.CreatedAt, _ = time.Parse(time.RFC3339Nano, stamp)
		events = append(events, e)
	}
	return events, total, rows.Err()
}
