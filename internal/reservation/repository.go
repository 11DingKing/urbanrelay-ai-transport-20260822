package reservation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
	"urbanrelay/internal/apperr"
	"urbanrelay/internal/platformdb"
)

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

const columns = "id,resource_type,resource_key,owner_type,owner_id,starts_at,ends_at,status,version,created_at,updated_at"

func (r *Repository) ConflictsTx(ctx context.Context, tx *sql.Tx, req ReserveRequest) ([]Reservation, error) {
	rows, err := tx.QueryContext(ctx, `SELECT `+columns+` FROM resource_reservations
		WHERE resource_type=? AND resource_key=? AND status='active' AND starts_at<? AND ends_at>?
		ORDER BY starts_at,id`, req.ResourceType, req.ResourceKey, stamp(req.EndsAt), stamp(req.StartsAt))
	if err != nil {
		return nil, fmt.Errorf("query reservation conflicts: %w", err)
	}
	defer rows.Close()
	conflicts := []Reservation{}
	for rows.Next() {
		value, err := scanReservation(rows)
		if err != nil {
			return nil, err
		}
		conflicts = append(conflicts, *value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate reservation conflicts: %w", err)
	}
	return conflicts, nil
}

func (r *Repository) InsertTx(ctx context.Context, tx *sql.Tx, value Reservation) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO resource_reservations(`+columns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		value.ID, value.ResourceType, value.ResourceKey, value.OwnerType, value.OwnerID, stamp(value.StartsAt), stamp(value.EndsAt), value.Status, value.Version, stamp(value.CreatedAt), stamp(value.UpdatedAt))
	if err != nil {
		return fmt.Errorf("insert resource reservation: %w", err)
	}
	return nil
}

func (r *Repository) Get(ctx context.Context, id string) (*Reservation, error) {
	value, err := scanReservation(r.db.QueryRowContext(ctx, `SELECT `+columns+` FROM resource_reservations WHERE id=?`, id))
	if err != nil {
		return nil, err
	}
	return value, nil
}

func (r *Repository) ActiveForOwner(ctx context.Context, ownerType, ownerID string) ([]Reservation, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+columns+` FROM resource_reservations WHERE owner_type=? AND owner_id=? AND status='active' ORDER BY starts_at`, ownerType, ownerID)
	if err != nil {
		return nil, fmt.Errorf("query owner reservations: %w", err)
	}
	defer rows.Close()
	values := []Reservation{}
	for rows.Next() {
		value, err := scanReservation(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, *value)
	}
	return values, rows.Err()
}

func (r *Repository) ListWindow(ctx context.Context, query WindowQuery) ([]Reservation, error) {
	if err := query.Validate(); err != nil {
		return nil, apperr.Wrap(apperr.CodeInvalid, "invalid reservation window", err)
	}
	if query.Limit < 1 || query.Limit > 500 {
		query.Limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+columns+` FROM resource_reservations
		WHERE resource_type=? AND resource_key=? AND starts_at<? AND ends_at>?
		ORDER BY starts_at,id LIMIT ?`, query.ResourceType, query.ResourceKey, stamp(query.EndsAt), stamp(query.StartsAt), query.Limit)
	if err != nil {
		return nil, fmt.Errorf("list reservation window: %w", err)
	}
	defer rows.Close()
	values := []Reservation{}
	for rows.Next() {
		value, err := scanReservation(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, *value)
	}
	return values, rows.Err()
}

func (r *Repository) ReleaseTx(ctx context.Context, tx *sql.Tx, id string, version int, now time.Time) error {
	result, err := tx.ExecContext(ctx, `UPDATE resource_reservations SET status='released',version=version+1,updated_at=? WHERE id=? AND status='active' AND version=?`, stamp(now), id, version)
	if err != nil {
		return fmt.Errorf("release reservation: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read release result: %w", err)
	}
	if changed != 1 {
		return apperr.New(apperr.CodeConflict, "reservation is no longer active")
	}
	return nil
}

func (r *Repository) Expire(ctx context.Context, now time.Time, limit int) ([]Reservation, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin reservation expiry: %w", err)
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT `+columns+` FROM resource_reservations WHERE status='active' AND (ends_at<=? OR starts_at>?) ORDER BY ends_at,id LIMIT ?`, stamp(now), stamp(now), limit)
	if err != nil {
		return nil, fmt.Errorf("find expired reservations: %w", err)
	}
	values := []Reservation{}
	for rows.Next() {
		value, scanErr := scanReservation(rows)
		if scanErr != nil {
			rows.Close()
			return nil, scanErr
		}
		values = append(values, *value)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close expiry rows: %w", err)
	}
	for _, value := range values {
		if _, err := tx.ExecContext(ctx, `UPDATE resource_reservations SET status='expired',version=version+1,updated_at=? WHERE id=? AND status='active' AND version=?`, stamp(now), value.ID, value.Version); err != nil {
			return nil, fmt.Errorf("expire reservation %s: %w", value.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit reservation expiry: %w", err)
	}
	return values, nil
}

type scanner interface{ Scan(...any) error }

func scanReservation(row scanner) (*Reservation, error) {
	var value Reservation
	var starts, ends, created, updated string
	err := row.Scan(&value.ID, &value.ResourceType, &value.ResourceKey, &value.OwnerType, &value.OwnerID, &starts, &ends, &value.Status, &value.Version, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperr.New(apperr.CodeNotFound, "reservation not found")
	}
	if err != nil {
		return nil, fmt.Errorf("scan reservation: %w", err)
	}
	value.StartsAt = parseStamp(starts)
	value.EndsAt = parseStamp(ends)
	value.CreatedAt = parseStamp(created)
	value.UpdatedAt = parseStamp(updated)
	return &value, nil
}

func stamp(value time.Time) string { return platformdb.Timestamp(value) }
func parseStamp(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, value)
	return parsed
}
