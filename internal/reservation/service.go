package reservation

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
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

func (s *Service) Reserve(ctx context.Context, req ReserveRequest) (*Reservation, error) {
	now := s.clock.Now()
	if err := req.Validate(now); err != nil {
		return nil, apperr.Wrap(apperr.CodeInvalid, "invalid reservation", err)
	}
	value := &Reservation{
		ID: uuid.NewString(), ResourceType: req.ResourceType, ResourceKey: req.ResourceKey,
		OwnerType: req.OwnerType, OwnerID: req.OwnerID, StartsAt: req.StartsAt.UTC(), EndsAt: req.EndsAt.UTC(),
		Status: "active", Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	err := platformdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		conflicts, err := s.repo.ConflictsTx(ctx, tx, req)
		if err != nil {
			return err
		}
		if len(conflicts) > 0 {
			return apperr.New(apperr.CodeConflict, "resource is already reserved in this window")
		}
		if err := s.repo.InsertTx(ctx, tx, *value); err != nil {
			return err
		}
		payload, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("encode reservation event: %w", err)
		}
		if err := s.audit.AppendTx(ctx, tx, audit.Event{ActorID: req.ActorID, Action: "reservation.create", ObjectType: req.OwnerType, ObjectID: req.OwnerID, Outcome: "reserved", Detail: req.ResourceType + ":" + req.ResourceKey, CreatedAt: now}); err != nil {
			return err
		}
		return s.outbox.AppendTx(ctx, tx, outbox.Event{Topic: "resource.reserved", AggregateType: req.OwnerType, AggregateID: req.OwnerID, Payload: string(payload), CreatedAt: now})
	})
	if err != nil {
		return nil, err
	}
	return value, nil
}

func (s *Service) Release(ctx context.Context, id string, version int, actor, reason string) (*Reservation, error) {
	if reason == "" {
		return nil, apperr.New(apperr.CodeInvalid, "release reason is required")
	}
	value, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if value.Status != "active" {
		return nil, apperr.New(apperr.CodeConflict, "reservation is not active")
	}
	now := s.clock.Now()
	err = platformdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.repo.ReleaseTx(ctx, tx, value.ID, value.Version, now); err != nil {
			return err
		}
		if err := s.audit.AppendTx(ctx, tx, audit.Event{ActorID: actor, Action: "reservation.release", ObjectType: value.OwnerType, ObjectID: value.OwnerID, Outcome: "released", Detail: reason, CreatedAt: now}); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"reservation_id": value.ID, "reason": reason})
		return s.outbox.AppendTx(ctx, tx, outbox.Event{Topic: "resource.released", AggregateType: value.OwnerType, AggregateID: value.OwnerID, Payload: string(payload), CreatedAt: now})
	})
	if err != nil {
		return nil, err
	}
	return s.repo.Get(ctx, value.ID)
}

func (s *Service) Get(ctx context.Context, id string) (*Reservation, error) {
	return s.repo.Get(ctx, id)
}

func (s *Service) ListWindow(ctx context.Context, query WindowQuery) ([]Reservation, error) {
	return s.repo.ListWindow(ctx, query)
}

func (s *Service) ActiveForOwner(ctx context.Context, ownerType, ownerID string) ([]Reservation, error) {
	return s.repo.ActiveForOwner(ctx, ownerType, ownerID)
}
