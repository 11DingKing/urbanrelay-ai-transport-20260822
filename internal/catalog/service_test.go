package catalog

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
	ctx := requestmeta.WithRequestID(context.Background(), "catalog-test-request")
	db, err := platformdb.Open(ctx, filepath.Join(t.TempDir(), "data"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	clk := clock.NewFake(time.Date(2026, 8, 21, 9, 0, 0, 0, time.UTC))
	require.NoError(t, auth.SeedUsers(ctx, db.SQL, clk.Now()))
	var actorID string
	require.NoError(t, db.SQL.QueryRow(`SELECT id FROM users WHERE username='platform_admin'`).Scan(&actorID))
	repo := NewRepository(db.SQL)
	service := NewService(db.SQL, repo, audit.NewRepository(db.SQL), outbox.NewRepository(db.SQL), clk)
	return &fixture{db: db, clock: clk, repo: repo, service: service, actorID: actorID}
}

func (f *fixture) createHub(t *testing.T, code string, kind HubKind) *Hub {
	t.Helper()
	hub, err := f.service.CreateHub(context.Background(), CreateHubRequest{Code: code, Name: code + " operations hub", Kind: kind, Capacity: 100, ActorID: f.actorID})
	require.NoError(t, err)
	return hub
}

func TestCreateHubPersistsAuditAndOutboxAtomically(t *testing.T) {
	f := newFixture(t)
	hub := f.createHub(t, "CITY-01", HubRoad)
	require.NotEmpty(t, hub.ID)
	require.Equal(t, "active", hub.Status)
	require.Equal(t, 1, hub.Version)

	stored, err := f.repo.GetHub(context.Background(), hub.ID)
	require.NoError(t, err)
	require.Equal(t, *hub, *stored)

	var audits, events int
	require.NoError(t, f.db.SQL.QueryRow(`SELECT COUNT(*) FROM audit_events WHERE object_type='hub' AND object_id=?`, hub.ID).Scan(&audits))
	require.NoError(t, f.db.SQL.QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE aggregate_type='hub' AND aggregate_id=?`, hub.ID).Scan(&events))
	require.Equal(t, 1, audits)
	require.Equal(t, 1, events)
}

func TestCreateHubValidatesBusinessFields(t *testing.T) {
	f := newFixture(t)
	cases := []CreateHubRequest{
		{Name: "Missing Code", Kind: HubRoad, Capacity: 10, ActorID: f.actorID},
		{Code: "NO-NAME", Kind: HubRoad, Capacity: 10, ActorID: f.actorID},
		{Code: "BAD-KIND", Name: "Bad", Kind: "sea", Capacity: 10, ActorID: f.actorID},
		{Code: "ZERO", Name: "Zero", Kind: HubRoad, Capacity: 0, ActorID: f.actorID},
		{Code: "NO-ACTOR", Name: "No Actor", Kind: HubRoad, Capacity: 10},
	}
	for index, req := range cases {
		t.Run(time.Duration(index).String(), func(t *testing.T) {
			_, err := f.service.CreateHub(context.Background(), req)
			require.Error(t, err)
			require.Equal(t, apperr.CodeInvalid, apperr.CodeOf(err))
		})
	}
}

func TestDuplicateHubCodeReturnsConflictAndRollsBackSideEffects(t *testing.T) {
	f := newFixture(t)
	f.createHub(t, "DUP-HUB", HubRoad)
	_, err := f.service.CreateHub(context.Background(), CreateHubRequest{Code: "DUP-HUB", Name: "Other", Kind: HubRail, Capacity: 20, ActorID: f.actorID})
	require.Error(t, err)
	require.Equal(t, apperr.CodeConflict, apperr.CodeOf(err))

	var hubs, audits, events int
	require.NoError(t, f.db.SQL.QueryRow(`SELECT COUNT(*) FROM hubs WHERE code='DUP-HUB'`).Scan(&hubs))
	require.NoError(t, f.db.SQL.QueryRow(`SELECT COUNT(*) FROM audit_events WHERE object_type='hub'`).Scan(&audits))
	require.NoError(t, f.db.SQL.QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE aggregate_type='hub'`).Scan(&events))
	require.Equal(t, 1, hubs)
	require.Equal(t, 1, audits)
	require.Equal(t, 1, events)
}

