package sorting

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

const columns = "id, wave_no, hub_id, chute_code, expected_items, profile, status, version, requested_by, closes_at, created_at, updated_at"

func (r *Repository) CreateTx(ctx context.Context, tx *sql.Tx, value *Wave) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO sorting_waves(`+columns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		value.ID, value.WaveNo, nullable(value.HubID), nullable(value.ChuteCode), value.ExpectedItems, value.Profile, value.Status, value.Version,
		value.RequestedBy, stamp(value.ClosesAt), stamp(value.CreatedAt), stamp(value.UpdatedAt))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return apperr.Wrap(apperr.CodeConflict, "sorting wave already exists", err)
		}
		return fmt.Errorf("insert sorting wave: %w", err)
	}
	return nil
}

func (r *Repository) Get(ctx context.Context, id string) (*Wave, error) {
	return scanWave(r.db.QueryRowContext(ctx, `SELECT `+columns+` FROM sorting_waves WHERE id=?`, id))
}

func (r *Repository) GetByNumber(ctx context.Context, number string) (*Wave, error) {
	return scanWave(r.db.QueryRowContext(ctx, `SELECT `+columns+` FROM sorting_waves WHERE wave_no=?`, number))
}

func (r *Repository) List(ctx context.Context, page domain.Page) ([]Wave, int, error) {
	page = page.Normalize()
	args := []any{}
	where := " WHERE 1=1"
	if page.Status != "" {
		where += " AND status=?"
		args = append(args, page.Status)
	}
	if page.Query != "" {
		where += " AND (wave_no LIKE ? OR profile LIKE ?)"
		term := "%" + page.Query + "%"
		args = append(args, term, term)
	}
	var total int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sorting_waves"+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count sorting wave: %w", err)
	}
	rows, err := r.db.QueryContext(ctx, "SELECT "+columns+" FROM sorting_waves"+where+" ORDER BY created_at DESC, id LIMIT ? OFFSET ?", append(args, page.Limit, page.Offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list sorting wave: %w", err)
	}
	defer rows.Close()
	values := make([]Wave, 0, page.Limit)
	for rows.Next() {
		value, err := scanWave(rows)
		if err != nil {
			return nil, 0, err
		}
		values = append(values, *value)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate sorting wave: %w", err)
	}
	return values, total, nil
}

func (r *Repository) TransitionTx(ctx context.Context, tx *sql.Tx, id string, from, to domain.Status, expectedVersion int, now time.Time) error {
	result, err := tx.ExecContext(ctx, `UPDATE sorting_waves SET status=?, version=version+1, updated_at=? WHERE id=? AND status=? AND version=?`, to, stamp(now), id, from, expectedVersion)
	if err != nil {
		return fmt.Errorf("transition sorting wave: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("transition sorting wave result: %w", err)
	}
	if affected != 1 {
		return apperr.New(apperr.CodeConflict, "sorting wave state or version changed")
	}
	return nil
}

func (r *Repository) DeleteDraft(ctx context.Context, id string, expectedVersion int) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM sorting_waves WHERE id=? AND status=? AND version=?`, id, domain.StatusPlanned, expectedVersion)
	if err != nil {
		return fmt.Errorf("delete sorting wave: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return apperr.New(apperr.CodeConflict, "sorting wave is no longer removable")
	}
	return nil
}

type scanner interface{ Scan(...any) error }

func scanWave(row scanner) (*Wave, error) {
	var value Wave
	var parent, resource sql.NullString
	var due, created, updated string
	err := row.Scan(&value.ID, &value.WaveNo, &parent, &resource, &value.ExpectedItems, &value.Profile, &value.Status, &value.Version, &value.RequestedBy, &due, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperr.New(apperr.CodeNotFound, "sorting wave not found")
	}
	if err != nil {
		return nil, fmt.Errorf("scan sorting wave: %w", err)
	}
	value.HubID = parent.String
	value.ChuteCode = resource.String
	value.ClosesAt = parseStamp(due)
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
