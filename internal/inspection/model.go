package inspection

import (
	"fmt"
	"strings"
	"time"
	"urbanrelay/internal/domain"
)

type Inspection struct {
	ID           string        `json:"id"`
	InspectionNo string        `json:"inspection_no"`
	HubID        string        `json:"hub_id"`
	AssetID      string        `json:"asset_id"`
	Priority     int           `json:"priority"`
	TargetRef    string        `json:"target_ref"`
	Status       domain.Status `json:"status"`
	Version      int           `json:"version"`
	RequestedBy  string        `json:"requested_by"`
	DueAt        time.Time     `json:"due_at"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
}

type CreateRequest struct {
	InspectionNo   string
	HubID          string
	AssetID        string
	Priority       int
	TargetRef      string
	RequestedBy    string
	DueAt          time.Time
	IdempotencyKey string
}

type TransitionRequest struct {
	ID              string
	Target          domain.Status
	ExpectedVersion int
	ActorID         string
	Note            string
}

var lifecycle = domain.NewGraph(map[domain.Status][]domain.Status{
	domain.StatusPlanned:        {domain.StatusReserved, domain.StatusDispatched, domain.StatusCanceled},
	domain.StatusReserved:       {domain.StatusDispatched, domain.StatusActive, domain.StatusCanceled},
	domain.StatusDispatched:     {domain.StatusActive, domain.StatusCanceled, domain.StatusFailed},
	domain.StatusActive:         {domain.StatusAwaitingReview, domain.StatusCompleted, domain.StatusFailed},
	domain.StatusAwaitingReview: {domain.StatusActive, domain.StatusCompleted, domain.StatusFailed},
	domain.StatusFailed:         {domain.StatusPlanned, domain.StatusCanceled},
})

func (r CreateRequest) Validate(now time.Time) error {
	if strings.TrimSpace(r.InspectionNo) == "" {
		return fmt.Errorf("inspection_no is required")
	}
	if strings.TrimSpace(r.HubID) == "" {
		return fmt.Errorf("hub_id is required")
	}
	if strings.TrimSpace(r.RequestedBy) == "" {
		return fmt.Errorf("requested_by is required")
	}
	if strings.TrimSpace(r.IdempotencyKey) == "" {
		return fmt.Errorf("idempotency key is required")
	}
	if r.Priority <= 0 {
		return fmt.Errorf("priority must be positive")
	}
	if strings.TrimSpace(r.TargetRef) == "" {
		return fmt.Errorf("target_ref is required")
	}
	if !r.DueAt.After(now) {
		return fmt.Errorf("DueAt must be in the future")
	}
	return nil
}

func (v Inspection) ValidateTransition(target domain.Status) error {
	return lifecycle.Validate(v.Status, target)
}
func (v Inspection) IsTerminal() bool {
	return v.Status == domain.StatusCompleted || v.Status == domain.StatusCanceled
}
func (v Inspection) CanReleaseResource() bool {
	return v.IsTerminal() || v.Status == domain.StatusFailed
}
