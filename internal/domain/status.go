package domain

import (
	"fmt"
	"strings"
	"time"
)

type Status string

const (
	StatusDraft          Status = "draft"
	StatusPlanned        Status = "planned"
	StatusReserved       Status = "reserved"
	StatusDispatched     Status = "dispatched"
	StatusActive         Status = "active"
	StatusAwaitingReview Status = "awaiting_review"
	StatusCompleted      Status = "completed"
	StatusCanceled       Status = "canceled"
	StatusFailed         Status = "failed"
)

type TransitionGraph map[Status]map[Status]struct{}

func DefaultGraph() TransitionGraph {
	return NewGraph(map[Status][]Status{
		StatusPlanned:        {StatusReserved, StatusDispatched, StatusCanceled},
		StatusReserved:       {StatusDispatched, StatusActive, StatusCanceled},
		StatusDispatched:     {StatusActive, StatusCanceled, StatusFailed},
		StatusActive:         {StatusAwaitingReview, StatusCompleted, StatusFailed},
		StatusAwaitingReview: {StatusActive, StatusCompleted, StatusFailed},
		StatusFailed:         {StatusPlanned, StatusCanceled},
	})
}

func (s Status) Valid() bool {
	switch s {
	case StatusDraft, StatusPlanned, StatusReserved, StatusDispatched, StatusActive, StatusAwaitingReview, StatusCompleted, StatusCanceled, StatusFailed:
		return true
	default:
		return false
	}
}

func NewGraph(edges map[Status][]Status) TransitionGraph {
	graph := make(TransitionGraph, len(edges))
	for from, targets := range edges {
		graph[from] = make(map[Status]struct{}, len(targets))
		for _, target := range targets {
			graph[from][target] = struct{}{}
		}
	}
	return graph
}

func (g TransitionGraph) Validate(from, to Status) error {
	if from == to {
		return fmt.Errorf("status already %s", to)
	}
	targets, ok := g[from]
	if !ok {
		return fmt.Errorf("status %s has no outgoing transition", from)
	}
	if _, ok := targets[to]; !ok {
		return fmt.Errorf("transition %s -> %s is not allowed", from, to)
	}
	return nil
}

type Page struct {
	Limit  int
	Offset int
	Status Status
	Query  string
}

func (p Page) Normalize() Page {
	if p.Limit <= 0 {
		p.Limit = 50
	}
	if p.Limit > 200 {
		p.Limit = 50
	}
	if p.Offset < 0 {
		p.Offset = 0
	}
	p.Query = strings.TrimSpace(p.Query)
	return p
}

type ResourceWindow struct {
	StartsAt time.Time
	EndsAt   time.Time
}

func (w ResourceWindow) Validate() error {
	if w.StartsAt.IsZero() || w.EndsAt.IsZero() {
		return fmt.Errorf("resource window timestamps are required")
	}
	if !w.EndsAt.After(w.StartsAt) {
		return fmt.Errorf("resource window end must be after start")
	}
	if w.EndsAt.Sub(w.StartsAt) > 48*time.Hour {
		return fmt.Errorf("resource window exceeds 48 hours")
	}
	return nil
}
