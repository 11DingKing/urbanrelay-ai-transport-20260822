package inspection

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

const columns = "id, inspection_no, hub_id, asset_id, priority, target_ref, status, version, requested_by, due_at, created_at, updated_at"

func (r *Repository) CreateTx(ctx context.Context, tx *sql.Tx, value *Inspection) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO inspections(`+columns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		value.ID, value.InspectionNo, nullable(value.HubID), nullable(value.AssetID), value.Priority, value.TargetRef, value.Status, value.Version,
		value.RequestedBy, stamp(value.DueAt), stamp(value.CreatedAt), stamp(value.UpdatedAt))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return apperr.Wrap(apperr.CodeConflict, "drone inspection already exists", err)
		}
		return fmt.Errorf("insert drone inspection: %w", err)
	}
	return nil
}

func (r *Repository) Get(ctx context.Context, id string) (*Inspection, error) {
	return scanInspection(r.db.QueryRowContext(ctx, `SELECT `+columns+` FROM inspections WHERE id=?`, id))
}

func (r *Repository) GetByNumber(ctx context.Context, number string) (*Inspection, error) {
	return scanInspection(r.db.QueryRowContext(ctx, `SELECT `+columns+` FROM inspections WHERE inspection_no=?`, number))
}

func (r *Repository) List(ctx context.Context, page domain.Page) ([]Inspection, int, error) {
	page = page.Normalize()
	args := []any{}
	where := " WHERE 1=1"
	if page.Status != "" {
		where += " AND status=?"
		args = append(args, page.Status)
	}
	if page.Query != "" {
		where += " AND (inspection_no LIKE ? OR target_ref LIKE ?)"
		term := "%" + page.Query + "%"
		args = append(args, term, term)
	}
	var total int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM inspections"+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count drone inspection: %w", err)
	}
	rows, err := r.db.QueryContext(ctx, "SELECT "+columns+" FROM inspections"+where+" ORDER BY created_at DESC, id LIMIT ? OFFSET ?", append(args, page.Limit, page.Offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list drone inspection: %w", err)
	}
	defer rows.Close()
	values := make([]Inspection, 0, page.Limit)
	for rows.Next() {
		value, err := scanInspection(rows)
		if err != nil {
			return nil, 0, err
		}
		values = append(values, *value)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate drone inspection: %w", err)
	}
	return values, total, nil
}

func (r *Repository) TransitionTx(ctx context.Context, tx *sql.Tx, id string, from, to domain.Status, expectedVersion int, now time.Time) error {
	result, err := tx.ExecContext(ctx, `UPDATE inspections SET status=?, version=version+1, updated_at=? WHERE id=? AND status=? AND version=?`, to, stamp(now), id, from, expectedVersion)
	if err != nil {
		return fmt.Errorf("transition drone inspection: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("transition drone inspection result: %w", err)
	}
	if affected != 1 {
		return apperr.New(apperr.CodeConflict, "drone inspection state or version changed")
	}
	return nil
}

func (r *Repository) DeleteDraft(ctx context.Context, id string, expectedVersion int) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM inspections WHERE id=? AND status=? AND version=?`, id, domain.StatusPlanned, expectedVersion)
	if err != nil {
		return fmt.Errorf("delete drone inspection: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return apperr.New(apperr.CodeConflict, "drone inspection is no longer removable")
	}
	return nil
}

type scanner interface{ Scan(...any) error }

func scanInspection(row scanner) (*Inspection, error) {
	var value Inspection
	var parent, resource sql.NullString
	var due, created, updated string
	err := row.Scan(&value.ID, &value.InspectionNo, &parent, &resource, &value.Priority, &value.TargetRef, &value.Status, &value.Version, &value.RequestedBy, &due, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperr.New(apperr.CodeNotFound, "drone inspection not found")
	}
	if err != nil {
		return nil, fmt.Errorf("scan drone inspection: %w", err)
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
