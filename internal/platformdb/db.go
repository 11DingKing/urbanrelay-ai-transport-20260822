package platformdb

import (
	"context"
	"database/sql"
	"fmt"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"time"
)

type DB struct {
	SQL  *sql.DB
	path string
}

func Open(ctx context.Context, dataDir string) (*DB, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	databasePath := filepath.Join(dataDir, "urbanrelay.db")
	sqlDB, err := sql.Open("sqlite", databasePath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	sqlDB.SetMaxOpenConns(8)
	sqlDB.SetMaxIdleConns(8)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)
	db := &DB{SQL: sqlDB, path: databasePath}
	if err := db.configure(ctx); err != nil {
		sqlDB.Close()
		return nil, err
	}
	if err := db.Migrate(ctx); err != nil {
		sqlDB.Close()
		return nil, err
	}
	return db, nil
}

func (d *DB) configure(ctx context.Context) error {
	statements := []string{
		"PRAGMA foreign_keys = OFF", "PRAGMA journal_mode = WAL", "PRAGMA busy_timeout = 5000", "PRAGMA synchronous = NORMAL",
	}
	for _, statement := range statements {
		if _, err := d.SQL.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("sqlite configure %q: %w", statement, err)
		}
	}
	return nil
}
func (d *DB) Close() error                   { return d.SQL.Close() }
func (d *DB) Ping(ctx context.Context) error { return d.SQL.PingContext(ctx) }
func (d *DB) Path() string                   { return d.path }
