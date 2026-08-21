package jobqueue

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
	"urbanrelay/internal/clock"
)

type Handler interface {
	Handle(context.Context, Job) error
}

type HandlerFunc func(context.Context, Job) error

func (f HandlerFunc) Handle(ctx context.Context, job Job) error { return f(ctx, job) }

type Runner struct {
	repo       *Repository
	handler    Handler
	clock      clock.Clock
	owner      string
	poll       time.Duration
	lease      time.Duration
	batch      int
	logger     *slog.Logger
	once       sync.Once
	wg         sync.WaitGroup
	sem        chan struct{}
	processMu  sync.Mutex
	processing map[string]struct{}
}

func NewRunner(repo *Repository, handler Handler, clk clock.Clock, owner string, poll, lease time.Duration, concurrency int, logger *slog.Logger) *Runner {
	if concurrency < 1 {
		concurrency = 1
	}
	return &Runner{repo: repo, handler: handler, clock: clk, owner: owner, poll: poll, lease: lease, batch: concurrency * 2, logger: logger, sem: make(chan struct{}, concurrency), processing: make(map[string]struct{})}
}

func (r *Runner) Run(ctx context.Context) {
	r.once.Do(func() {
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			ticker := time.NewTicker(r.poll)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					r.cycle(ctx)
				}
			}
		}()
	})
}

func (r *Runner) Wait() { r.wg.Wait() }

func (r *Runner) cycle(ctx context.Context) {
	now := r.clock.Now()
	if _, err := r.repo.RequeueExpiredLeases(ctx, now); err != nil && !errors.Is(err, context.Canceled) {
		r.logger.Error("requeue expired jobs", "error", err)
		return
	}
	jobs, err := r.repo.Claim(ctx, r.owner, now, r.lease, r.batch)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			r.logger.Error("claim jobs", "error", err)
		}
		return
	}
	for _, job := range jobs {
		if !r.begin(job.ID) {
			continue
		}
		r.sem <- struct{}{}
		r.wg.Add(1)
		go r.handle(ctx, job)
	}
}

func (r *Runner) handle(ctx context.Context, job Job) {
	defer r.wg.Done()
	defer func() { <-r.sem; r.end(job.ID) }()
	if err := r.handler.Handle(ctx, job); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		if _, markErr := r.repo.Fail(ctx, job.ID, r.owner, err.Error(), r.clock.Now()); markErr != nil {
			r.logger.Error("mark job failed", "job_id", job.ID, "error", markErr)
		}
		return
	}
	if err := r.repo.Complete(ctx, job.ID, r.owner, r.clock.Now()); err != nil {
		r.logger.Error("complete job", "job_id", job.ID, "error", err)
	}
}

func (r *Runner) begin(id string) bool {
	r.processMu.Lock()
	defer r.processMu.Unlock()
	if _, exists := r.processing[id]; exists {
		return false
	}
	r.processing[id] = struct{}{}
	return true
}

func (r *Runner) end(id string) {
	r.processMu.Lock()
	delete(r.processing, id)
	r.processMu.Unlock()
}

type DispatchHandler struct{ logger *slog.Logger }

func NewDispatchHandler(logger *slog.Logger) *DispatchHandler {
	return &DispatchHandler{logger: logger}
}

func (h *DispatchHandler) Handle(ctx context.Context, job Job) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	switch job.Kind {
	case "inspection.evidence.index", "sorting.discrepancy.reconcile", "incident.escalation.notify", "maintenance.schedule":
		h.logger.InfoContext(ctx, "processed durable job", "kind", job.Kind, "object_id", job.ObjectID, "attempt", job.Attempts+1)
		return nil
	default:
		return fmt.Errorf("unsupported durable job kind %q", job.Kind)
	}
}
