package reservation

import (
	"fmt"
	"strings"
	"time"
)

type Reservation struct {
	ID           string    `json:"id"`
	ResourceType string    `json:"resource_type"`
	ResourceKey  string    `json:"resource_key"`
	OwnerType    string    `json:"owner_type"`
	OwnerID      string    `json:"owner_id"`
	StartsAt     time.Time `json:"starts_at"`
	EndsAt       time.Time `json:"ends_at"`
	Status       string    `json:"status"`
	Version      int       `json:"version"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type ReserveRequest struct {
	ResourceType string
	ResourceKey  string
	OwnerType    string
	OwnerID      string
	StartsAt     time.Time
	EndsAt       time.Time
	ActorID      string
}

type WindowQuery struct {
	ResourceType string
	ResourceKey  string
	StartsAt     time.Time
	EndsAt       time.Time
	Limit        int
}

func (r ReserveRequest) Validate(now time.Time) error {
	if strings.TrimSpace(r.ResourceType) == "" || strings.TrimSpace(r.ResourceKey) == "" {
		return fmt.Errorf("resource type and key are required")
	}
	if strings.TrimSpace(r.OwnerType) == "" || strings.TrimSpace(r.OwnerID) == "" {
		return fmt.Errorf("owner type and id are required")
	}
	if strings.TrimSpace(r.ActorID) == "" {
		return fmt.Errorf("actor is required")
	}
	if !r.EndsAt.After(r.StartsAt) {
		return fmt.Errorf("reservation end must be after start")
	}
	if !r.EndsAt.After(now) {
		return fmt.Errorf("reservation window has already ended")
	}
	if r.EndsAt.Sub(r.StartsAt) > 72*time.Hour {
		return fmt.Errorf("reservation window exceeds 72 hours")
	}
	return nil
}

func (q WindowQuery) Validate() error {
	if q.ResourceType == "" || q.ResourceKey == "" {
		return fmt.Errorf("resource type and key are required")
	}
	if !q.EndsAt.After(q.StartsAt) {
		return fmt.Errorf("window end must be after start")
	}
	return nil
}

func Overlaps(leftStart, leftEnd, rightStart, rightEnd time.Time) bool {
	return leftStart.Before(rightEnd) && rightStart.Before(leftEnd)
}
