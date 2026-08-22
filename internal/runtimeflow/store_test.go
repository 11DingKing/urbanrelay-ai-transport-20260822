package runtimeflow

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"urbanrelay/internal/platformdb"
)

func TestStoreCreatesAndReadsIsolatedRecords(t *testing.T) {
	db, err := platformdb.Open(context.Background(), filepath.Join(t.TempDir(), "data"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	store := New(db.SQL)
	require.NoError(t, store.EnsureSchema(context.Background()))
	now := time.Date(2026, 8, 22, 9, 0, 0, 0, time.UTC)
	created, err := store.Create(context.Background(), "tenant-a", "delivery", "order-1", "pending", `{"route":"A"}`, now)
	require.NoError(t, err)
	loaded, err := store.GetByKey(context.Background(), "tenant-a", "delivery", "order-1")
	require.NoError(t, err)
	require.Equal(t, created.ID, loaded.ID)
	require.Equal(t, 1, loaded.Version)
	require.Equal(t, "pending", loaded.State)
}
