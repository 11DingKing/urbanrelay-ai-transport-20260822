package transfer

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

const columns = "id, plan_no, origin_hub_id, destination_hub_id, segment_count, mode_chain, status, version, requested_by, connection_at, created_at, updated_at"

func (r *Repository) CreateTx(ctx context.Context, tx *sql.Tx, value *Transfer) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO transfer_plans(`+columns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		value.ID, value.PlanNo, nullable(value.OriginHubID), nullable(value.DestinationHubID), value.SegmentCount, value.ModeChain, value.Status, value.Version,
		value.RequestedBy, stamp(value.ConnectionAt), stamp(value.CreatedAt), stamp(value.UpdatedAt))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return apperr.Wrap(apperr.CodeConflict, "multimodal transfer already exists", err)
		}
		return fmt.Errorf("insert multimodal transfer: %w", err)
	}
	return nil
}

func (r *Repository) Get(ctx context.Context, id string) (*Transfer, error) {
	return scanTransfer(r.db.QueryRowContext(ctx, `SELECT `+columns+` FROM transfer_plans WHERE id=?`, id))
}

func (r *Repository) GetByNumber(ctx context.Context, number string) (*Transfer, error) {
	return scanTransfer(r.db.QueryRowContext(ctx, `SELECT `+columns+` FROM transfer_plans WHERE plan_no=?`, number))
}

func (r *Repository) List(ctx context.Context, page domain.Page) ([]Transfer, int, error) {
	page = page.Normalize()
	args := []any{}
	where := " WHERE 1=1"
	if page.Status != "" {
		where += " AND status=?"
		args = append(args, page.Status)
	}
	if page.Query != "" {
		where += " AND (plan_no LIKE ? OR mode_chain LIKE ?)"
		term := "%" + page.Query + "%"
		args = append(args, term, term)
	}
	var total int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM transfer_plans"+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count multimodal transfer: %w", err)
	}
	rows, err := r.db.QueryContext(ctx, "SELECT "+columns+" FROM transfer_plans"+where+" ORDER BY created_at DESC, id LIMIT ? OFFSET ?", append(args, page.Limit, page.Offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list multimodal transfer: %w", err)
	}
	defer rows.Close()
	values := make([]Transfer, 0, page.Limit)
	for rows.Next() {
		value, err := scanTransfer(rows)
		if err != nil {
			return nil, 0, err
		}
		values = append(values, *value)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate multimodal transfer: %w", err)
	}
	return values, total, nil
}

func (r *Repository) TransitionTx(ctx context.Context, tx *sql.Tx, id string, from, to domain.Status, expectedVersion int, now time.Time) error {
	result, err := tx.ExecContext(ctx, `UPDATE transfer_plans SET status=?, version=version+1, updated_at=? WHERE id=? AND status=? AND version=?`, to, stamp(now), id, from, expectedVersion)
	if err != nil {
		return fmt.Errorf("transition multimodal transfer: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("transition multimodal transfer result: %w", err)
	}
	if affected != 1 {
		return apperr.New(apperr.CodeConflict, "multimodal transfer state or version changed")
	}
	return nil
}

func (r *Repository) DeleteDraft(ctx context.Context, id string, expectedVersion int) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM transfer_plans WHERE id=? AND status=? AND version=?`, id, domain.StatusPlanned, expectedVersion)
	if err != nil {
		return fmt.Errorf("delete multimodal transfer: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return apperr.New(apperr.CodeConflict, "multimodal transfer is no longer removable")
	}
	return nil
}

type scanner interface{ Scan(...any) error }

func scanTransfer(row scanner) (*Transfer, error) {
	var value Transfer
	var parent, resource sql.NullString
	var due, created, updated string
	err := row.Scan(&value.ID, &value.PlanNo, &parent, &resource, &value.SegmentCount, &value.ModeChain, &value.Status, &value.Version, &value.RequestedBy, &due, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperr.New(apperr.CodeNotFound, "multimodal transfer not found")
	}
	if err != nil {
		return nil, fmt.Errorf("scan multimodal transfer: %w", err)
	}
	value.OriginHubID = parent.String
	value.DestinationHubID = resource.String
	value.ConnectionAt = parseStamp(due)
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
