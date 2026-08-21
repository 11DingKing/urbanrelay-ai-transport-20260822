package mission

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

const columns = "id, business_no, hub_id, asset_id, payload_units, route_code, status, version, requested_by, due_at, created_at, updated_at"

func (r *Repository) CreateTx(ctx context.Context, tx *sql.Tx, value *Mission) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO missions(`+columns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		value.ID, value.BusinessNo, nullable(value.HubID), nullable(value.AssetID), value.PayloadUnits, value.RouteCode, value.Status, value.Version,
		value.RequestedBy, stamp(value.DueAt), stamp(value.CreatedAt), stamp(value.UpdatedAt))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return apperr.Wrap(apperr.CodeConflict, "delivery mission already exists", err)
		}
		return fmt.Errorf("insert delivery mission: %w", err)
	}
	return nil
}

func (r *Repository) Get(ctx context.Context, id string) (*Mission, error) {
	return scanMission(r.db.QueryRowContext(ctx, `SELECT `+columns+` FROM missions WHERE id=?`, id))
}

func (r *Repository) GetByNumber(ctx context.Context, number string) (*Mission, error) {
	return scanMission(r.db.QueryRowContext(ctx, `SELECT `+columns+` FROM missions WHERE business_no=?`, number))
}

func (r *Repository) List(ctx context.Context, page domain.Page) ([]Mission, int, error) {
	page = page.Normalize()
	args := []any{}
	where := " WHERE 1=1"
	if page.Status != "" {
		where += " AND status=?"
		args = append(args, page.Status)
	}
	if page.Query != "" {
		where += " AND (business_no LIKE ? OR route_code LIKE ?)"
		term := "%" + page.Query + "%"
		args = append(args, term, term)
	}
	var total int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM missions"+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count delivery mission: %w", err)
	}
	rows, err := r.db.QueryContext(ctx, "SELECT "+columns+" FROM missions"+where+" ORDER BY created_at DESC, id LIMIT ? OFFSET ?", append(args, page.Limit, page.Offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list delivery mission: %w", err)
	}
	defer rows.Close()
	values := make([]Mission, 0, page.Limit)
	for rows.Next() {
		value, err := scanMission(rows)
		if err != nil {
			return nil, 0, err
		}
		values = append(values, *value)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate delivery mission: %w", err)
	}
	return values, total, nil
}

func (r *Repository) TransitionTx(ctx context.Context, tx *sql.Tx, id string, from, to domain.Status, expectedVersion int, now time.Time) error {
	result, err := tx.ExecContext(ctx, `UPDATE missions SET status=?, version=version+1, updated_at=? WHERE id=? AND status=? AND version=?`, to, stamp(now), id, from, expectedVersion)
	if err != nil {
		return fmt.Errorf("transition delivery mission: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("transition delivery mission result: %w", err)
	}
	if affected != 1 {
		return apperr.New(apperr.CodeConflict, "delivery mission state or version changed")
	}
	return nil
}

func (r *Repository) DeleteDraft(ctx context.Context, id string, expectedVersion int) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM missions WHERE id=? AND status=? AND version=?`, id, domain.StatusPlanned, expectedVersion)
	if err != nil {
		return fmt.Errorf("delete delivery mission: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return apperr.New(apperr.CodeConflict, "delivery mission is no longer removable")
	}
	return nil
}

type scanner interface{ Scan(...any) error }

func scanMission(row scanner) (*Mission, error) {
	var value Mission
	var parent, resource sql.NullString
	var due, created, updated string
	err := row.Scan(&value.ID, &value.BusinessNo, &parent, &resource, &value.PayloadUnits, &value.RouteCode, &value.Status, &value.Version, &value.RequestedBy, &due, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperr.New(apperr.CodeNotFound, "delivery mission not found")
	}
	if err != nil {
		return nil, fmt.Errorf("scan delivery mission: %w", err)
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
