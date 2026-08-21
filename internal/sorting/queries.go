package sorting

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
	rows, err := r.db.QueryContext(ctx, `SELECT status, COUNT(*), MIN(closes_at) FROM sorting_waves GROUP BY status ORDER BY status`)
	if err != nil {
		return nil, fmt.Errorf("query sorting wave queue: %w", err)
	}
	defer rows.Close()
	result := []QueueSummary{}
	for rows.Next() {
		var value QueueSummary
		var earliest string
		if err := rows.Scan(&value.Status, &value.Count, &earliest); err != nil {
			return nil, fmt.Errorf("scan sorting wave queue: %w", err)
		}
		value.EarliestDueAt = parseStamp(earliest)
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sorting wave queue: %w", err)
	}
	return result, nil
}

func (r *Repository) DueBefore(ctx context.Context, deadline time.Time, limit int) ([]Wave, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+columns+` FROM sorting_waves WHERE closes_at <= ? AND status NOT IN (?,?) ORDER BY closes_at, id LIMIT ?`, stamp(deadline), domain.StatusCompleted, domain.StatusCanceled, limit)
	if err != nil {
		return nil, fmt.Errorf("query due sorting wave: %w", err)
	}
	defer rows.Close()
	values := []Wave{}
	for rows.Next() {
		value, err := scanWave(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, *value)
	}
	return values, rows.Err()
}
