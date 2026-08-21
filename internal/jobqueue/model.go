package jobqueue

import (
	"fmt"
	"strings"
	"time"
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusRetrying  Status = "retrying"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusCanceled  Status = "canceled"
)

type Job struct {
	ID            string     `json:"id"`
	Kind          string     `json:"kind"`
	ObjectID      string     `json:"object_id"`
	Payload       string     `json:"payload"`
	Status        Status     `json:"status"`
	Attempts      int        `json:"attempts"`
	MaxAttempts   int        `json:"max_attempts"`
	NextAttemptAt time.Time  `json:"next_attempt_at"`
	LeaseOwner    string     `json:"lease_owner,omitempty"`
	LeaseUntil    *time.Time `json:"lease_until,omitempty"`
	LastError     string     `json:"last_error,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type EnqueueRequest struct {
	Kind          string
	ObjectID      string
	Payload       string
	MaxAttempts   int
	NextAttemptAt time.Time
}

func (r EnqueueRequest) Validate() error {
	if strings.TrimSpace(r.Kind) == "" || strings.TrimSpace(r.ObjectID) == "" {
		return fmt.Errorf("job kind and object id are required")
	}
	if strings.TrimSpace(r.Payload) == "" {
		return fmt.Errorf("job payload is required")
	}
	if r.MaxAttempts < 1 || r.MaxAttempts > 20 {
		return fmt.Errorf("max attempts must be between 1 and 20")
	}
	return nil
}

func (j Job) LeaseExpired(now time.Time) bool {
	return j.LeaseUntil == nil || !j.LeaseUntil.After(now)
}

func RetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 8 {
		attempt = 8
	}
	return time.Duration(1<<(attempt-1)) * time.Second
}
