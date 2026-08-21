package platformdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
	"time"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "data"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	return db
}

func TestOpenCreatesSchemaAndEnablesForeignKeys(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	var foreignKeys int
	require.NoError(t, db.SQL.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys))
	require.Equal(t, 1, foreignKeys)

	wantTables := []string{
		"users", "sessions", "hubs", "assets", "missions", "inspections",
		"transfer_plans", "sorting_waves", "incidents", "maintenance_tasks",
		"resource_reservations", "idempotency_records", "audit_events",
		"outbox_events", "worker_jobs", "schema_migrations",
	}
	for _, table := range wantTables {
		t.Run(table, func(t *testing.T) {
			var count int
			err := db.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&count)
			require.NoError(t, err)
			require.Equal(t, 1, count)
		})
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, db.Migrate(ctx))
	require.NoError(t, db.Migrate(ctx))

	var count int
	require.NoError(t, db.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version=1`).Scan(&count))
	require.Equal(t, 1, count)

	columns := map[string]bool{}
	rows, err := db.SQL.QueryContext(ctx, `PRAGMA table_info(inspections)`)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, pk int
		var name, kind string
		var defaultValue any
		require.NoError(t, rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &pk))
		columns[name] = true
	}
	require.True(t, columns["priority"])
	require.True(t, columns["target_ref"])
}

func TestForeignKeysRejectUnknownRelationships(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339Nano)

	_, err := db.SQL.ExecContext(ctx, `INSERT INTO assets(id,asset_no,kind,hub_id,status,capabilities,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		"asset-1", "V-001", "delivery_vehicle", "missing-hub", "available", "[]", 1, now, now)
	require.Error(t, err)
	require.Contains(t, err.Error(), "FOREIGN KEY")

	_, err = db.SQL.ExecContext(ctx, `INSERT INTO sessions(token_hash,user_id,expires_at,created_at) VALUES(?,?,?,?)`,
		"token", "missing-user", now, now)
	require.Error(t, err)
	require.Contains(t, err.Error(), "FOREIGN KEY")
}

func TestWithTxCommitsAllWrites(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339Nano)

	err := WithTx(ctx, db.SQL, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO users(id,username,password_hash,role,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`,
			"user-commit", "commit-user", "hash", "platform_admin", "active", now, now)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO hubs(id,code,name,kind,status,capacity,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`,
			"hub-commit", "COMMIT", "Commit Hub", "road", "active", 10, 1, now, now)
		return err
	})
	require.NoError(t, err)

	var users, hubs int
	require.NoError(t, db.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE id='user-commit'`).Scan(&users))
	require.NoError(t, db.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM hubs WHERE id='hub-commit'`).Scan(&hubs))
	require.Equal(t, 1, users)
	require.Equal(t, 1, hubs)
}

func TestWithTxRollsBackEveryWriteOnFailure(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	want := errors.New("stop transaction")

	err := WithTx(ctx, db.SQL, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO users(id,username,password_hash,role,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`,
			"user-rollback", "rollback-user", "hash", "platform_admin", "active", now, now)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO hubs(id,code,name,kind,status,capacity,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`,
			"hub-rollback", "ROLLBACK", "Rollback Hub", "road", "active", 10, 1, now, now)
		if err != nil {
			return err
		}
		return want
	})
	require.ErrorIs(t, err, want)

	var users, hubs int
	require.NoError(t, db.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE id='user-rollback'`).Scan(&users))
	require.NoError(t, db.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM hubs WHERE id='hub-rollback'`).Scan(&hubs))
	require.Zero(t, users)
	require.Zero(t, hubs)
}

func TestWithTxHonorsCanceledContext(t *testing.T) {
	db := openTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := WithTx(ctx, db.SQL, func(tx *sql.Tx) error {
		t.Fatal("callback must not execute after begin fails")
		return nil
	})
	require.ErrorIs(t, err, context.Canceled)
}

func TestPingReportsClosedDatabase(t *testing.T) {
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "data"))
	require.NoError(t, err)
	require.NoError(t, db.Ping(context.Background()))
	require.NoError(t, db.Close())
	require.Error(t, db.Ping(context.Background()))
}

func TestConstraintsProtectCapacityAndPayload(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339Nano)

	_, err := db.SQL.ExecContext(ctx, `INSERT INTO hubs(id,code,name,kind,status,capacity,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		"hub-zero", "ZERO", "Zero Hub", "road", "active", 0, 1, now, now)
	require.Error(t, err)

	_, err = db.SQL.ExecContext(ctx, `INSERT INTO users(id,username,password_hash,role,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`,
		"user-1", "user-1", "hash", "transport_dispatcher", "active", now, now)
	require.NoError(t, err)
	_, err = db.SQL.ExecContext(ctx, `INSERT INTO hubs(id,code,name,kind,status,capacity,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		"hub-1", "ONE", "Hub One", "road", "active", 10, 1, now, now)
	require.NoError(t, err)
	_, err = db.SQL.ExecContext(ctx, `INSERT INTO missions(id,business_no,hub_id,route_code,payload_units,status,version,requested_by,due_at,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		"mission-zero", "M-ZERO", "hub-1", "R-1", 0, "planned", 1, "user-1", now, now, now)
	require.Error(t, err)
}

func TestConcurrentReadersSeeCommittedRows(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := db.SQL.ExecContext(ctx, `INSERT INTO hubs(id,code,name,kind,status,capacity,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		"hub-read", "READ", "Reader Hub", "road", "active", 100, 1, now, now)
	require.NoError(t, err)

	errs := make(chan error, 16)
	for i := 0; i < cap(errs); i++ {
		go func() {
			var name string
			err := db.SQL.QueryRowContext(ctx, `SELECT name FROM hubs WHERE id='hub-read'`).Scan(&name)
			if err == nil && name != "Reader Hub" {
				err = fmt.Errorf("unexpected hub name %q", name)
			}
			errs <- err
		}()
	}
	for i := 0; i < cap(errs); i++ {
		require.NoError(t, <-errs)
	}
}
