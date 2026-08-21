package operations

import (
	"context"
	"database/sql"
	"fmt"
	"time"
	"urbanrelay/internal/platformdb"
)

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) QueueMetrics(ctx context.Context) ([]QueueMetric, error) {
	queries := []struct {
		object string
		table  string
	}{
		{"mission", "missions"},
		{"inspection", "inspections"},
		{"transfer", "transfer_plans"},
		{"sorting", "sorting_waves"},
		{"incident", "incidents"},
		{"maintenance", "maintenance_tasks"},
	}
	metrics := []QueueMetric{}
	for _, query := range queries {
		rows, err := r.db.QueryContext(ctx, "SELECT status,COUNT(*) FROM "+query.table+" GROUP BY status ORDER BY status")
		if err != nil {
			return nil, fmt.Errorf("query %s queue metrics: %w", query.object, err)
		}
		for rows.Next() {
			metric := QueueMetric{ObjectType: query.object}
			if err := rows.Scan(&metric.Status, &metric.Count); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan %s queue metric: %w", query.object, err)
			}
			metrics = append(metrics, metric)
		}
		if err := rows.Close(); err != nil {
			return nil, fmt.Errorf("close %s queue metrics: %w", query.object, err)
		}
	}
	return metrics, nil
}

func (r *Repository) HubMetrics(ctx context.Context, now time.Time) ([]HubMetric, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT h.id,h.code,h.name,
		COUNT(DISTINCT a.id),
		COUNT(DISTINCT CASE WHEN a.status='available' THEN a.id END),
		(SELECT COUNT(*) FROM missions m WHERE m.hub_id=h.id AND m.status IN ('reserved','dispatched','active'))+
		(SELECT COUNT(*) FROM inspections i WHERE i.hub_id=h.id AND i.status IN ('reserved','dispatched','active','awaiting_review'))+
		(SELECT COUNT(*) FROM incidents n WHERE n.hub_id=h.id AND n.status IN ('dispatched','active','awaiting_review')),
		(SELECT COUNT(*) FROM missions m WHERE m.hub_id=h.id AND m.due_at<? AND m.status NOT IN ('completed','canceled'))+
		(SELECT COUNT(*) FROM inspections i WHERE i.hub_id=h.id AND i.due_at<? AND i.status NOT IN ('completed','canceled'))+
		(SELECT COUNT(*) FROM incidents n WHERE n.hub_id=h.id AND n.due_at<? AND n.status NOT IN ('completed','canceled'))
		FROM hubs h LEFT JOIN assets a ON a.hub_id=h.id WHERE h.status='active'
		GROUP BY h.id,h.code,h.name ORDER BY h.code`, stamp(now), stamp(now), stamp(now))
	if err != nil {
		return nil, fmt.Errorf("query hub metrics: %w", err)
	}
	defer rows.Close()
	metrics := []HubMetric{}
	for rows.Next() {
		var metric HubMetric
		if err := rows.Scan(&metric.HubID, &metric.HubCode, &metric.HubName, &metric.AssetCount, &metric.AvailableAssets, &metric.ActiveWork, &metric.OverdueWork); err != nil {
			return nil, fmt.Errorf("scan hub metric: %w", err)
		}
		metrics = append(metrics, metric)
	}
	return metrics, rows.Err()
}

func (r *Repository) Reliability(ctx context.Context, now time.Time) (ReliabilityMetric, error) {
	var metric ReliabilityMetric
	err := r.db.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM outbox_events WHERE status='pending'),
		(SELECT COUNT(*) FROM outbox_events WHERE status='retrying'),
		(SELECT COUNT(*) FROM worker_jobs WHERE status='failed'),
		(SELECT COUNT(*) FROM worker_jobs WHERE status='retrying'),
		(SELECT COUNT(*) FROM sessions WHERE expires_at<=? AND revoked_at IS NULL)`, stamp(now)).
		Scan(&metric.PendingOutbox, &metric.RetryingOutbox, &metric.FailedJobs, &metric.RetryingJobs, &metric.ExpiredSessions)
	if err != nil {
		return ReliabilityMetric{}, fmt.Errorf("query reliability metrics: %w", err)
	}
	return metric, nil
}

func (r *Repository) SearchAudit(ctx context.Context, query AuditSearch) ([]AuditEntry, int, error) {
	query = query.Normalize()
	where := " WHERE created_at>=? AND created_at<?"
	args := []any{stamp(query.From), stamp(query.Until)}
	if query.ObjectType != "" {
		where += " AND object_type=?"
		args = append(args, query.ObjectType)
	}
	if query.ObjectID != "" {
		where += " AND object_id=?"
		args = append(args, query.ObjectID)
	}
	if query.ActorID != "" {
		where += " AND actor_id=?"
		args = append(args, query.ActorID)
	}
	var total int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM audit_events"+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count audit entries: %w", err)
	}
	offset := (query.Page - 1) * query.Limit
	rows, err := r.db.QueryContext(ctx, "SELECT id,actor_id,action,object_type,object_id,outcome,request_id,detail,created_at FROM audit_events"+where+" ORDER BY id DESC LIMIT ? OFFSET ?", append(args, query.Limit, offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("search audit entries: %w", err)
	}
	defer rows.Close()
	entries := []AuditEntry{}
	for rows.Next() {
		var entry AuditEntry
		var created string
		if err := rows.Scan(&entry.ID, &entry.ActorID, &entry.Action, &entry.ObjectType, &entry.ObjectID, &entry.Outcome, &entry.RequestID, &entry.Detail, &created); err != nil {
			return nil, 0, fmt.Errorf("scan audit entry: %w", err)
		}
		entry.CreatedAt = parseStamp(created)
		entries = append(entries, entry)
	}
	return entries, total, rows.Err()
}

func stamp(value time.Time) string { return platformdb.Timestamp(value) }
func parseStamp(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, value)
	return parsed
}
