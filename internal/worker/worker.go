package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
	"urbanrelay/internal/clock"
	"urbanrelay/internal/outbox"
)

type Publisher interface {
	Publish(context.Context, string, string) error
}
type Worker struct {
	repo        *outbox.Repository
	publisher   Publisher
	clock       clock.Clock
	poll        time.Duration
	maxAttempts int
	logger      *slog.Logger
	wg          sync.WaitGroup
	once        sync.Once
}

func New(repo *outbox.Repository, publisher Publisher, clk clock.Clock, poll time.Duration, maxAttempts int, logger *slog.Logger) *Worker {
	return &Worker{repo: repo, publisher: publisher, clock: clk, poll: poll, maxAttempts: maxAttempts, logger: logger}
}
func (w *Worker) Run(ctx context.Context) {
	w.once.Do(func() {
		w.wg.Add(1)
		go func() {
			defer w.wg.Done()
			ticker := time.NewTicker(w.poll)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					w.process(ctx)
				}
			}
		}()
	})
}
func (w *Worker) Wait() { w.wg.Wait() }
func (w *Worker) process(ctx context.Context) {
	events, err := w.repo.Due(ctx, w.clock.Now(), 50)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			w.logger.Error("load outbox", "error", err)
		}
		return
	}
	for _, event := range events {
		if err := ctx.Err(); err != nil {
			return
		}
		if err := w.publisher.Publish(ctx, event.Topic, event.Payload); err != nil {
			attempts := event.Attempts + 1
			delay := time.Duration(1<<min(attempts, 6)) * time.Second
			if attempts >= w.maxAttempts {
				delay = 24 * time.Hour
			}
			if markErr := w.repo.MarkRetry(ctx, event.ID, attempts, w.clock.Now().Add(delay)); markErr != nil {
				w.logger.Error("mark retry", "event_id", event.ID, "error", markErr)
			}
			continue
		}
		if err := w.repo.MarkPublished(ctx, event.ID, w.clock.Now()); err != nil {
			w.logger.Error("mark published", "event_id", event.ID, "error", err)
		}
	}
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

type LogPublisher struct{ logger *slog.Logger }

func NewLogPublisher(logger *slog.Logger) *LogPublisher { return &LogPublisher{logger: logger} }
func (p *LogPublisher) Publish(ctx context.Context, topic, payload string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if topic == "" {
		return fmt.Errorf("topic is required")
	}
	p.logger.InfoContext(ctx, "outbox event", "topic", topic, "payload_bytes", len(payload))
	return nil
}
