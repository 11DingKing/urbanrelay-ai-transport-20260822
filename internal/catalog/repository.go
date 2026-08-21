package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"urbanrelay/internal/apperr"
	"urbanrelay/internal/platformdb"
)

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) InsertHubTx(ctx context.Context, tx *sql.Tx, hub Hub) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO hubs(id,code,name,kind,status,capacity,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		hub.ID, hub.Code, hub.Name, hub.Kind, hub.Status, hub.Capacity, hub.Version, stamp(hub.CreatedAt), stamp(hub.UpdatedAt))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return apperr.Wrap(apperr.CodeConflict, "hub code already exists", err)
		}
		return fmt.Errorf("insert hub: %w", err)
	}
	return nil
}

func (r *Repository) InsertAssetTx(ctx context.Context, tx *sql.Tx, asset Asset) error {
	capabilities, err := json.Marshal(asset.Capabilities)
	if err != nil {
		return fmt.Errorf("encode asset capabilities: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO assets(id,asset_no,kind,hub_id,status,capabilities,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		asset.ID, asset.AssetNo, asset.Kind, asset.HubID, asset.Status, string(capabilities), asset.Version, stamp(asset.CreatedAt), stamp(asset.UpdatedAt))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return apperr.Wrap(apperr.CodeConflict, "asset number already exists", err)
		}
		return fmt.Errorf("insert asset: %w", err)
	}
	return nil
}

func (r *Repository) GetHub(ctx context.Context, id string) (*Hub, error) {
	var hub Hub
	var created, updated string
	err := r.db.QueryRowContext(ctx, `SELECT id,code,name,kind,status,capacity,version,created_at,updated_at FROM hubs WHERE id=?`, id).
		Scan(&hub.ID, &hub.Code, &hub.Name, &hub.Kind, &hub.Status, &hub.Capacity, &hub.Version, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperr.New(apperr.CodeNotFound, "hub not found")
	}
	if err != nil {
		return nil, fmt.Errorf("get hub: %w", err)
	}
	hub.CreatedAt = parseStamp(created)
	hub.UpdatedAt = parseStamp(updated)
	return &hub, nil
}

func (r *Repository) GetAsset(ctx context.Context, id string) (*Asset, error) {
	return scanAsset(r.db.QueryRowContext(ctx, `SELECT id,asset_no,kind,hub_id,status,capabilities,version,created_at,updated_at FROM assets WHERE id=?`, id))
}

func (r *Repository) ListAssets(ctx context.Context, filter AssetFilter) ([]Asset, int, error) {
	filter = filter.Normalize()
	where := " WHERE 1=1"
	args := []any{}
	if filter.HubID != "" {
		where += " AND hub_id=?"
		args = append(args, filter.HubID)
	}
	if filter.Kind != "" {
		where += " AND kind=?"
		args = append(args, filter.Kind)
	}
	if filter.State != "" {
		where += " AND status=?"
		args = append(args, filter.State)
	}
	var total int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM assets"+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count assets: %w", err)
	}
	offset := (filter.Page - 1) * filter.Limit
	rows, err := r.db.QueryContext(ctx, "SELECT id,asset_no,kind,hub_id,status,capabilities,version,created_at,updated_at FROM assets"+where+" ORDER BY asset_no LIMIT ? OFFSET ?", append(args, filter.Limit, offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list assets: %w", err)
	}
	defer rows.Close()
	assets := make([]Asset, 0, filter.Limit)
	for rows.Next() {
		asset, err := scanAsset(rows)
		if err != nil {
			return nil, 0, err
		}
		assets = append(assets, *asset)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate assets: %w", err)
	}
	return assets, total, nil
}

func (r *Repository) UpdateAssetStateTx(ctx context.Context, tx *sql.Tx, id, from, to string, version int, now time.Time) error {
	result, err := tx.ExecContext(ctx, `UPDATE assets SET status=?,version=version+1,updated_at=? WHERE id=? AND status=? AND version=?`, to, stamp(now), id, from, version)
	if err != nil {
		return fmt.Errorf("update asset state: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read asset update result: %w", err)
	}
	if changed != 1 {
		return apperr.New(apperr.CodeConflict, "asset state or version changed")
	}
	return nil
}

type scanner interface{ Scan(...any) error }

func scanAsset(row scanner) (*Asset, error) {
	var asset Asset
	var capabilities, created, updated string
	err := row.Scan(&asset.ID, &asset.AssetNo, &asset.Kind, &asset.HubID, &asset.Status, &capabilities, &asset.Version, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperr.New(apperr.CodeNotFound, "asset not found")
	}
	if err != nil {
		return nil, fmt.Errorf("scan asset: %w", err)
	}
	if err := json.Unmarshal([]byte(capabilities), &asset.Capabilities); err != nil {
		return nil, fmt.Errorf("decode asset capabilities: %w", err)
	}
	asset.CreatedAt = parseStamp(created)
	asset.UpdatedAt = parseStamp(updated)
	return &asset, nil
}

func stamp(value time.Time) string { return platformdb.Timestamp(value) }
func parseStamp(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, value)
	return parsed
}
