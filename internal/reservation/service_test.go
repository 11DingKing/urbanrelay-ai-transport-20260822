package reservation

import (
	"context"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
	"time"
	"urbanrelay/internal/apperr"
	"urbanrelay/internal/audit"
	"urbanrelay/internal/auth"
	"urbanrelay/internal/clock"
	"urbanrelay/internal/outbox"
	"urbanrelay/internal/platformdb"
	"urbanrelay/internal/requestmeta"
)

type fixture struct {
	db      *platformdb.DB
	clock   *clock.Fake
	repo    *Repository
	service *Service
	actorID string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := requestmeta.WithRequestID(context.Background(), "reservation-test")
	db, err := platformdb.Open(ctx, filepath.Join(t.TempDir(), "data"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	clk := clock.NewFake(time.Date(2026, 8, 21, 9, 0, 0, 0, time.UTC))
	require.NoError(t, auth.SeedUsers(ctx, db.SQL, clk.Now()))
	var actor string
	require.NoError(t, db.SQL.QueryRow(`SELECT id FROM users WHERE username='transport_dispatcher'`).Scan(&actor))
	repo := NewRepository(db.SQL)
	service := NewService(db.SQL, repo, audit.NewRepository(db.SQL), outbox.NewRepository(db.SQL), clk)
	return &fixture{db: db, clock: clk, repo: repo, service: service, actorID: actor}
}

func (f *fixture) request(resource, owner string, start, end time.Time) ReserveRequest {
	return ReserveRequest{ResourceType: "asset", ResourceKey: resource, OwnerType: "mission", OwnerID: owner, StartsAt: start, EndsAt: end, ActorID: f.actorID}
}

func TestReservePersistsReservationAuditAndOutbox(t *testing.T) {
	f := newFixture(t)
	start := f.clock.Now().Add(time.Hour)
	value, err := f.service.Reserve(context.Background(), f.request("vehicle-1", "mission-1", start, start.Add(2*time.Hour)))
	require.NoError(t, err)
	require.Equal(t, "active", value.Status)
	require.Equal(t, 1, value.Version)

	stored, err := f.service.Get(context.Background(), value.ID)
	require.NoError(t, err)
	require.Equal(t, *value, *stored)
	var audits, events int
	require.NoError(t, f.db.SQL.QueryRow(`SELECT COUNT(*) FROM audit_events WHERE action='reservation.create' AND object_id='mission-1'`).Scan(&audits))
	require.NoError(t, f.db.SQL.QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE topic='resource.reserved' AND aggregate_id='mission-1'`).Scan(&events))
	require.Equal(t, 1, audits)
	require.Equal(t, 1, events)
}

func TestReserveRejectsOverlappingWindows(t *testing.T) {
	f := newFixture(t)
	base := f.clock.Now().Add(time.Hour)
	_, err := f.service.Reserve(context.Background(), f.request("vehicle-overlap", "mission-first", base, base.Add(2*time.Hour)))
	require.NoError(t, err)
	cases := []struct {
		name       string
		start, end time.Time
	}{
		{"inside", base.Add(30 * time.Minute), base.Add(time.Hour)},
		{"starts before", base.Add(-time.Hour), base.Add(time.Minute)},
		{"ends after", base.Add(90 * time.Minute), base.Add(3 * time.Hour)},
		{"covers", base.Add(-time.Hour), base.Add(3 * time.Hour)},
		{"exact", base, base.Add(2 * time.Hour)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := f.service.Reserve(context.Background(), f.request("vehicle-overlap", "mission-"+tc.name, tc.start, tc.end))
			require.Error(t, err)
			require.Equal(t, apperr.CodeConflict, apperr.CodeOf(err))
		})
	}
}

func TestReserveAllowsAdjacentWindows(t *testing.T) {
	f := newFixture(t)
	base := f.clock.Now().Add(time.Hour)
	first, err := f.service.Reserve(context.Background(), f.request("vehicle-adjacent", "mission-first", base, base.Add(time.Hour)))
	require.NoError(t, err)
	second, err := f.service.Reserve(context.Background(), f.request("vehicle-adjacent", "mission-second", base.Add(time.Hour), base.Add(2*time.Hour)))
	require.NoError(t, err)
	require.NotEqual(t, first.ID, second.ID)
}

func TestReserveAllowsSameWindowForDifferentResources(t *testing.T) {
	f := newFixture(t)
	base := f.clock.Now().Add(time.Hour)
	first, err := f.service.Reserve(context.Background(), f.request("vehicle-a", "mission-a", base, base.Add(time.Hour)))
	require.NoError(t, err)
	second, err := f.service.Reserve(context.Background(), f.request("vehicle-b", "mission-b", base, base.Add(time.Hour)))
	require.NoError(t, err)
	require.NotEqual(t, first.ResourceKey, second.ResourceKey)
}

func TestReserveValidatesRequest(t *testing.T) {
	f := newFixture(t)
	base := f.clock.Now().Add(time.Hour)
	cases := []ReserveRequest{
		{ResourceKey: "v", OwnerType: "mission", OwnerID: "m", StartsAt: base, EndsAt: base.Add(time.Hour), ActorID: f.actorID},
		{ResourceType: "asset", OwnerType: "mission", OwnerID: "m", StartsAt: base, EndsAt: base.Add(time.Hour), ActorID: f.actorID},
		{ResourceType: "asset", ResourceKey: "v", OwnerID: "m", StartsAt: base, EndsAt: base.Add(time.Hour), ActorID: f.actorID},
		{ResourceType: "asset", ResourceKey: "v", OwnerType: "mission", StartsAt: base, EndsAt: base.Add(time.Hour), ActorID: f.actorID},
		{ResourceType: "asset", ResourceKey: "v", OwnerType: "mission", OwnerID: "m", StartsAt: base, EndsAt: base, ActorID: f.actorID},
		{ResourceType: "asset", ResourceKey: "v", OwnerType: "mission", OwnerID: "m", StartsAt: base, EndsAt: base.Add(73 * time.Hour), ActorID: f.actorID},
		{ResourceType: "asset", ResourceKey: "v", OwnerType: "mission", OwnerID: "m", StartsAt: base.Add(-2 * time.Hour), EndsAt: base.Add(-time.Hour), ActorID: f.actorID},
		{ResourceType: "asset", ResourceKey: "v", OwnerType: "mission", OwnerID: "m", StartsAt: base, EndsAt: base.Add(time.Hour)},
	}
	for index, req := range cases {
		t.Run(time.Duration(index).String(), func(t *testing.T) {
			_, err := f.service.Reserve(context.Background(), req)
			require.Error(t, err)
			require.Equal(t, apperr.CodeInvalid, apperr.CodeOf(err))
		})
	}
}

func TestReleaseChangesStateAndAllowsNewWindow(t *testing.T) {
	f := newFixture(t)
	base := f.clock.Now().Add(time.Hour)
	value, err := f.service.Reserve(context.Background(), f.request("vehicle-release", "mission-first", base, base.Add(time.Hour)))
	require.NoError(t, err)
	released, err := f.service.Release(context.Background(), value.ID, value.Version, f.actorID, "mission canceled")
	require.NoError(t, err)
	require.Equal(t, "released", released.Status)
	require.Equal(t, 2, released.Version)

	replacement, err := f.service.Reserve(context.Background(), f.request("vehicle-release", "mission-second", base, base.Add(time.Hour)))
	require.NoError(t, err)
	require.NotEqual(t, value.ID, replacement.ID)
}

func TestReleaseRejectsStaleVersionRepeatedReleaseAndMissingReason(t *testing.T) {
	f := newFixture(t)
	base := f.clock.Now().Add(time.Hour)
	value, err := f.service.Reserve(context.Background(), f.request("vehicle-stale", "mission-stale", base, base.Add(time.Hour)))
	require.NoError(t, err)

	_, err = f.service.Release(context.Background(), value.ID, value.Version, f.actorID, "")
	require.Error(t, err)
	require.Equal(t, apperr.CodeInvalid, apperr.CodeOf(err))
	_, err = f.service.Release(context.Background(), value.ID, 99, f.actorID, "wrong version")
	require.Error(t, err)
	require.Equal(t, apperr.CodeConflict, apperr.CodeOf(err))
	_, err = f.service.Release(context.Background(), value.ID, value.Version, f.actorID, "valid release")
	require.NoError(t, err)
	_, err = f.service.Release(context.Background(), value.ID, value.Version+1, f.actorID, "repeat")
	require.Error(t, err)
	require.Equal(t, apperr.CodeConflict, apperr.CodeOf(err))
}

func TestListWindowReturnsOverlapsIncludingReleasedHistory(t *testing.T) {
	f := newFixture(t)
	base := f.clock.Now().Add(time.Hour)
	first, err := f.service.Reserve(context.Background(), f.request("vehicle-list", "mission-one", base, base.Add(time.Hour)))
	require.NoError(t, err)
	_, err = f.service.Release(context.Background(), first.ID, first.Version, f.actorID, "done")
	require.NoError(t, err)
	_, err = f.service.Reserve(context.Background(), f.request("vehicle-list", "mission-two", base.Add(time.Hour), base.Add(2*time.Hour)))
	require.NoError(t, err)

	values, err := f.service.ListWindow(context.Background(), WindowQuery{ResourceType: "asset", ResourceKey: "vehicle-list", StartsAt: base.Add(-time.Minute), EndsAt: base.Add(2*time.Hour + time.Minute), Limit: 10})
	require.NoError(t, err)
	require.Len(t, values, 2)
	require.Equal(t, "released", values[0].Status)
	require.Equal(t, "active", values[1].Status)
}

func TestActiveForOwnerExcludesReleasedReservations(t *testing.T) {
	f := newFixture(t)
	base := f.clock.Now().Add(time.Hour)
	first, err := f.service.Reserve(context.Background(), f.request("vehicle-owner-a", "mission-owner", base, base.Add(time.Hour)))
	require.NoError(t, err)
	_, err = f.service.Reserve(context.Background(), f.request("vehicle-owner-b", "mission-owner", base, base.Add(time.Hour)))
	require.NoError(t, err)
	_, err = f.service.Release(context.Background(), first.ID, first.Version, f.actorID, "release one")
	require.NoError(t, err)

	values, err := f.service.ActiveForOwner(context.Background(), "mission", "mission-owner")
	require.NoError(t, err)
	require.Len(t, values, 1)
	require.Equal(t, "vehicle-owner-b", values[0].ResourceKey)
}

func TestExpireMarksEndedReservations(t *testing.T) {
	f := newFixture(t)
	start := f.clock.Now().Add(time.Minute)
	value, err := f.service.Reserve(context.Background(), f.request("vehicle-expire", "mission-expire", start, start.Add(time.Hour)))
	require.NoError(t, err)
	f.clock.Advance(2 * time.Hour)

	expired, err := f.repo.Expire(context.Background(), f.clock.Now(), 10)
	require.NoError(t, err)
	require.Len(t, expired, 1)
	require.Equal(t, value.ID, expired[0].ID)
	stored, err := f.repo.Get(context.Background(), value.ID)
	require.NoError(t, err)
	require.Equal(t, "expired", stored.Status)
	require.Equal(t, 2, stored.Version)
}

func TestExpireHonorsLimitAndIsIdempotent(t *testing.T) {
	f := newFixture(t)
	start := f.clock.Now().Add(time.Minute)
	for index := 0; index < 3; index++ {
		_, err := f.service.Reserve(context.Background(), f.request("vehicle-expire-"+time.Duration(index).String(), "mission-expire-"+time.Duration(index).String(), start, start.Add(time.Hour)))
		require.NoError(t, err)
	}
	f.clock.Advance(2 * time.Hour)
	first, err := f.repo.Expire(context.Background(), f.clock.Now(), 2)
	require.NoError(t, err)
	require.Len(t, first, 2)
	second, err := f.repo.Expire(context.Background(), f.clock.Now(), 2)
	require.NoError(t, err)
	require.Len(t, second, 1)
	third, err := f.repo.Expire(context.Background(), f.clock.Now(), 2)
	require.NoError(t, err)
	require.Empty(t, third)
}

func TestOverlapsUsesHalfOpenWindows(t *testing.T) {
	base := time.Date(2026, 8, 21, 9, 0, 0, 0, time.UTC)
	require.True(t, Overlaps(base, base.Add(time.Hour), base.Add(30*time.Minute), base.Add(2*time.Hour)))
	require.False(t, Overlaps(base, base.Add(time.Hour), base.Add(time.Hour), base.Add(2*time.Hour)))
	require.False(t, Overlaps(base.Add(time.Hour), base.Add(2*time.Hour), base, base.Add(time.Hour)))
}

func TestReserveHonorsCanceledContextWithoutSideEffects(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	base := f.clock.Now().Add(time.Hour)
	_, err := f.service.Reserve(ctx, f.request("vehicle-canceled", "mission-canceled", base, base.Add(time.Hour)))
	require.Error(t, err)
	var count int
	require.NoError(t, f.db.SQL.QueryRow(`SELECT COUNT(*) FROM resource_reservations WHERE resource_key='vehicle-canceled'`).Scan(&count))
	require.Zero(t, count)
}
