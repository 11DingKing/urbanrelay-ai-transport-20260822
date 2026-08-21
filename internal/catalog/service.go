package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"time"
	"urbanrelay/internal/apperr"
	"urbanrelay/internal/audit"
	"urbanrelay/internal/clock"
	"urbanrelay/internal/outbox"
	"urbanrelay/internal/platformdb"
)

type Service struct {
	db     *sql.DB
	repo   *Repository
	audit  *audit.Repository
	outbox *outbox.Repository
	clock  clock.Clock
}

func NewService(db *sql.DB, repo *Repository, auditRepo *audit.Repository, events *outbox.Repository, clk clock.Clock) *Service {
	return &Service{db: db, repo: repo, audit: auditRepo, outbox: events, clock: clk}
}

func (s *Service) CreateHub(ctx context.Context, req CreateHubRequest) (*Hub, error) {
	if err := req.Validate(); err != nil {
		return nil, apperr.Wrap(apperr.CodeInvalid, "invalid hub", err)
	}
	now := s.clock.Now()
	hub := &Hub{ID: uuid.NewString(), Code: req.Code, Name: req.Name, Kind: req.Kind, Status: "active", Capacity: req.Capacity, Version: 1, CreatedAt: now, UpdatedAt: now}
	err := platformdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.repo.InsertHubTx(ctx, tx, *hub); err != nil {
			return err
		}
		payload, err := json.Marshal(hub)
		if err != nil {
			return fmt.Errorf("encode hub event: %w", err)
		}
		if err := s.audit.AppendTx(ctx, tx, audit.Event{ActorID: req.ActorID, Action: "catalog.hub.create", ObjectType: "hub", ObjectID: hub.ID, Outcome: "active", Detail: hub.Code, CreatedAt: now}); err != nil {
			return err
		}
		return s.outbox.AppendTx(ctx, tx, outbox.Event{Topic: "catalog.hub.created", AggregateType: "hub", AggregateID: hub.ID, Payload: string(payload), CreatedAt: now})
	})
	if err != nil {
		return nil, err
	}
	return hub, nil
}

func (s *Service) CreateAsset(ctx context.Context, req CreateAssetRequest) (*Asset, error) {
	if err := req.Validate(); err != nil {
		return nil, apperr.Wrap(apperr.CodeInvalid, "invalid asset", err)
	}
	hub, err := s.repo.GetHub(ctx, req.HubID)
	if err != nil {
		return nil, err
	}
	if hub.Status != "active" {
		return nil, apperr.New(apperr.CodeConflict, "asset cannot be assigned to an inactive hub")
	}
	now := s.clock.Now()
	asset := &Asset{ID: uuid.NewString(), AssetNo: req.AssetNo, Kind: req.Kind, HubID: req.HubID, Status: "available", Capabilities: append([]string(nil), req.Capabilities...), Version: 1, CreatedAt: now, UpdatedAt: now}
	err = platformdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.repo.InsertAssetTx(ctx, tx, *asset); err != nil {
			return err
		}
		payload, err := json.Marshal(asset)
		if err != nil {
			return fmt.Errorf("encode asset event: %w", err)
		}
		if err := s.audit.AppendTx(ctx, tx, audit.Event{ActorID: req.ActorID, Action: "catalog.asset.create", ObjectType: "asset", ObjectID: asset.ID, Outcome: "available", Detail: asset.AssetNo, CreatedAt: now}); err != nil {
			return err
		}
		return s.outbox.AppendTx(ctx, tx, outbox.Event{Topic: "catalog.asset.created", AggregateType: "asset", AggregateID: asset.ID, Payload: string(payload), CreatedAt: now})
	})
	if err != nil {
		return nil, err
	}
	return asset, nil
}

func (s *Service) ChangeAssetState(ctx context.Context, id, target string, version int, actor, reason string) (*Asset, error) {
	if reason == "" {
		return nil, apperr.New(apperr.CodeInvalid, "state change reason is required")
	}
	asset, err := s.repo.GetAsset(ctx, id)
	if err != nil {
		return nil, err
	}
	if !allowedAssetTransition(asset.Status, target) {
		return nil, apperr.New(apperr.CodeConflict, "asset state transition is not allowed")
	}
	now := s.clock.Now()
	err = platformdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.repo.UpdateAssetStateTx(ctx, tx, asset.ID, asset.Status, target, version, now); err != nil {
			return err
		}
		if err := s.audit.AppendTx(ctx, tx, audit.Event{ActorID: actor, Action: "catalog.asset.state", ObjectType: "asset", ObjectID: asset.ID, Outcome: target, Detail: reason, CreatedAt: now}); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"from": asset.Status, "to": target, "version": version + 1})
		return s.outbox.AppendTx(ctx, tx, outbox.Event{Topic: "catalog.asset.state.changed", AggregateType: "asset", AggregateID: asset.ID, Payload: string(payload), CreatedAt: now})
	})
	if err != nil {
		return nil, err
	}
	return s.repo.GetAsset(ctx, asset.ID)
}

func allowedAssetTransition(from, to string) bool {
	allowed := map[string]map[string]bool{
		"available":   {"reserved": true, "maintenance": true, "retired": true},
		"reserved":    {"active": true, "available": true, "maintenance": true},
		"active":      {"available": true, "maintenance": true},
		"maintenance": {"available": true, "retired": true},
	}
	return allowed[from][to]
}

func (s *Service) GetHub(ctx context.Context, id string) (*Hub, error) { return s.repo.GetHub(ctx, id) }
func (s *Service) GetAsset(ctx context.Context, id string) (*Asset, error) {
	return s.repo.GetAsset(ctx, id)
}
func (s *Service) ListAssets(ctx context.Context, filter AssetFilter) ([]Asset, int, error) {
	return s.repo.ListAssets(ctx, filter)
}

func Seed(ctx context.Context, db *sql.DB, now time.Time) error {
	stamp := platformdb.Timestamp(now)
	hubs := []struct {
		id, code, name, kind string
		capacity             int
	}{
		{"hub-road-wuhan", "WUH-ROAD", "武汉城市配送枢纽", string(HubRoad), 240},
		{"hub-port-qingdao", "TAO-PORT", "青岛港智能作业区", string(HubPort), 360},
		{"hub-rail-nanjing", "NKG-RAIL", "南京铁路安全中心", string(HubRail), 180},
		{"hub-air-wuhan", "WUH-AIR", "长江低空巡检基地", string(HubAir), 120},
	}
	for _, hub := range hubs {
		if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO hubs(id,code,name,kind,status,capacity,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, hub.id, hub.code, hub.name, hub.kind, "active", hub.capacity, 1, stamp, stamp); err != nil {
			return fmt.Errorf("seed hub %s: %w", hub.code, err)
		}
	}
	return nil
}
