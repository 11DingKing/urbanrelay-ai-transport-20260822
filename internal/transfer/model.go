package transfer

import (
	"fmt"
	"strings"
	"time"
	"urbanrelay/internal/domain"
)

type Transfer struct {
	ID               string        `json:"id"`
	PlanNo           string        `json:"plan_no"`
	OriginHubID      string        `json:"origin_hub_id"`
	DestinationHubID string        `json:"destination_hub_id"`
	SegmentCount     int           `json:"segment_count"`
	ModeChain        string        `json:"mode_chain"`
	Status           domain.Status `json:"status"`
	Version          int           `json:"version"`
	RequestedBy      string        `json:"requested_by"`
	ConnectionAt     time.Time     `json:"connection_at"`
	CreatedAt        time.Time     `json:"created_at"`
	UpdatedAt        time.Time     `json:"updated_at"`
}

type CreateRequest struct {
	PlanNo           string
	OriginHubID      string
	DestinationHubID string
	SegmentCount     int
	ModeChain        string
	RequestedBy      string
	ConnectionAt     time.Time
	IdempotencyKey   string
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
	if strings.TrimSpace(r.PlanNo) == "" {
		return fmt.Errorf("plan_no is required")
	}
	if strings.TrimSpace(r.OriginHubID) == "" {
		return fmt.Errorf("origin_hub_id is required")
	}
	if strings.TrimSpace(r.RequestedBy) == "" {
		return fmt.Errorf("requested_by is required")
	}
	if strings.TrimSpace(r.IdempotencyKey) == "" {
		return fmt.Errorf("idempotency key is required")
	}
	if r.SegmentCount <= 0 {
		return fmt.Errorf("segment_count must be positive")
	}
	if strings.TrimSpace(r.ModeChain) == "" {
		return fmt.Errorf("mode_chain is required")
	}
	if !r.ConnectionAt.After(now) {
		return fmt.Errorf("ConnectionAt must be in the future")
	}
	return nil
}

func (v Transfer) ValidateTransition(target domain.Status) error {
	return lifecycle.Validate(v.Status, target)
}
func (v Transfer) IsTerminal() bool {
	return v.Status == domain.StatusCompleted || v.Status == domain.StatusCanceled
}
func (v Transfer) CanReleaseResource() bool { return v.IsTerminal() || v.Status == domain.StatusFailed }
