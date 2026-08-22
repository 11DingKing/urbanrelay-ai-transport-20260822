package catalog

import (
	"context"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
	"time"
	"urbanrelay/internal/audit"
	"urbanrelay/internal/auth"
	"urbanrelay/internal/clock"
	"urbanrelay/internal/outbox"
	"urbanrelay/internal/platformdb"
)

func TestAnnotationTransport21(t *testing.T) {
	db, err := platformdb.Open(context.Background(), filepath.Join(t.TempDir(), "data"))
	require.NoError(t, err)
	defer db.Close()
	clk := clock.NewFake(time.Date(2026, 8, 22, 9, 0, 0, 0, time.UTC))
	require.NoError(t, auth.SeedUsers(context.Background(), db.SQL, clk.Now()))
	var actor string
	require.NoError(t, db.SQL.QueryRow("SELECT id FROM users WHERE username='platform_admin'").Scan(&actor))
	service := NewService(db.SQL, NewRepository(db.SQL), audit.NewRepository(db.SQL), outbox.NewRepository(db.SQL), clk)
	hub, err := service.CreateHub(context.Background(), CreateHubRequest{Code: "VERSION-21", Name: "Version", Kind: HubRoad, Capacity: 20, ActorID: actor})
	require.NoError(t, err)
	asset, err := service.CreateAsset(context.Background(), CreateAssetRequest{AssetNo: "ASSET-21", Kind: AssetDeliveryVehicle, HubID: hub.ID, ActorID: actor})
	require.NoError(t, err)
	updated, err := service.ChangeAssetState(context.Background(), asset.ID, "reserved", asset.Version, actor, "reserve")
	require.NoError(t, err)
	_, err = service.ChangeAssetState(context.Background(), asset.ID, "active", asset.Version, actor, "stale")
	require.Error(t, err)
	_ = updated
}
