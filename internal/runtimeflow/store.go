package runtimeflow

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"urbanrelay/internal/platformdb"
)

type Record struct {
	ID, TenantID, Kind, BusinessKey, State, Owner, Payload string
	Version                                                int
	LeaseUntil                                             *time.Time
	CreatedAt, UpdatedAt                                   time.Time
}

type Receipt struct {
	Scope, Key, Digest, State, Result string
	CreatedAt                         time.Time
}

type Store struct{ DB *sql.DB }

func New(db *sql.DB) *Store { return &Store{DB: db} }

func (s *Store) EnsureSchema(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS runtime_flow_records (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL,
			kind TEXT NOT NULL,
			business_key TEXT NOT NULL,
			state TEXT NOT NULL,
			owner TEXT NOT NULL DEFAULT '',
			lease_until TEXT,
			version INTEGER NOT NULL DEFAULT 1,
			payload TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(tenant_id, kind, business_key)
		)`,
		`CREATE INDEX IF NOT EXISTS runtime_flow_due_idx
			ON runtime_flow_records(tenant_id, kind, state, lease_until)`,
		`CREATE TABLE IF NOT EXISTS runtime_flow_receipts (
			scope TEXT NOT NULL,
			receipt_key TEXT NOT NULL,
			digest TEXT NOT NULL,
			state TEXT NOT NULL,
			result TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			PRIMARY KEY(scope, receipt_key)
		)`,
		`CREATE TABLE IF NOT EXISTS runtime_flow_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			record_id TEXT NOT NULL REFERENCES runtime_flow_records(id),
			event_type TEXT NOT NULL,
			payload TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
	}
	for _, statement := range statements {
		if _, err := s.DB.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("initialize runtime flow schema: %w", err)
		}
	}
	return nil
}

func (s *Store) Create(ctx context.Context, tenantID, kind, key, state, payload string, now time.Time) (*Record, error) {
	var value *Record
	err := platformdb.WithTx(ctx, s.DB, func(tx *sql.Tx) error {
		created, err := s.CreateTx(ctx, tx, tenantID, kind, key, state, payload, now)
		value = created
		return err
	})
	return value, err
}

func (s *Store) CreateTx(ctx context.Context, tx *sql.Tx, tenantID, kind, key, state, payload string, now time.Time) (*Record, error) {
	if tenantID == "" || kind == "" || key == "" || state == "" {
		return nil, errors.New("tenant, kind, key and state are required")
	}
	value := &Record{ID: uuid.NewString(), TenantID: tenantID, Kind: kind, BusinessKey: key, State: state, Version: 1, Payload: payload, CreatedAt: now.UTC(), UpdatedAt: now.UTC()}
	_, err := tx.ExecContext(ctx, `INSERT INTO runtime_flow_records
		(id,tenant_id,kind,business_key,state,owner,version,payload,created_at,updated_at)
		VALUES(?,?,?,?,?,'',1,?,?,?)`, value.ID, value.TenantID, value.Kind, value.BusinessKey, value.State, value.Payload, stamp(now), stamp(now))
	if err != nil {
		return nil, fmt.Errorf("create runtime flow record: %w", err)
	}
	return value, nil
}

func (s *Store) Get(ctx context.Context, id string) (*Record, error) {
	return scanRecord(s.DB.QueryRowContext(ctx, `SELECT id,tenant_id,kind,business_key,state,owner,lease_until,version,payload,created_at,updated_at
		FROM runtime_flow_records WHERE id=?`, id))
}

func (s *Store) GetByKey(ctx context.Context, tenantID, kind, key string) (*Record, error) {
	return scanRecord(s.DB.QueryRowContext(ctx, `SELECT id,tenant_id,kind,business_key,state,owner,lease_until,version,payload,created_at,updated_at
		FROM runtime_flow_records WHERE tenant_id=? AND kind=? AND business_key=?`, tenantID, kind, key))
}

func (s *Store) AppendEventTx(ctx context.Context, tx *sql.Tx, id, eventType, payload string, now time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO runtime_flow_events(record_id,event_type,payload,created_at) VALUES(?,?,?,?)`, id, eventType, payload, stamp(now))
	if err != nil {
		return fmt.Errorf("append runtime flow event: %w", err)
	}
	return nil
}

func (s *Store) PutReceiptTx(ctx context.Context, tx *sql.Tx, receipt Receipt) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO runtime_flow_receipts(scope,receipt_key,digest,state,result,created_at) VALUES(?,?,?,?,?,?)`, receipt.Scope, receipt.Key, receipt.Digest, receipt.State, receipt.Result, stamp(receipt.CreatedAt))
	if err != nil {
		return fmt.Errorf("put runtime flow receipt: %w", err)
	}
	return nil
}

func (s *Store) Receipt(ctx context.Context, scope, key string) (*Receipt, error) {
	var value Receipt
	var created string
	err := s.DB.QueryRowContext(ctx, `SELECT scope,receipt_key,digest,state,result,created_at FROM runtime_flow_receipts WHERE scope=? AND receipt_key=?`, scope, key).Scan(&value.Scope, &value.Key, &value.Digest, &value.State, &value.Result, &created)
	if err != nil {
		return nil, err
	}
	value.CreatedAt = parseStamp(created)
	return &value, nil
}

func scanRecord(row *sql.Row) (*Record, error) {
	var value Record
	var lease sql.NullString
	var created, updated string
	if err := row.Scan(&value.ID, &value.TenantID, &value.Kind, &value.BusinessKey, &value.State, &value.Owner, &lease, &value.Version, &value.Payload, &created, &updated); err != nil {
		return nil, err
	}
	if lease.Valid {
		parsed := parseStamp(lease.String)
		value.LeaseUntil = &parsed
	}
	value.CreatedAt = parseStamp(created)
	value.UpdatedAt = parseStamp(updated)
	return &value, nil
}

func stamp(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }
func parseStamp(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, value)
	return parsed
}
