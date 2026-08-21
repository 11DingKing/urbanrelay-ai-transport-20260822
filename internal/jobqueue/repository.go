package jobqueue

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"strings"
	"time"
	"urbanrelay/internal/apperr"
	"urbanrelay/internal/platformdb"
)

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

const columns = "id,kind,object_id,payload,status,attempts,max_attempts,next_attempt_at,lease_owner,lease_until,last_error,created_at,updated_at"

func (r *Repository) Enqueue(ctx context.Context, req EnqueueRequest, now time.Time) (*Job, error) {
	if err := req.Validate(); err != nil {
		return nil, apperr.Wrap(apperr.CodeInvalid, "invalid worker job", err)
	}
	next := req.NextAttemptAt.UTC()
	if next.IsZero() || next.Before(now) {
		next = now
	}
	job := &Job{ID: uuid.NewString(), Kind: req.Kind, ObjectID: req.ObjectID, Payload: req.Payload, Status: StatusPending, MaxAttempts: req.MaxAttempts, NextAttemptAt: next, CreatedAt: now, UpdatedAt: now}
	_, err := r.db.ExecContext(ctx, `INSERT INTO worker_jobs(`+columns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		job.ID, job.Kind, job.ObjectID, job.Payload, job.Status, job.Attempts, job.MaxAttempts, stamp(job.NextAttemptAt), nil, nil, job.LastError, stamp(job.CreatedAt), stamp(job.UpdatedAt))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, apperr.Wrap(apperr.CodeConflict, "job already exists for object", err)
		}
		return nil, fmt.Errorf("enqueue worker job: %w", err)
	}
	return job, nil
}

func (r *Repository) Get(ctx context.Context, id string) (*Job, error) {
	return scanJob(r.db.QueryRowContext(ctx, `SELECT `+columns+` FROM worker_jobs WHERE id=?`, id))
}