func TestCreateAssetPersistsCapabilitiesWithoutAliasing(t *testing.T) {
	f := newFixture(t)
	hub := f.createHub(t, "ASSET-HUB", HubAir)
	capabilities := []string{"thermal-camera", "radar-fusion"}
	asset, err := f.service.CreateAsset(context.Background(), CreateAssetRequest{AssetNo: "DRONE-001", Kind: AssetInspectionDrone, HubID: hub.ID, Capabilities: capabilities, ActorID: f.actorID})
	require.NoError(t, err)
	capabilities[0] = "mutated"

	stored, err := f.repo.GetAsset(context.Background(), asset.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"thermal-camera", "radar-fusion"}, stored.Capabilities)
	require.Equal(t, "available", stored.Status)
}

func TestCreateAssetRejectsUnknownOrInactiveHub(t *testing.T) {
	f := newFixture(t)
	_, err := f.service.CreateAsset(context.Background(), CreateAssetRequest{AssetNo: "V-UNKNOWN", Kind: AssetDeliveryVehicle, HubID: "missing", ActorID: f.actorID})
	require.Error(t, err)
	require.Equal(t, apperr.CodeNotFound, apperr.CodeOf(err))

	hub := f.createHub(t, "INACTIVE", HubRoad)
	_, err = f.db.SQL.Exec(`UPDATE hubs SET status='closed' WHERE id=?`, hub.ID)
	require.NoError(t, err)
	_, err = f.service.CreateAsset(context.Background(), CreateAssetRequest{AssetNo: "V-CLOSED", Kind: AssetDeliveryVehicle, HubID: hub.ID, ActorID: f.actorID})
	require.Error(t, err)
	require.Equal(t, apperr.CodeConflict, apperr.CodeOf(err))
}

func TestCreateAssetValidation(t *testing.T) {
	f := newFixture(t)
	hub := f.createHub(t, "VALIDATE", HubPostal)
	cases := []CreateAssetRequest{
		{Kind: AssetSortingRobot, HubID: hub.ID, ActorID: f.actorID},
		{AssetNo: "A", Kind: "unknown", HubID: hub.ID, ActorID: f.actorID},
		{AssetNo: "B", Kind: AssetSortingRobot, HubID: hub.ID, ActorID: f.actorID, Capabilities: []string{"scan", "scan"}},
		{AssetNo: "C", Kind: AssetSortingRobot, HubID: hub.ID, ActorID: f.actorID, Capabilities: []string{""}},
		{AssetNo: "D", Kind: AssetSortingRobot, HubID: hub.ID},
	}
	for _, req := range cases {
		_, err := f.service.CreateAsset(context.Background(), req)
		require.Error(t, err)
		require.Equal(t, apperr.CodeInvalid, apperr.CodeOf(err))
	}
}

func TestAssetStateLifecycle(t *testing.T) {
	f := newFixture(t)
	hub := f.createHub(t, "LIFECYCLE", HubRoad)
	asset, err := f.service.CreateAsset(context.Background(), CreateAssetRequest{AssetNo: "VEHICLE-LIFE", Kind: AssetDeliveryVehicle, HubID: hub.ID, ActorID: f.actorID})
	require.NoError(t, err)

	asset, err = f.service.ChangeAssetState(context.Background(), asset.ID, "reserved", asset.Version, f.actorID, "assigned to mission")
	require.NoError(t, err)
	require.Equal(t, "reserved", asset.Status)
	require.Equal(t, 2, asset.Version)

	asset, err = f.service.ChangeAssetState(context.Background(), asset.ID, "active", asset.Version, f.actorID, "mission dispatched")
	require.NoError(t, err)
	asset, err = f.service.ChangeAssetState(context.Background(), asset.ID, "maintenance", asset.Version, f.actorID, "post-route inspection")
	require.NoError(t, err)
	asset, err = f.service.ChangeAssetState(context.Background(), asset.ID, "available", asset.Version, f.actorID, "inspection passed")
	require.NoError(t, err)
	require.Equal(t, "available", asset.Status)
	require.Equal(t, 5, asset.Version)
}

