package sorting

import (
	"fmt"
	"strings"
	"time"
	"urbanrelay/internal/domain"
)

type Wave struct {
	ID            string        `json:"id"`
	WaveNo        string        `json:"wave_no"`
	HubID         string        `json:"hub_id"`
	ChuteCode     string        `json:"chute_code"`
	ExpectedItems int           `json:"expected_items"`
	Profile       string        `json:"profile"`
	Status        domain.Status `json:"status"`
	Version       int           `json:"version"`
	RequestedBy   string        `json:"requested_by"`
	ClosesAt      time.Time     `json:"closes_at"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
}

type CreateRequest struct {
	WaveNo         string
	HubID          string
	ChuteCode      string
	ExpectedItems  int
	Profile        string
	RequestedBy    string
	ClosesAt       time.Time
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
	if strings.TrimSpace(r.WaveNo) == "" {
		return fmt.Errorf("wave_no is required")
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
	if r.ExpectedItems <= 0 {
		return fmt.Errorf("expected_items must be positive")
	}
	if strings.TrimSpace(r.Profile) == "" {
		return fmt.Errorf("profile is required")
	}
	if !r.ClosesAt.After(now) {
		return fmt.Errorf("ClosesAt must be in the future")
	}
	return nil
}

func (v Wave) ValidateTransition(target domain.Status) error {
	return lifecycle.Validate(v.Status, target)
}
func (v Wave) IsTerminal() bool {
	return v.Status == domain.StatusCompleted || v.Status == domain.StatusCanceled
}
func (v Wave) CanReleaseResource() bool { return v.IsTerminal() || v.Status == domain.StatusFailed }