func (r *Repository) Claim(ctx context.Context, owner string, now time.Time, lease time.Duration, limit int) ([]Job, error) {
	if strings.TrimSpace(owner) == "" {
		return nil, apperr.New(apperr.CodeInvalid, "lease owner is required")
	}
	if lease <= 0 {
		return nil, apperr.New(apperr.CodeInvalid, "lease duration must be positive")
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	claimed := []Job{}
	err := platformdb.WithTx(ctx, r.db, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT `+columns+` FROM worker_jobs
			WHERE status IN ('pending','retrying','running') AND next_attempt_at<=?
			AND (lease_until IS NULL OR lease_until<=?) ORDER BY next_attempt_at,id LIMIT ?`, stamp(now), stamp(now), limit)
		if err != nil {
			return fmt.Errorf("find claimable jobs: %w", err)
		}
		candidates := []Job{}
		for rows.Next() {
			job, scanErr := scanJob(rows)
			if scanErr != nil {
				rows.Close()
				return scanErr
			}
			candidates = append(candidates, *job)
		}
		if err := rows.Close(); err != nil {
			return fmt.Errorf("close claim rows: %w", err)
		}
		until := now.Add(lease)
		for _, candidate := range candidates {
			result, err := tx.ExecContext(ctx, `UPDATE worker_jobs SET status='running',lease_owner=?,lease_until=?,updated_at=?
				WHERE id=? AND status IN ('pending','retrying','running') AND (lease_until IS NULL OR lease_until<=?)`, owner, stamp(until), stamp(now), candidate.ID, stamp(now))
			if err != nil {
				return fmt.Errorf("claim worker job %s: %w", candidate.ID, err)
			}
			changed, _ := result.RowsAffected()
			if changed != 1 {
				continue
			}
			candidate.Status = StatusRunning
			candidate.LeaseOwner = owner
			candidate.LeaseUntil = &until
			candidate.UpdatedAt = now
			claimed = append(claimed, candidate)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return claimed, nil
}

func (r *Repository) Complete(ctx context.Context, id, owner string, now time.Time) error {
	result, err := r.db.ExecContext(ctx, `UPDATE worker_jobs SET status='completed',lease_owner=NULL,lease_until=NULL,last_error='',updated_at=? WHERE id=? AND status='running' AND lease_owner=?`, stamp(now), id, owner)
	if err != nil {
		return fmt.Errorf("complete worker job: %w", err)
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return apperr.New(apperr.CodeConflict, "worker job lease is no longer owned")
	}
	return nil
}

func (r *Repository) Fail(ctx context.Context, id, owner, message string, now time.Time) (*Job, error) {
	var updated *Job
	err := platformdb.WithTx(ctx, r.db, func(tx *sql.Tx) error {
		job, err := scanJob(tx.QueryRowContext(ctx, `SELECT `+columns+` FROM worker_jobs WHERE id=?`, id))
		if err != nil {
			return err
		}
		if job.Status != StatusRunning || job.LeaseOwner != owner {
			return apperr.New(apperr.CodeConflict, "worker job lease is no longer owned")
		}
		attempts := job.Attempts + 1
		status := StatusRetrying
		next := now.Add(RetryDelay(attempts))
		if attempts >= job.MaxAttempts {
			status = StatusFailed
			next = now
		}
		_, err = tx.ExecContext(ctx, `UPDATE worker_jobs SET status=?,attempts=?,next_attempt_at=?,lease_owner=NULL,lease_until=NULL,last_error=?,updated_at=? WHERE id=? AND status='running' AND lease_owner=?`, status, attempts, stamp(next), message, stamp(now), job.ID, owner)
		if err != nil {
			return fmt.Errorf("fail worker job: %w", err)
		}
		job.Status = status
		job.Attempts = attempts
		job.NextAttemptAt = next
		job.LeaseOwner = ""
		job.LeaseUntil = nil
		job.LastError = message
		job.UpdatedAt = now
		updated = job
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func (r *Repository) Cancel(ctx context.Context, id string, now time.Time) error {
	result, err := r.db.ExecContext(ctx, `UPDATE worker_jobs SET status='canceled',lease_owner=NULL,lease_until=NULL,updated_at=? WHERE id=? AND status IN ('pending','retrying')`, stamp(now), id)
	if err != nil {
		return fmt.Errorf("cancel worker job: %w", err)
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return apperr.New(apperr.CodeConflict, "worker job cannot be canceled")
	}
	return nil
}

func (r *Repository) RequeueExpiredLeases(ctx context.Context, now time.Time) (int64, error) {
	result, err := r.db.ExecContext(ctx, `UPDATE worker_jobs SET status='retrying',lease_owner=NULL,lease_until=NULL,next_attempt_at=?,updated_at=? WHERE status='running' AND lease_until<=?`, stamp(now), stamp(now), stamp(now))
	if err != nil {
		return 0, fmt.Errorf("requeue expired worker leases: %w", err)
	}
	return result.RowsAffected()
}

type scanner interface{ Scan(...any) error }

func scanJob(row scanner) (*Job, error) {
	var job Job
	var next, created, updated string
	var leaseOwner, leaseUntil sql.NullString
	err := row.Scan(&job.ID, &job.Kind, &job.ObjectID, &job.Payload, &job.Status, &job.Attempts, &job.MaxAttempts, &next, &leaseOwner, &leaseUntil, &job.LastError, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperr.New(apperr.CodeNotFound, "worker job not found")
	}
	if err != nil {
		return nil, fmt.Errorf("scan worker job: %w", err)
	}
	job.NextAttemptAt = parseStamp(next)
	job.CreatedAt = parseStamp(created)
	job.UpdatedAt = parseStamp(updated)
	job.LeaseOwner = leaseOwner.String
	if leaseUntil.Valid {
		value := parseStamp(leaseUntil.String)
		job.LeaseUntil = &value
	}
	return &job, nil
}

func stamp(value time.Time) string { return platformdb.Timestamp(value) }
func parseStamp(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, value)
	return parsed
}