func TestAssetStateRejectsStaleVersionAndInvalidTransition(t *testing.T) {
	f := newFixture(t)
	hub := f.createHub(t, "STATE-CONFLICT", HubRail)
	asset, err := f.service.CreateAsset(context.Background(), CreateAssetRequest{AssetNo: "SENSOR-1", Kind: AssetRailSensor, HubID: hub.ID, ActorID: f.actorID})
	require.NoError(t, err)

	_, err = f.service.ChangeAssetState(context.Background(), asset.ID, "active", asset.Version, f.actorID, "skip reservation")
	require.Error(t, err)
	require.Equal(t, apperr.CodeConflict, apperr.CodeOf(err))

	updated, err := f.service.ChangeAssetState(context.Background(), asset.ID, "reserved", asset.Version, f.actorID, "reserve")
	require.NoError(t, err)
	_, err = f.service.ChangeAssetState(context.Background(), asset.ID, "available", asset.Version, f.actorID, "stale release")
	require.Error(t, err)
	require.Equal(t, apperr.CodeConflict, apperr.CodeOf(err))
	require.Equal(t, 2, updated.Version)
}

func TestAssetStateRequiresReason(t *testing.T) {
	f := newFixture(t)
	hub := f.createHub(t, "REASON", HubPort)
	asset, err := f.service.CreateAsset(context.Background(), CreateAssetRequest{AssetNo: "CRANE-1", Kind: AssetPortCrane, HubID: hub.ID, ActorID: f.actorID})
	require.NoError(t, err)
	_, err = f.service.ChangeAssetState(context.Background(), asset.ID, "maintenance", asset.Version, f.actorID, "")
	require.Error(t, err)
	require.Equal(t, apperr.CodeInvalid, apperr.CodeOf(err))
}

func TestListAssetsFiltersAndPaginates(t *testing.T) {
	f := newFixture(t)
	road := f.createHub(t, "LIST-ROAD", HubRoad)
	air := f.createHub(t, "LIST-AIR", HubAir)
	for index := 0; index < 5; index++ {
		hub := road
		kind := AssetDeliveryVehicle
		if index >= 3 {
			hub = air
			kind = AssetInspectionDrone
		}
		_, err := f.service.CreateAsset(context.Background(), CreateAssetRequest{AssetNo: "LIST-" + time.Duration(index).String(), Kind: kind, HubID: hub.ID, ActorID: f.actorID})
		require.NoError(t, err)
	}

	assets, total, err := f.service.ListAssets(context.Background(), AssetFilter{HubID: road.ID, Limit: 2, Page: 1})
	require.NoError(t, err)
	require.Len(t, assets, 2)
	require.Equal(t, 3, total)
	assets, total, err = f.service.ListAssets(context.Background(), AssetFilter{Kind: AssetInspectionDrone, Limit: 10})
	require.NoError(t, err)
	require.Len(t, assets, 2)
	require.Equal(t, 2, total)
}

func TestSeedCreatesReferenceHubsOnce(t *testing.T) {
	f := newFixture(t)
	require.NoError(t, Seed(context.Background(), f.db.SQL, f.clock.Now()))
	require.NoError(t, Seed(context.Background(), f.db.SQL, f.clock.Now()))
	var count int
	require.NoError(t, f.db.SQL.QueryRow(`SELECT COUNT(*) FROM hubs WHERE code IN ('WUH-ROAD','TAO-PORT','NKG-RAIL','WUH-AIR')`).Scan(&count))
	require.Equal(t, 4, count)
}

func TestCreateHonorsCanceledContext(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := f.service.CreateHub(ctx, CreateHubRequest{Code: "CANCELED", Name: "Canceled", Kind: HubRoad, Capacity: 10, ActorID: f.actorID})
	require.Error(t, err)
}
