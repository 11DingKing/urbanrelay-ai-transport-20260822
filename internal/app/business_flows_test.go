package app_test

import (
	"context"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
	"time"
	"urbanrelay/internal/apperr"
	"urbanrelay/internal/audit"
	"urbanrelay/internal/auth"
	"urbanrelay/internal/catalog"
	"urbanrelay/internal/clock"
	"urbanrelay/internal/domain"
	"urbanrelay/internal/idempotency"
	"urbanrelay/internal/incident"
	"urbanrelay/internal/inspection"
	"urbanrelay/internal/maintenance"
	"urbanrelay/internal/mission"
	"urbanrelay/internal/outbox"
	"urbanrelay/internal/platformdb"
	"urbanrelay/internal/requestmeta"
	"urbanrelay/internal/sorting"
	"urbanrelay/internal/transfer"
)

type businessFixture struct {
	db          *platformdb.DB
	clock       *clock.Fake
	actorID     string
	originHubID string
	targetHubID string
	assetID     string
	audit       *audit.Repository
	events      *outbox.Repository
	idem        *idempotency.Repository
}

func newBusinessFixture(t *testing.T) *businessFixture {
	t.Helper()
	ctx := requestmeta.WithRequestID(context.Background(), "business-flow-test")
	db, err := platformdb.Open(ctx, filepath.Join(t.TempDir(), "data"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	clk := clock.NewFake(time.Date(2026, 8, 21, 9, 0, 0, 0, time.UTC))
	require.NoError(t, auth.SeedUsers(ctx, db.SQL, clk.Now()))
	var actor string
	require.NoError(t, db.SQL.QueryRow(`SELECT id FROM users WHERE username='transport_dispatcher'`).Scan(&actor))
	auditRepo := audit.NewRepository(db.SQL)
	events := outbox.NewRepository(db.SQL)
	catalogService := catalog.NewService(db.SQL, catalog.NewRepository(db.SQL), auditRepo, events, clk)
	origin, err := catalogService.CreateHub(ctx, catalog.CreateHubRequest{Code: "FLOW-ORIGIN", Name: "Flow Origin", Kind: catalog.HubMultimodal, Capacity: 500, ActorID: actor})
	require.NoError(t, err)
	target, err := catalogService.CreateHub(ctx, catalog.CreateHubRequest{Code: "FLOW-TARGET", Name: "Flow Target", Kind: catalog.HubPort, Capacity: 500, ActorID: actor})
	require.NoError(t, err)
	asset, err := catalogService.CreateAsset(ctx, catalog.CreateAssetRequest{AssetNo: "FLOW-ASSET", Kind: catalog.AssetInspectionDrone, HubID: origin.ID, Capabilities: []string{"camera", "radar"}, ActorID: actor})
	require.NoError(t, err)
	return &businessFixture{db: db, clock: clk, actorID: actor, originHubID: origin.ID, targetHubID: target.ID, assetID: asset.ID, audit: auditRepo, events: events, idem: idempotency.NewRepository(db.SQL)}
}

func (f *businessFixture) due() time.Time { return f.clock.Now().Add(4 * time.Hour) }

func (f *businessFixture) assertCreatedSideEffects(t *testing.T, objectType, objectID string) {
	t.Helper()
	var audits, events, receipts int
	require.NoError(t, f.db.SQL.QueryRow(`SELECT COUNT(*) FROM audit_events WHERE object_type=? AND object_id=?`, objectType, objectID).Scan(&audits))
	require.NoError(t, f.db.SQL.QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE aggregate_type=? AND aggregate_id=?`, objectType, objectID).Scan(&events))
	require.NoError(t, f.db.SQL.QueryRow(`SELECT COUNT(*) FROM idempotency_records WHERE scope=?`, objectType+".create").Scan(&receipts))
	require.Equal(t, 1, audits)
	require.Equal(t, 1, events)
	require.Equal(t, 1, receipts)
}

func TestMissionLifecycleIdempotencyAndConflict(t *testing.T) {
	f := newBusinessFixture(t)
	ctx := requestmeta.WithRequestID(context.Background(), "mission-request")
	repo := mission.NewRepository(f.db.SQL)
	service := mission.NewService(f.db.SQL, repo, f.audit, f.idem, f.events, f.clock)
	req := mission.CreateRequest{BusinessNo: "MISSION-001", HubID: f.originHubID, AssetID: f.assetID, PayloadUnits: 80, RouteCode: "WUH-R-17", RequestedBy: f.actorID, DueAt: f.due(), IdempotencyKey: "mission-key"}

	created, err := service.Create(ctx, req)
	require.NoError(t, err)
	replayed, err := service.Create(ctx, req)
	require.NoError(t, err)
	require.Equal(t, created.ID, replayed.ID)
	f.assertCreatedSideEffects(t, "mission", created.ID)

	dispatched, err := service.Transition(ctx, mission.TransitionRequest{ID: created.ID, Target: domain.StatusDispatched, ExpectedVersion: created.Version, ActorID: f.actorID, Note: "vehicle checked"})
	require.NoError(t, err)
	require.Equal(t, domain.StatusDispatched, dispatched.Status)
	require.Equal(t, 2, dispatched.Version)

	_, err = service.Transition(ctx, mission.TransitionRequest{ID: created.ID, Target: domain.StatusActive, ExpectedVersion: created.Version, ActorID: f.actorID, Note: "stale command"})
	require.Error(t, err)
	require.Equal(t, apperr.CodeConflict, apperr.CodeOf(err))
	active, err := service.Transition(ctx, mission.TransitionRequest{ID: dispatched.ID, Target: domain.StatusActive, ExpectedVersion: dispatched.Version, ActorID: f.actorID, Note: "left hub"})
	require.NoError(t, err)
	completed, err := service.Transition(ctx, mission.TransitionRequest{ID: active.ID, Target: domain.StatusCompleted, ExpectedVersion: active.Version, ActorID: f.actorID, Note: "delivery confirmed"})
	require.NoError(t, err)
	require.True(t, completed.IsTerminal())
	require.True(t, completed.CanReleaseResource())
}

func TestMissionIdempotencyRejectsChangedPayload(t *testing.T) {
	f := newBusinessFixture(t)
	service := mission.NewService(f.db.SQL, mission.NewRepository(f.db.SQL), f.audit, f.idem, f.events, f.clock)
	req := mission.CreateRequest{BusinessNo: "MISSION-IDEM", HubID: f.originHubID, PayloadUnits: 20, RouteCode: "R-1", RequestedBy: f.actorID, DueAt: f.due(), IdempotencyKey: "same-key"}
	_, err := service.Create(context.Background(), req)
	require.NoError(t, err)
	req.PayloadUnits = 40
	_, err = service.Create(context.Background(), req)
	require.Error(t, err)
	require.Equal(t, apperr.CodeConflict, apperr.CodeOf(err))
}

func TestInspectionLifecycleRequiresReviewBeforeCompletion(t *testing.T) {
	f := newBusinessFixture(t)
	ctx := context.Background()
	service := inspection.NewService(f.db.SQL, inspection.NewRepository(f.db.SQL), f.audit, f.idem, f.events, f.clock)
	created, err := service.Create(ctx, inspection.CreateRequest{InspectionNo: "INSPECT-001", HubID: f.originHubID, AssetID: f.assetID, Priority: 4, TargetRef: "Yangtze bridge pier 12", RequestedBy: f.actorID, DueAt: f.due(), IdempotencyKey: "inspection-key"})
	require.NoError(t, err)
	f.assertCreatedSideEffects(t, "inspection", created.ID)
	_, err = service.Transition(ctx, inspection.TransitionRequest{ID: created.ID, Target: domain.StatusCompleted, ExpectedVersion: created.Version, ActorID: f.actorID, Note: "skip review"})
	require.Error(t, err)

	dispatched, err := service.Transition(ctx, inspection.TransitionRequest{ID: created.ID, Target: domain.StatusDispatched, ExpectedVersion: created.Version, ActorID: f.actorID, Note: "airspace clear"})
	require.NoError(t, err)
	active, err := service.Transition(ctx, inspection.TransitionRequest{ID: created.ID, Target: domain.StatusActive, ExpectedVersion: dispatched.Version, ActorID: f.actorID, Note: "drone launched"})
	require.NoError(t, err)
	review, err := service.Transition(ctx, inspection.TransitionRequest{ID: created.ID, Target: domain.StatusAwaitingReview, ExpectedVersion: active.Version, ActorID: f.actorID, Note: "evidence uploaded"})
	require.NoError(t, err)
	completed, err := service.Transition(ctx, inspection.TransitionRequest{ID: created.ID, Target: domain.StatusCompleted, ExpectedVersion: review.Version, ActorID: f.actorID, Note: "evidence accepted"})
	require.NoError(t, err)
	require.Equal(t, domain.StatusCompleted, completed.Status)
}

func TestTransferLifecycleAcrossTwoHubs(t *testing.T) {
	f := newBusinessFixture(t)
	ctx := context.Background()
	service := transfer.NewService(f.db.SQL, transfer.NewRepository(f.db.SQL), f.audit, f.idem, f.events, f.clock)
	created, err := service.Create(ctx, transfer.CreateRequest{PlanNo: "TRANSFER-001", OriginHubID: f.originHubID, DestinationHubID: f.targetHubID, SegmentCount: 3, ModeChain: "road-rail-water", RequestedBy: f.actorID, ConnectionAt: f.due(), IdempotencyKey: "transfer-key"})
	require.NoError(t, err)
	f.assertCreatedSideEffects(t, "transfer", created.ID)
	reserved, err := service.Transition(ctx, transfer.TransitionRequest{ID: created.ID, Target: domain.StatusReserved, ExpectedVersion: created.Version, ActorID: f.actorID, Note: "segments locked"})
	require.NoError(t, err)
	active, err := service.Transition(ctx, transfer.TransitionRequest{ID: created.ID, Target: domain.StatusActive, ExpectedVersion: reserved.Version, ActorID: f.actorID, Note: "first handoff"})
	require.NoError(t, err)
	completed, err := service.Transition(ctx, transfer.TransitionRequest{ID: created.ID, Target: domain.StatusCompleted, ExpectedVersion: active.Version, ActorID: f.actorID, Note: "port accepted"})
	require.NoError(t, err)
	require.Equal(t, 4, completed.Version)
}

func TestSortingWaveCanCancelBeforeExecution(t *testing.T) {
	f := newBusinessFixture(t)
	service := sorting.NewService(f.db.SQL, sorting.NewRepository(f.db.SQL), f.audit, f.idem, f.events, f.clock)
	created, err := service.Create(context.Background(), sorting.CreateRequest{WaveNo: "WAVE-001", HubID: f.originHubID, ChuteCode: "CHUTE-A7", ExpectedItems: 12000, Profile: "cold-chain-priority", RequestedBy: f.actorID, ClosesAt: f.due(), IdempotencyKey: "sorting-key"})
	require.NoError(t, err)
	f.assertCreatedSideEffects(t, "sorting", created.ID)
	canceled, err := service.Cancel(context.Background(), created.ID, created.Version, f.actorID, "inbound train delayed")
	require.NoError(t, err)
	require.Equal(t, domain.StatusCanceled, canceled.Status)
	require.True(t, canceled.IsTerminal())
	_, err = service.Cancel(context.Background(), canceled.ID, canceled.Version, f.actorID, "repeat")
	require.Error(t, err)
}

func TestIncidentReviewAndClosure(t *testing.T) {
	f := newBusinessFixture(t)
	service := incident.NewService(f.db.SQL, incident.NewRepository(f.db.SQL), f.audit, f.idem, f.events, f.clock)
	created, err := service.Create(context.Background(), incident.CreateRequest{IncidentNo: "INCIDENT-001", HubID: f.originHubID, AssignedTo: f.actorID, RiskScore: 95, Category: "rail-intrusion", RequestedBy: f.actorID, DueAt: f.due(), IdempotencyKey: "incident-key"})
	require.NoError(t, err)
	f.assertCreatedSideEffects(t, "incident", created.ID)
	dispatched, err := service.Transition(context.Background(), incident.TransitionRequest{ID: created.ID, Target: domain.StatusDispatched, ExpectedVersion: created.Version, ActorID: f.actorID, Note: "field team assigned"})
	require.NoError(t, err)
	active, err := service.Transition(context.Background(), incident.TransitionRequest{ID: created.ID, Target: domain.StatusActive, ExpectedVersion: dispatched.Version, ActorID: f.actorID, Note: "team on site"})
	require.NoError(t, err)
	review, err := service.Transition(context.Background(), incident.TransitionRequest{ID: created.ID, Target: domain.StatusAwaitingReview, ExpectedVersion: active.Version, ActorID: f.actorID, Note: "track cleared"})
	require.NoError(t, err)
	closed, err := service.Transition(context.Background(), incident.TransitionRequest{ID: created.ID, Target: domain.StatusCompleted, ExpectedVersion: review.Version, ActorID: f.actorID, Note: "auditor confirmed"})
	require.NoError(t, err)
	require.Equal(t, domain.StatusCompleted, closed.Status)
}

func TestMaintenanceFailureCanReturnToPlanning(t *testing.T) {
	f := newBusinessFixture(t)
	service := maintenance.NewService(f.db.SQL, maintenance.NewRepository(f.db.SQL), f.audit, f.idem, f.events, f.clock)
	created, err := service.Create(context.Background(), maintenance.CreateRequest{TaskNo: "MAINT-001", HubID: f.originHubID, AssetID: f.assetID, EstimatedMinutes: 90, Reason: "battery health below threshold", RequestedBy: f.actorID, DueAt: f.due(), IdempotencyKey: "maintenance-key"})
	require.NoError(t, err)
	f.assertCreatedSideEffects(t, "maintenance", created.ID)
	reserved, err := service.Transition(context.Background(), maintenance.TransitionRequest{ID: created.ID, Target: domain.StatusReserved, ExpectedVersion: created.Version, ActorID: f.actorID, Note: "maintenance bay reserved"})
	require.NoError(t, err)
	active, err := service.Transition(context.Background(), maintenance.TransitionRequest{ID: created.ID, Target: domain.StatusActive, ExpectedVersion: reserved.Version, ActorID: f.actorID, Note: "inspection started"})
	require.NoError(t, err)
	failed, err := service.Transition(context.Background(), maintenance.TransitionRequest{ID: created.ID, Target: domain.StatusFailed, ExpectedVersion: active.Version, ActorID: f.actorID, Note: "replacement unavailable"})
	require.NoError(t, err)
	replanned, err := service.Transition(context.Background(), maintenance.TransitionRequest{ID: created.ID, Target: domain.StatusPlanned, ExpectedVersion: failed.Version, ActorID: f.actorID, Note: "part ordered"})
	require.NoError(t, err)
	require.Equal(t, domain.StatusPlanned, replanned.Status)
}

func TestListAndQueueExposePersistedBusinessState(t *testing.T) {
	f := newBusinessFixture(t)
	repo := mission.NewRepository(f.db.SQL)
	service := mission.NewService(f.db.SQL, repo, f.audit, f.idem, f.events, f.clock)
	for index := 0; index < 5; index++ {
		created, err := service.Create(context.Background(), mission.CreateRequest{BusinessNo: "PAGE-" + time.Duration(index).String(), HubID: f.originHubID, PayloadUnits: index + 1, RouteCode: "PAGE-ROUTE", RequestedBy: f.actorID, DueAt: f.due().Add(time.Duration(index) * time.Minute), IdempotencyKey: "page-key-" + time.Duration(index).String()})
		require.NoError(t, err)
		if index < 2 {
			_, err = service.Transition(context.Background(), mission.TransitionRequest{ID: created.ID, Target: domain.StatusDispatched, ExpectedVersion: created.Version, ActorID: f.actorID, Note: "dispatch"})
			require.NoError(t, err)
		}
	}
	items, total, err := service.List(context.Background(), domain.Page{Limit: 2, Offset: 0, Status: domain.StatusPlanned, Query: "PAGE"})
	require.NoError(t, err)
	require.Len(t, items, 2)
	require.Equal(t, 3, total)
	queue, err := repo.Queue(context.Background())
	require.NoError(t, err)
	require.Len(t, queue, 2)
	counts := map[domain.Status]int{}
	for _, metric := range queue {
		counts[metric.Status] = metric.Count
	}
	require.Equal(t, 2, counts[domain.StatusDispatched])
	require.Equal(t, 3, counts[domain.StatusPlanned])
}

func TestCreateValidationRollsBackAllSideEffects(t *testing.T) {
	f := newBusinessFixture(t)
	service := mission.NewService(f.db.SQL, mission.NewRepository(f.db.SQL), f.audit, f.idem, f.events, f.clock)
	_, err := service.Create(context.Background(), mission.CreateRequest{BusinessNo: "INVALID", HubID: f.originHubID, PayloadUnits: 0, RouteCode: "R", RequestedBy: f.actorID, DueAt: f.due(), IdempotencyKey: "invalid-key"})
	require.Error(t, err)
	require.Equal(t, apperr.CodeInvalid, apperr.CodeOf(err))

	var missions, audits, events, receipts int
	require.NoError(t, f.db.SQL.QueryRow(`SELECT COUNT(*) FROM missions WHERE business_no='INVALID'`).Scan(&missions))
	require.NoError(t, f.db.SQL.QueryRow(`SELECT COUNT(*) FROM audit_events WHERE object_type='mission'`).Scan(&audits))
	require.NoError(t, f.db.SQL.QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE aggregate_type='mission'`).Scan(&events))
	require.NoError(t, f.db.SQL.QueryRow(`SELECT COUNT(*) FROM idempotency_records WHERE idempotency_key='invalid-key'`).Scan(&receipts))
	require.Zero(t, missions)
	require.Zero(t, audits)
	require.Zero(t, events)
	require.Zero(t, receipts)
}

func TestCreateHonorsCanceledContext(t *testing.T) {
	f := newBusinessFixture(t)
	service := mission.NewService(f.db.SQL, mission.NewRepository(f.db.SQL), f.audit, f.idem, f.events, f.clock)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := service.Create(ctx, mission.CreateRequest{BusinessNo: "CANCELED", HubID: f.originHubID, PayloadUnits: 1, RouteCode: "R", RequestedBy: f.actorID, DueAt: f.due(), IdempotencyKey: "canceled-key"})
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
}
