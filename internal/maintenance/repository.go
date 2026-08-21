package maintenance

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"urbanrelay/internal/apperr"
	"urbanrelay/internal/domain"
	"urbanrelay/internal/platformdb"
)

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

const columns = "id, task_no, hub_id, asset_id, estimated_minutes, reason, status, version, requested_by, due_at, created_at, updated_at"

func (r *Repository) CreateTx(ctx context.Context, tx *sql.Tx, value *Maintenance) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO maintenance_tasks(`+columns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		value.ID, value.TaskNo, nullable(value.HubID), nullable(value.AssetID), value.EstimatedMinutes, value.Reason, value.Status, value.Version,
		value.RequestedBy, stamp(value.DueAt), stamp(value.CreatedAt), stamp(value.UpdatedAt))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return apperr.Wrap(apperr.CodeConflict, "equipment maintenance already exists", err)
		}
		return fmt.Errorf("insert equipment maintenance: %w", err)
	}
	return nil
}

func (r *Repository) Get(ctx context.Context, id string) (*Maintenance, error) {
	return scanMaintenance(r.db.QueryRowContext(ctx, `SELECT `+columns+` FROM maintenance_tasks WHERE id=?`, id))
}

func (r *Repository) GetByNumber(ctx context.Context, number string) (*Maintenance, error) {
	return scanMaintenance(r.db.QueryRowContext(ctx, `SELECT `+columns+` FROM maintenance_tasks WHERE task_no=?`, number))
}

func (r *Repository) List(ctx context.Context, page domain.Page) ([]Maintenance, int, error) {
	page = page.Normalize()
	args := []any{}
	where := " WHERE 1=1"
	if page.Status != "" {
		where += " AND status=?"
		args = append(args, page.Status)
	}
	if page.Query != "" {
		where += " AND (task_no LIKE ? OR reason LIKE ?)"
		term := "%" + page.Query + "%"
		args = append(args, term, term)
	}
	var total int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM maintenance_tasks"+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count equipment maintenance: %w", err)
	}
	rows, err := r.db.QueryContext(ctx, "SELECT "+columns+" FROM maintenance_tasks"+where+" ORDER BY created_at DESC, id LIMIT ? OFFSET ?", append(args, page.Limit, page.Offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list equipment maintenance: %w", err)
	}
	defer rows.Close()
	values := make([]Maintenance, 0, page.Limit)
	for rows.Next() {
		value, err := scanMaintenance(rows)
		if err != nil {
			return nil, 0, err
		}
		values = append(values, *value)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate equipment maintenance: %w", err)
	}
	return values, total, nil
}

func (r *Repository) TransitionTx(ctx context.Context, tx *sql.Tx, id string, from, to domain.Status, expectedVersion int, now time.Time) error {
	result, err := tx.ExecContext(ctx, `UPDATE maintenance_tasks SET status=?, version=version+1, updated_at=? WHERE id=? AND status=? AND version=?`, to, stamp(now), id, from, expectedVersion)
	if err != nil {
		return fmt.Errorf("transition equipment maintenance: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("transition equipment maintenance result: %w", err)
	}
	if affected != 1 {
		return apperr.New(apperr.CodeConflict, "equipment maintenance state or version changed")
	}
	return nil
}

func (r *Repository) DeleteDraft(ctx context.Context, id string, expectedVersion int) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM maintenance_tasks WHERE id=? AND status=? AND version=?`, id, domain.StatusPlanned, expectedVersion)
	if err != nil {
		return fmt.Errorf("delete equipment maintenance: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return apperr.New(apperr.CodeConflict, "equipment maintenance is no longer removable")
	}
	return nil
}

type scanner interface{ Scan(...any) error }

func scanMaintenance(row scanner) (*Maintenance, error) {
	var value Maintenance
	var parent, resource sql.NullString
	var due, created, updated string
	err := row.Scan(&value.ID, &value.TaskNo, &parent, &resource, &value.EstimatedMinutes, &value.Reason, &value.Status, &value.Version, &value.RequestedBy, &due, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperr.New(apperr.CodeNotFound, "equipment maintenance not found")
	}
	if err != nil {
		return nil, fmt.Errorf("scan equipment maintenance: %w", err)
	}
	value.HubID = parent.String
	value.AssetID = resource.String
	value.DueAt = parseStamp(due)
	value.CreatedAt = parseStamp(created)
	value.UpdatedAt = parseStamp(updated)
	return &value, nil
}
func stamp(value time.Time) string { return platformdb.Timestamp(value) }
func parseStamp(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, value)
	return parsed
}
func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

var _ platformdb.Executor = (*sql.DB)(nil)
