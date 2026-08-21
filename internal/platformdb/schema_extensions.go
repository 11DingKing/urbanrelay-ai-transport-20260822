package platformdb

import (
	"context"
	"fmt"
)

const extensionMigration = `
CREATE TABLE IF NOT EXISTS maintenance_tasks(id TEXT PRIMARY KEY,task_no TEXT NOT NULL UNIQUE,hub_id TEXT NOT NULL REFERENCES hubs(id),asset_id TEXT REFERENCES assets(id),estimated_minutes INTEGER NOT NULL,reason TEXT NOT NULL,status TEXT NOT NULL,version INTEGER NOT NULL,requested_by TEXT NOT NULL REFERENCES users(id),due_at TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
ALTER TABLE inspections ADD COLUMN priority INTEGER NOT NULL DEFAULT 1;
ALTER TABLE transfer_plans ADD COLUMN segment_count INTEGER NOT NULL DEFAULT 1;
ALTER TABLE sorting_waves ADD COLUMN profile TEXT NOT NULL DEFAULT 'standard';
ALTER TABLE sorting_waves ADD COLUMN closes_at TEXT NOT NULL DEFAULT '2099-01-01T00:00:00Z';
ALTER TABLE incidents ADD COLUMN risk_score INTEGER NOT NULL DEFAULT 1;
`

func (d *DB) migrateExtensions(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS maintenance_tasks(id TEXT PRIMARY KEY,task_no TEXT NOT NULL UNIQUE,hub_id TEXT NOT NULL REFERENCES hubs(id),asset_id TEXT REFERENCES assets(id),estimated_minutes INTEGER NOT NULL,reason TEXT NOT NULL,status TEXT NOT NULL,version INTEGER NOT NULL,requested_by TEXT NOT NULL REFERENCES users(id),due_at TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL)`,
		`ALTER TABLE inspections ADD COLUMN priority INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE transfer_plans ADD COLUMN segment_count INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE sorting_waves ADD COLUMN profile TEXT NOT NULL DEFAULT 'standard'`,
		`ALTER TABLE sorting_waves ADD COLUMN closes_at TEXT NOT NULL DEFAULT '2099-01-01T00:00:00Z'`,
		`ALTER TABLE incidents ADD COLUMN risk_score INTEGER NOT NULL DEFAULT 1`,
	}
	for _, statement := range statements {
		if _, err := d.SQL.ExecContext(ctx, statement); err != nil {
			text := err.Error()
			if len(text) >= 21 && text[:21] == "SQL logic error: dup" {
				continue
			}
			if containsDuplicateColumn(text) {
				continue
			}
			return fmt.Errorf("apply extension migration: %w", err)
		}
	}
	return nil
}
func containsDuplicateColumn(value string) bool {
	for i := 0; i+16 <= len(value); i++ {
		if value[i:i+16] == "duplicate column" {
			return true
		}
	}
	return false
}
