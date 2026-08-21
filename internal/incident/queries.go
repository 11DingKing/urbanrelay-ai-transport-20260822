package incident

import (
	"context"
	"fmt"
	"time"
	"urbanrelay/internal/domain"
)

type QueueSummary struct {
	Status        domain.Status `json:"status"`
	Count         int           `json:"count"`
	EarliestDueAt time.Time     `json:"earliest_due_at"`
}

func (r *Repository) Queue(ctx context.Context) ([]QueueSummary, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT status, COUNT(*), MIN(due_at) FROM incidents GROUP BY status ORDER BY status`)
	if err != nil {
		return nil, fmt.Errorf("query traffic incident queue: %w", err)
	}
	defer rows.Close()
	result := []QueueSummary{}
	for rows.Next() {
		var value QueueSummary
		var earliest string
		if err := rows.Scan(&value.Status, &value.Count, &earliest); err != nil {
			return nil, fmt.Errorf("scan traffic incident queue: %w", err)
		}
		value.EarliestDueAt = parseStamp(earliest)
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate traffic incident queue: %w", err)
	}
	return result, nil
}

func (r *Repository) DueBefore(ctx context.Context, deadline time.Time, limit int) ([]Incident, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+columns+` FROM incidents WHERE due_at <= ? AND status NOT IN (?,?) ORDER BY due_at, id LIMIT ?`, stamp(deadline), domain.StatusCompleted, domain.StatusCanceled, limit)
	if err != nil {
		return nil, fmt.Errorf("query due traffic incident: %w", err)
	}
	defer rows.Close()
	values := []Incident{}
	for rows.Next() {
		value, err := scanIncident(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, *value)
	}
	return values, rows.Err()
}
