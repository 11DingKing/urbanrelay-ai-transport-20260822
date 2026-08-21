package jobqueue

import (
	"context"
	"errors"
	"github.com/stretchr/testify/require"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"
	"urbanrelay/internal/apperr"
	"urbanrelay/internal/clock"
	"urbanrelay/internal/platformdb"
)

type fixture struct {
	db    *platformdb.DB
	clock *clock.Fake
	repo  *Repository
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db, err := platformdb.Open(context.Background(), filepath.Join(t.TempDir(), "data"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	clk := clock.NewFake(time.Date(2026, 8, 21, 9, 0, 0, 0, time.UTC))
	return &fixture{db: db, clock: clk, repo: NewRepository(db.SQL)}
}

func (f *fixture) enqueue(t *testing.T, kind, object string, attempts int) *Job {
	t.Helper()
	job, err := f.repo.Enqueue(context.Background(), EnqueueRequest{Kind: kind, ObjectID: object, Payload: `{"object":"` + object + `"}`, MaxAttempts: attempts}, f.clock.Now())
	require.NoError(t, err)
	return job
}

func TestEnqueueAndGetRoundTrip(t *testing.T) {
	f := newFixture(t)
	job := f.enqueue(t, "inspection.evidence.index", "inspection-1", 4)
	require.NotEmpty(t, job.ID)
	require.Equal(t, StatusPending, job.Status)
	require.Zero(t, job.Attempts)
	require.Equal(t, f.clock.Now(), job.NextAttemptAt)

	stored, err := f.repo.Get(context.Background(), job.ID)
	require.NoError(t, err)
	require.Equal(t, *job, *stored)
}

func TestEnqueueValidatesInput(t *testing.T) {
	f := newFixture(t)
	cases := []EnqueueRequest{
		{ObjectID: "object", Payload: `{}`, MaxAttempts: 3},
		{Kind: "kind", Payload: `{}`, MaxAttempts: 3},
		{Kind: "kind", ObjectID: "object", MaxAttempts: 3},
		{Kind: "kind", ObjectID: "object", Payload: `{}`, MaxAttempts: 0},
		{Kind: "kind", ObjectID: "object", Payload: `{}`, MaxAttempts: 21},
	}
	for _, req := range cases {
		_, err := f.repo.Enqueue(context.Background(), req, f.clock.Now())
		require.Error(t, err)
		require.Equal(t, apperr.CodeInvalid, apperr.CodeOf(err))
	}
}

func TestEnqueueRejectsDuplicateKindAndObject(t *testing.T) {
	f := newFixture(t)
	f.enqueue(t, "maintenance.schedule", "asset-1", 3)
	_, err := f.repo.Enqueue(context.Background(), EnqueueRequest{Kind: "maintenance.schedule", ObjectID: "asset-1", Payload: `{}`, MaxAttempts: 3}, f.clock.Now())
	require.Error(t, err)
	require.Equal(t, apperr.CodeConflict, apperr.CodeOf(err))

	_, err = f.repo.Enqueue(context.Background(), EnqueueRequest{Kind: "maintenance.schedule", ObjectID: "asset-2", Payload: `{}`, MaxAttempts: 3}, f.clock.Now())
	require.NoError(t, err)
	_, err = f.repo.Enqueue(context.Background(), EnqueueRequest{Kind: "incident.escalation.notify", ObjectID: "asset-1", Payload: `{}`, MaxAttempts: 3}, f.clock.Now())
	require.NoError(t, err)
}

func TestClaimLeasesDueJobsInOrder(t *testing.T) {
	f := newFixture(t)
	first := f.enqueue(t, "inspection.evidence.index", "inspection-1", 3)
	f.clock.Advance(time.Nanosecond)
	second := f.enqueue(t, "inspection.evidence.index", "inspection-2", 3)
	_, err := f.repo.Enqueue(context.Background(), EnqueueRequest{Kind: "inspection.evidence.index", ObjectID: "inspection-later", Payload: `{}`, MaxAttempts: 3, NextAttemptAt: f.clock.Now().Add(time.Hour)}, f.clock.Now())
	require.NoError(t, err)

	claimed, err := f.repo.Claim(context.Background(), "worker-a", f.clock.Now(), 30*time.Second, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 2)
	require.Equal(t, []string{first.ID, second.ID}, []string{claimed[0].ID, claimed[1].ID})
	for _, job := range claimed {
		require.Equal(t, StatusRunning, job.Status)
		require.Equal(t, "worker-a", job.LeaseOwner)
		require.NotNil(t, job.LeaseUntil)
		require.Equal(t, f.clock.Now().Add(30*time.Second), *job.LeaseUntil)
	}
}

func TestActiveLeaseCannotBeClaimedTwice(t *testing.T) {
	f := newFixture(t)
	f.enqueue(t, "sorting.discrepancy.reconcile", "wave-1", 3)
	first, err := f.repo.Claim(context.Background(), "worker-a", f.clock.Now(), time.Minute, 10)
	require.NoError(t, err)
	require.Len(t, first, 1)
	second, err := f.repo.Claim(context.Background(), "worker-b", f.clock.Now(), time.Minute, 10)
	require.NoError(t, err)
	require.Empty(t, second)
}

func TestExpiredLeaseCanBeRequeuedAndClaimed(t *testing.T) {
	f := newFixture(t)
	job := f.enqueue(t, "incident.escalation.notify", "incident-1", 3)
	claimed, err := f.repo.Claim(context.Background(), "worker-a", f.clock.Now(), time.Minute, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	f.clock.Advance(2 * time.Minute)

	changed, err := f.repo.RequeueExpiredLeases(context.Background(), f.clock.Now())
	require.NoError(t, err)
	require.EqualValues(t, 1, changed)
	claimed, err = f.repo.Claim(context.Background(), "worker-b", f.clock.Now(), time.Minute, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, job.ID, claimed[0].ID)
	require.Equal(t, "worker-b", claimed[0].LeaseOwner)
}

func TestCompleteRequiresCurrentLeaseOwner(t *testing.T) {
	f := newFixture(t)
	job := f.enqueue(t, "maintenance.schedule", "asset-complete", 3)
	_, err := f.repo.Claim(context.Background(), "worker-a", f.clock.Now(), time.Minute, 10)
	require.NoError(t, err)
	require.Error(t, f.repo.Complete(context.Background(), job.ID, "worker-b", f.clock.Now()))
	require.NoError(t, f.repo.Complete(context.Background(), job.ID, "worker-a", f.clock.Now()))

	stored, err := f.repo.Get(context.Background(), job.ID)
	require.NoError(t, err)
	require.Equal(t, StatusCompleted, stored.Status)
	require.Empty(t, stored.LeaseOwner)
	require.Nil(t, stored.LeaseUntil)
	require.Error(t, f.repo.Complete(context.Background(), job.ID, "worker-a", f.clock.Now()))
}

func TestFailSchedulesRetryWithBackoff(t *testing.T) {
	f := newFixture(t)
	job := f.enqueue(t, "maintenance.schedule", "asset-retry", 4)
	_, err := f.repo.Claim(context.Background(), "worker-a", f.clock.Now(), time.Minute, 10)
	require.NoError(t, err)
	failed, err := f.repo.Fail(context.Background(), job.ID, "worker-a", "temporary gateway error", f.clock.Now())
	require.NoError(t, err)
	require.Equal(t, StatusRetrying, failed.Status)
	require.Equal(t, 1, failed.Attempts)
	require.Equal(t, f.clock.Now().Add(time.Second), failed.NextAttemptAt)
	require.Equal(t, "temporary gateway error", failed.LastError)

	claimed, err := f.repo.Claim(context.Background(), "worker-b", f.clock.Now(), time.Minute, 10)
	require.NoError(t, err)
	require.Empty(t, claimed)
	f.clock.Advance(time.Second)
	claimed, err = f.repo.Claim(context.Background(), "worker-b", f.clock.Now(), time.Minute, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
}

func TestFailStopsAtMaximumAttempts(t *testing.T) {
	f := newFixture(t)
	job := f.enqueue(t, "maintenance.schedule", "asset-fail", 2)
	for attempt := 1; attempt <= 2; attempt++ {
		claimed, err := f.repo.Claim(context.Background(), "worker-a", f.clock.Now(), time.Minute, 10)
		require.NoError(t, err)
		require.Len(t, claimed, 1)
		updated, err := f.repo.Fail(context.Background(), job.ID, "worker-a", "failure", f.clock.Now())
		require.NoError(t, err)
		if attempt == 1 {
			require.Equal(t, StatusRetrying, updated.Status)
			f.clock.Set(updated.NextAttemptAt)
		} else {
			require.Equal(t, StatusFailed, updated.Status)
		}
	}
	claimed, err := f.repo.Claim(context.Background(), "worker-a", f.clock.Now().Add(time.Hour), time.Minute, 10)
	require.NoError(t, err)
	require.Empty(t, claimed)
}

func TestFailRequiresCurrentLeaseOwner(t *testing.T) {
	f := newFixture(t)
	job := f.enqueue(t, "maintenance.schedule", "asset-owner", 3)
	_, err := f.repo.Claim(context.Background(), "worker-a", f.clock.Now(), time.Minute, 10)
	require.NoError(t, err)
	_, err = f.repo.Fail(context.Background(), job.ID, "worker-b", "wrong owner", f.clock.Now())
	require.Error(t, err)
	require.Equal(t, apperr.CodeConflict, apperr.CodeOf(err))
}

func TestCancelOnlyPendingOrRetryingJobs(t *testing.T) {
	f := newFixture(t)
	pending := f.enqueue(t, "maintenance.schedule", "asset-cancel", 3)
	require.NoError(t, f.repo.Cancel(context.Background(), pending.ID, f.clock.Now()))
	stored, err := f.repo.Get(context.Background(), pending.ID)
	require.NoError(t, err)
	require.Equal(t, StatusCanceled, stored.Status)
	require.Error(t, f.repo.Cancel(context.Background(), pending.ID, f.clock.Now()))

	running := f.enqueue(t, "maintenance.schedule", "asset-running", 3)
	_, err = f.repo.Claim(context.Background(), "worker-a", f.clock.Now(), time.Minute, 10)
	require.NoError(t, err)
	require.Error(t, f.repo.Cancel(context.Background(), running.ID, f.clock.Now()))
}

func TestClaimValidatesLeaseOwnerAndDuration(t *testing.T) {
	f := newFixture(t)
	f.enqueue(t, "maintenance.schedule", "asset-validation", 3)
	_, err := f.repo.Claim(context.Background(), "", f.clock.Now(), time.Minute, 10)
	require.Error(t, err)
	_, err = f.repo.Claim(context.Background(), "worker", f.clock.Now(), 0, 10)
	require.Error(t, err)
}

func TestRetryDelayIsBounded(t *testing.T) {
	require.Equal(t, time.Second, RetryDelay(0))
	require.Equal(t, time.Second, RetryDelay(1))
	require.Equal(t, 2*time.Second, RetryDelay(2))
	require.Equal(t, 4*time.Second, RetryDelay(3))
	require.Equal(t, 128*time.Second, RetryDelay(8))
	require.Equal(t, 128*time.Second, RetryDelay(100))
}

func TestDispatchHandlerAcceptsKnownKindsAndRejectsUnknown(t *testing.T) {
	handler := NewDispatchHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	kinds := []string{"inspection.evidence.index", "sorting.discrepancy.reconcile", "incident.escalation.notify", "maintenance.schedule"}
	for _, kind := range kinds {
		require.NoError(t, handler.Handle(context.Background(), Job{Kind: kind, ObjectID: "object"}))
	}
	require.Error(t, handler.Handle(context.Background(), Job{Kind: "unknown"}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, handler.Handle(ctx, Job{Kind: kinds[0]}), context.Canceled)
}

func TestRunnerCompletesClaimedJob(t *testing.T) {
	f := newFixture(t)
	job := f.enqueue(t, "maintenance.schedule", "asset-runner", 3)
	ctx, cancel := context.WithCancel(context.Background())
	runner := NewRunner(f.repo, HandlerFunc(func(context.Context, Job) error { return nil }), f.clock, "runner", time.Millisecond, time.Minute, 1, slog.New(slog.NewTextHandler(io.Discard, nil)))
	runner.Run(ctx)
	require.Eventually(t, func() bool {
		stored, err := f.repo.Get(context.Background(), job.ID)
		return err == nil && stored.Status == StatusCompleted
	}, time.Second, 10*time.Millisecond)
	cancel()
	runner.Wait()
}

func TestRunnerRetriesHandlerFailure(t *testing.T) {
	f := newFixture(t)
	job := f.enqueue(t, "maintenance.schedule", "asset-runner-retry", 3)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner := NewRunner(f.repo, HandlerFunc(func(context.Context, Job) error { return errors.New("temporary") }), f.clock, "runner", time.Millisecond, time.Minute, 1, slog.New(slog.NewTextHandler(io.Discard, nil)))
	runner.Run(ctx)
	require.Eventually(t, func() bool {
		stored, err := f.repo.Get(context.Background(), job.ID)
		return err == nil && stored.Status == StatusRetrying && stored.Attempts == 1
	}, time.Second, 10*time.Millisecond)
	cancel()
	runner.Wait()
}

func TestConcurrentClaimGivesJobToOneOwner(t *testing.T) {
	f := newFixture(t)
	f.enqueue(t, "incident.escalation.notify", "incident-race", 3)
	var wg sync.WaitGroup
	results := make(chan []Job, 2)
	errs := make(chan error, 2)
	for _, owner := range []string{"worker-a", "worker-b"} {
		wg.Add(1)
		go func(owner string) {
			defer wg.Done()
			jobs, err := f.repo.Claim(context.Background(), owner, f.clock.Now(), time.Minute, 1)
			results <- jobs
			errs <- err
		}(owner)
	}
	wg.Wait()
	close(results)
	close(errs)
	claimed := 0
	for err := range errs {
		require.NoError(t, err)
	}
	for jobs := range results {
		claimed += len(jobs)
	}
	require.Equal(t, 1, claimed)
}
