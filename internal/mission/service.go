package mission

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"time"
	"urbanrelay/internal/apperr"
	"urbanrelay/internal/audit"
	"urbanrelay/internal/clock"
	"urbanrelay/internal/domain"
	"urbanrelay/internal/idempotency"
	"urbanrelay/internal/outbox"
	"urbanrelay/internal/platformdb"
)

type Service struct {
	db          *sql.DB
	repo        *Repository
	audit       *audit.Repository
	idempotency *idempotency.Repository
	outbox      *outbox.Repository
	clock       clock.Clock
}

func NewService(db *sql.DB, repo *Repository, auditRepo *audit.Repository, idem *idempotency.Repository, events *outbox.Repository, clk clock.Clock) *Service {
	return &Service{db: db, repo: repo, audit: auditRepo, idempotency: idem, outbox: events, clock: clk}
}

func (s *Service) Create(ctx context.Context, req CreateRequest) (*Mission, error) {
	now := s.clock.Now()
	if err := req.Validate(now); err != nil {
		return nil, apperr.Wrap(apperr.CodeInvalid, "invalid delivery mission", err)
	}
	digest := requestDigest(req)
	lookupDigest := ""
	if prior, found, err := s.idempotency.Lookup(ctx, "mission.create", req.IdempotencyKey, lookupDigest, now); err != nil {
		return nil, err
	} else if found {
		var value Mission
		if err := json.Unmarshal([]byte(prior), &value); err != nil {
			return nil, fmt.Errorf("decode idempotent delivery mission: %w", err)
		}
		return &value, nil
	}
	value := &Mission{
		ID: uuid.NewString(), BusinessNo: req.BusinessNo, HubID: req.HubID, AssetID: req.AssetID, PayloadUnits: req.PayloadUnits,
		RouteCode: req.RouteCode, Status: domain.StatusPlanned, Version: 1, RequestedBy: req.RequestedBy, DueAt: req.DueAt.UTC(), CreatedAt: now, UpdatedAt: now,
	}
	response, _ := json.Marshal(value)
	err := platformdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.repo.CreateTx(ctx, tx, value); err != nil {
			return err
		}
		if err := s.audit.AppendTx(ctx, tx, audit.Event{ActorID: req.RequestedBy, Action: "mission.create", ObjectType: "mission", ObjectID: value.ID, Outcome: "accepted", Detail: value.BusinessNo, CreatedAt: now}); err != nil {
			return err
		}
		if err := s.outbox.AppendTx(ctx, tx, outbox.Event{Topic: "mission.created", AggregateType: "mission", AggregateID: value.ID, Payload: string(response), CreatedAt: now}); err != nil {
			return err
		}
		return s.idempotency.SaveTx(ctx, tx, idempotency.Record{Scope: "mission.create", Key: req.IdempotencyKey, RequestHash: digest, ResponseJSON: string(response), StatusCode: 201, ExpiresAt: now.Add(24 * time.Hour), CreatedAt: now})
	})
	if err != nil {
		return nil, err
	}
	return value, nil
}

func (s *Service) Transition(ctx context.Context, req TransitionRequest) (*Mission, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	current, err := s.repo.Get(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if current.Version != req.ExpectedVersion {
		return nil, apperr.New(apperr.CodeConflict, "delivery mission version changed")
	}
	if err := current.ValidateTransition(req.Target); err != nil {
		return nil, apperr.Wrap(apperr.CodeConflict, "invalid delivery mission transition", err)
	}
	now := s.clock.Now()
	err = platformdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.repo.TransitionTx(ctx, tx, current.ID, current.Status, req.Target, current.Version, now); err != nil {
			return err
		}
		if err := s.audit.AppendTx(ctx, tx, audit.Event{ActorID: req.ActorID, Action: "mission.transition", ObjectType: "mission", ObjectID: current.ID, Outcome: string(req.Target), Detail: req.Note, CreatedAt: now}); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"from": current.Status, "to": req.Target, "version": current.Version + 1})
		return s.outbox.AppendTx(ctx, tx, outbox.Event{Topic: "mission.status.changed", AggregateType: "mission", AggregateID: current.ID, Payload: string(payload), CreatedAt: now})
	})
	if err != nil {
		return nil, err
	}
	updated, err := s.repo.Get(ctx, current.ID)
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func (s *Service) Cancel(ctx context.Context, id string, version int, actor, reason string) (*Mission, error) {
	if reason == "" {
		return nil, apperr.New(apperr.CodeInvalid, "cancel reason is required")
	}
	return s.Transition(ctx, TransitionRequest{ID: id, Target: domain.StatusCanceled, ExpectedVersion: version, ActorID: actor, Note: reason})
}
func (s *Service) Get(ctx context.Context, id string) (*Mission, error) { return s.repo.Get(ctx, id) }
func (s *Service) List(ctx context.Context, page domain.Page) ([]Mission, int, error) {
	return s.repo.List(ctx, page)
}
func requestDigest(req CreateRequest) string {
	data, _ := json.Marshal(req)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
