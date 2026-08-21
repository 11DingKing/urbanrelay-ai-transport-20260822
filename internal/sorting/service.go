package sorting

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

func (s *Service) Create(ctx context.Context, req CreateRequest) (*Wave, error) {
	now := s.clock.Now()
	if err := req.Validate(now); err != nil {
		return nil, apperr.Wrap(apperr.CodeInvalid, "invalid sorting wave", err)
	}
	digest := requestDigest(req)
	if prior, found, err := s.idempotency.Lookup(ctx, "sorting.create", req.IdempotencyKey, digest, now); err != nil {
		return nil, err
	} else if found {
		var value Wave
		if err := json.Unmarshal([]byte(prior), &value); err != nil {
			return nil, fmt.Errorf("decode idempotent sorting wave: %w", err)
		}
		return &value, nil
	}
	value := &Wave{
		ID: uuid.NewString(), WaveNo: req.WaveNo, HubID: req.HubID, ChuteCode: req.ChuteCode, ExpectedItems: req.ExpectedItems,
		Profile: req.Profile, Status: domain.StatusPlanned, Version: 1, RequestedBy: req.RequestedBy, ClosesAt: req.ClosesAt.UTC(), CreatedAt: now, UpdatedAt: now,
	}
	response, _ := json.Marshal(value)
	err := platformdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.repo.CreateTx(ctx, tx, value); err != nil {
			return err
		}
		if err := s.audit.AppendTx(ctx, tx, audit.Event{ActorID: req.RequestedBy, Action: "sorting.create", ObjectType: "sorting", ObjectID: value.ID, Outcome: "accepted", Detail: value.WaveNo, CreatedAt: now}); err != nil {
			return err
		}
		if err := s.outbox.AppendTx(ctx, tx, outbox.Event{Topic: "sorting.created", AggregateType: "sorting", AggregateID: value.ID, Payload: string(response), CreatedAt: now}); err != nil {
			return err
		}
		return s.idempotency.SaveTx(ctx, tx, idempotency.Record{Scope: "sorting.create", Key: req.IdempotencyKey, RequestHash: digest, ResponseJSON: string(response), StatusCode: 201, ExpiresAt: now.Add(24 * time.Hour), CreatedAt: now})
	})
	if err != nil {
		return nil, err
	}
	return value, nil
}

func (s *Service) Transition(ctx context.Context, req TransitionRequest) (*Wave, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	current, err := s.repo.Get(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if current.Version != req.ExpectedVersion {
		return nil, apperr.New(apperr.CodeConflict, "sorting wave version changed")
	}
	if err := current.ValidateTransition(req.Target); err != nil {
		return nil, apperr.Wrap(apperr.CodeConflict, "invalid sorting wave transition", err)
	}
	now := s.clock.Now()
	err = platformdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.repo.TransitionTx(ctx, tx, current.ID, current.Status, req.Target, current.Version, now); err != nil {
			return err
		}
		if err := s.audit.AppendTx(ctx, tx, audit.Event{ActorID: req.ActorID, Action: "sorting.transition", ObjectType: "sorting", ObjectID: current.ID, Outcome: string(req.Target), Detail: req.Note, CreatedAt: now}); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"from": current.Status, "to": req.Target, "version": current.Version + 1})
		return s.outbox.AppendTx(ctx, tx, outbox.Event{Topic: "sorting.status.changed", AggregateType: "sorting", AggregateID: current.ID, Payload: string(payload), CreatedAt: now})
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

func (s *Service) Cancel(ctx context.Context, id string, version int, actor, reason string) (*Wave, error) {
	if reason == "" {
		return nil, apperr.New(apperr.CodeInvalid, "cancel reason is required")
	}
	return s.Transition(ctx, TransitionRequest{ID: id, Target: domain.StatusCanceled, ExpectedVersion: version, ActorID: actor, Note: reason})
}
func (s *Service) Get(ctx context.Context, id string) (*Wave, error) { return s.repo.Get(ctx, id) }
func (s *Service) List(ctx context.Context, page domain.Page) ([]Wave, int, error) {
	return s.repo.List(ctx, page)
}
func requestDigest(req CreateRequest) string {
	data, _ := json.Marshal(req)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
