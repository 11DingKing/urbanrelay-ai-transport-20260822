package mission

import (
	"fmt"
	"strings"
	"time"
	"urbanrelay/internal/domain"
)

type Mission struct {
	ID           string        `json:"id"`
	BusinessNo   string        `json:"business_no"`
	HubID        string        `json:"hub_id"`
	AssetID      string        `json:"asset_id"`
	PayloadUnits int           `json:"payload_units"`
	RouteCode    string        `json:"route_code"`
	Status       domain.Status `json:"status"`
	Version      int           `json:"version"`
	RequestedBy  string        `json:"requested_by"`
	DueAt        time.Time     `json:"due_at"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
}

type CreateRequest struct {
	BusinessNo     string
	HubID          string
	AssetID        string
	PayloadUnits   int
	RouteCode      string
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
	if strings.TrimSpace(r.BusinessNo) == "" {
		return fmt.Errorf("business_no is required")
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
	if r.PayloadUnits <= 0 {
		return fmt.Errorf("payload_units must be positive")
	}
	if strings.TrimSpace(r.RouteCode) == "" {
		return fmt.Errorf("route_code is required")
	}
	if !r.DueAt.After(now) {
		return fmt.Errorf("DueAt must be in the future")
	}
	return nil
}

func (v Mission) ValidateTransition(target domain.Status) error {
	return lifecycle.Validate(v.Status, target)
}
func (v Mission) IsTerminal() bool {
	return v.Status == domain.StatusCompleted || v.Status == domain.StatusCanceled
}
func (v Mission) CanReleaseResource() bool { return v.IsTerminal() || v.Status == domain.StatusFailed }
