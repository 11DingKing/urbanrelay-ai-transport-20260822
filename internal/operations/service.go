package operations

import (
	"context"
	"fmt"
	"urbanrelay/internal/clock"
)

type Service struct {
	repo  *Repository
	clock clock.Clock
}

func NewService(repo *Repository, clk clock.Clock) *Service {
	return &Service{repo: repo, clock: clk}
}

func (s *Service) Snapshot(ctx context.Context) (*Snapshot, error) {
	now := s.clock.Now()
	queues, err := s.repo.QueueMetrics(ctx)
	if err != nil {
		return nil, fmt.Errorf("load queue snapshot: %w", err)
	}
	hubs, err := s.repo.HubMetrics(ctx, now)
	if err != nil {
		return nil, fmt.Errorf("load hub snapshot: %w", err)
	}
	reliability, err := s.repo.Reliability(ctx, now)
	if err != nil {
		return nil, fmt.Errorf("load reliability snapshot: %w", err)
	}
	return &Snapshot{GeneratedAt: now, Queues: queues, Hubs: hubs, Reliability: reliability}, nil
}

func (s *Service) SearchAudit(ctx context.Context, query AuditSearch) ([]AuditEntry, int, error) {
	return s.repo.SearchAudit(ctx, query)
}
