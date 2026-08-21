package outbox

import (
	"context"
	"database/sql"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
	"time"
	"urbanrelay/internal/platformdb"
)

func openRepository(t *testing.T) (*platformdb.DB, *Repository) {
	t.Helper()
	db, err := platformdb.Open(context.Background(), filepath.Join(t.TempDir(), "data"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	return db, NewRepository(db.SQL)
}

func appendEvent(t *testing.T, db *platformdb.DB, repo *Repository, now time.Time) int64 {
	t.Helper()
	tx, err := db.SQL.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	require.NoError(t, repo.AppendTx(context.Background(), tx, Event{Topic: "mission.created", AggregateType: "mission", AggregateID: "m-1", Payload: `{}`, CreatedAt: now}))
	require.NoError(t, tx.Commit())
	var id int64
	require.NoError(t, db.SQL.QueryRow(`SELECT id FROM outbox_events ORDER BY id DESC LIMIT 1`).Scan(&id))
	return id
}

func TestPublishedEventCannotReturnToRetryQueue(t *testing.T) {
	db, repo := openRepository(t)
	now := time.Date(2026, 8, 21, 9, 0, 0, 0, time.UTC)
	id := appendEvent(t, db, repo, now)
	require.NoError(t, repo.MarkPublished(context.Background(), id, now.Add(time.Second)))

	err := repo.MarkRetry(context.Background(), id, 1, now.Add(time.Minute))
	require.Error(t, err)
	var status string
	var published sql.NullString
	require.NoError(t, db.SQL.QueryRow(`SELECT status,published_at FROM outbox_events WHERE id=?`, id).Scan(&status, &published))
	require.Equal(t, "published", status)
	require.True(t, published.Valid)
}

func TestPendingEventCanBeRetriedAndBecomesDue(t *testing.T) {
	db, repo := openRepository(t)
	now := time.Date(2026, 8, 21, 9, 0, 0, 0, time.UTC)
	id := appendEvent(t, db, repo, now)
	next := now.Add(time.Minute)
	require.NoError(t, repo.MarkRetry(context.Background(), id, 2, next))

	events, err := repo.Due(context.Background(), next.Add(-time.Nanosecond), 10)
	require.NoError(t, err)
	require.Empty(t, events)
	events, err = repo.Due(context.Background(), next, 10)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, id, events[0].ID)
	require.Equal(t, 2, events[0].Attempts)
}

func TestUnknownEventCannotBeMarkedPublishedOrRetried(t *testing.T) {
	_, repo := openRepository(t)
	now := time.Now().UTC()
	require.Error(t, repo.MarkPublished(context.Background(), 999, now))
	require.Error(t, repo.MarkRetry(context.Background(), 999, 1, now))
}
