package httpapi

import (
	"github.com/go-chi/chi/v5"
	"net/http"
	"time"
	"urbanrelay/internal/apperr"
	"urbanrelay/internal/domain"
	"urbanrelay/internal/incident"
	"urbanrelay/internal/inspection"
	"urbanrelay/internal/maintenance"
	"urbanrelay/internal/mission"
	"urbanrelay/internal/requestmeta"
	"urbanrelay/internal/sorting"
	"urbanrelay/internal/transfer"
)

type createRequest struct {
	BusinessNo    string    `json:"business_no"`
	ParentID      string    `json:"parent_id"`
	ResourceID    string    `json:"resource_id"`
	Amount        int       `json:"amount"`
	Detail        string    `json:"detail"`
	DestinationID string    `json:"destination_id,omitempty"`
	DueAt         time.Time `json:"due_at"`
}

type transitionRequest struct {
	Target          domain.Status `json:"target"`
	ExpectedVersion int           `json:"expected_version"`
	Note            string        `json:"note"`
}

func (s *Server) businessRoutes(r chi.Router) {
	r.Post("/api/v1/missions", s.createMission)
	r.Get("/api/v1/missions/{id}", s.getMission)
	r.Post("/api/v1/missions/{id}/transitions", s.transitionMission)
	r.Post("/api/v1/inspections", s.createInspection)
	r.Post("/api/v1/inspections/{id}/transitions", s.transitionInspection)
	r.Post("/api/v1/transfers", s.createTransfer)
	r.Post("/api/v1/transfers/{id}/transitions", s.transitionTransfer)
	r.Post("/api/v1/sorting-waves", s.createSorting)
	r.Post("/api/v1/sorting-waves/{id}/transitions", s.transitionSorting)
	r.Post("/api/v1/incidents", s.createIncident)
	r.Post("/api/v1/incidents/{id}/transitions", s.transitionIncident)
	r.Post("/api/v1/maintenance", s.createMaintenance)
	r.Post("/api/v1/maintenance/{id}/transitions", s.transitionMaintenance)
}

func actorFor(r *http.Request) (requestmeta.Actor, error) {
	actor, ok := requestmeta.ActorFrom(r.Context())
	if !ok {
		return requestmeta.Actor{}, apperr.New(apperr.CodeUnauthenticated, "actor is missing")
	}
	return actor, nil
}

func decodeCreate(r *http.Request) (createRequest, requestmeta.Actor, string, error) {
	var req createRequest
	if err := decodeJSON(r, &req); err != nil {
		return createRequest{}, requestmeta.Actor{}, "", apperr.Wrap(apperr.CodeInvalid, "invalid request", err)
	}
	actor, err := actorFor(r)
	if err != nil {
		return createRequest{}, requestmeta.Actor{}, "", err
	}
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		return createRequest{}, requestmeta.Actor{}, "", apperr.New(apperr.CodeInvalid, "Idempotency-Key header is required")
	}
	return req, actor, key, nil
}

func decodeTransition(r *http.Request) (transitionRequest, requestmeta.Actor, error) {
	var req transitionRequest
	if err := decodeJSON(r, &req); err != nil {
		return transitionRequest{}, requestmeta.Actor{}, apperr.Wrap(apperr.CodeInvalid, "invalid request", err)
	}
	actor, err := actorFor(r)
	return req, actor, err
}

func writeServiceResult(w http.ResponseWriter, r *http.Request, status int, value any, err error) {
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, status, value)
}

func (s *Server) require(actor requestmeta.Actor, action string) error {
	return s.auth.Require(actor, action)
}

func transitionAction(target domain.Status) string {
	switch target {
	case domain.StatusReserved:
		return "reserve"
	case domain.StatusDispatched:
		return "dispatch"
	case domain.StatusActive:
		return "start"
	case domain.StatusAwaitingReview, domain.StatusCompleted:
		return "complete"
	case domain.StatusCanceled:
		return "cancel"
	default:
		return "review"
	}
}

func (s *Server) createMission(w http.ResponseWriter, r *http.Request) {
	req, actor, key, err := decodeCreate(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if err := s.require(actor, "plan"); err != nil {
		writeError(w, r, err)
		return
	}
	value, err := s.mission.Create(r.Context(), mission.CreateRequest{BusinessNo: req.BusinessNo, HubID: req.ParentID, AssetID: req.ResourceID, PayloadUnits: req.Amount, RouteCode: req.Detail, RequestedBy: actor.UserID, DueAt: req.DueAt, IdempotencyKey: key})
	writeServiceResult(w, r, http.StatusCreated, value, err)
}

func (s *Server) getMission(w http.ResponseWriter, r *http.Request) {
	value, err := s.mission.Get(r.Context(), chi.URLParam(r, "id"))
	writeServiceResult(w, r, http.StatusOK, value, err)
}

func (s *Server) transitionMission(w http.ResponseWriter, r *http.Request) {
	req, actor, err := decodeTransition(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if err := s.require(actor, transitionAction(req.Target)); err != nil {
		writeError(w, r, err)
		return
	}
	value, err := s.mission.Transition(r.Context(), mission.TransitionRequest{ID: chi.URLParam(r, "id"), Target: req.Target, ExpectedVersion: req.ExpectedVersion, ActorID: actor.UserID, Note: req.Note})
	writeServiceResult(w, r, http.StatusOK, value, err)
}

func (s *Server) createInspection(w http.ResponseWriter, r *http.Request) {
	req, actor, key, err := decodeCreate(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if err := s.require(actor, "plan"); err != nil {
		writeError(w, r, err)
		return
	}
	value, err := s.inspection.Create(r.Context(), inspection.CreateRequest{InspectionNo: req.BusinessNo, HubID: req.ParentID, AssetID: req.ResourceID, Priority: req.Amount, TargetRef: req.Detail, RequestedBy: actor.UserID, DueAt: req.DueAt, IdempotencyKey: key})
	writeServiceResult(w, r, http.StatusCreated, value, err)
}

func (s *Server) transitionInspection(w http.ResponseWriter, r *http.Request) {
	req, actor, err := decodeTransition(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if err := s.require(actor, transitionAction(req.Target)); err != nil {
		writeError(w, r, err)
		return
	}
	value, err := s.inspection.Transition(r.Context(), inspection.TransitionRequest{ID: chi.URLParam(r, "id"), Target: req.Target, ExpectedVersion: req.ExpectedVersion, ActorID: actor.UserID, Note: req.Note})
	writeServiceResult(w, r, http.StatusOK, value, err)
}

func (s *Server) createTransfer(w http.ResponseWriter, r *http.Request) {
	req, actor, key, err := decodeCreate(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if err := s.require(actor, "plan"); err != nil {
		writeError(w, r, err)
		return
	}
	value, err := s.transfer.Create(r.Context(), transfer.CreateRequest{PlanNo: req.BusinessNo, OriginHubID: req.ParentID, DestinationHubID: req.DestinationID, SegmentCount: req.Amount, ModeChain: req.Detail, RequestedBy: actor.UserID, ConnectionAt: req.DueAt, IdempotencyKey: key})
	writeServiceResult(w, r, http.StatusCreated, value, err)
}

func (s *Server) transitionTransfer(w http.ResponseWriter, r *http.Request) {
	req, actor, err := decodeTransition(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if err := s.require(actor, transitionAction(req.Target)); err != nil {
		writeError(w, r, err)
		return
	}
	value, err := s.transfer.Transition(r.Context(), transfer.TransitionRequest{ID: chi.URLParam(r, "id"), Target: req.Target, ExpectedVersion: req.ExpectedVersion, ActorID: actor.UserID, Note: req.Note})
	writeServiceResult(w, r, http.StatusOK, value, err)
}

func (s *Server) createSorting(w http.ResponseWriter, r *http.Request) {
	req, actor, key, err := decodeCreate(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if err := s.require(actor, "plan"); err != nil {
		writeError(w, r, err)
		return
	}
	value, err := s.sorting.Create(r.Context(), sorting.CreateRequest{WaveNo: req.BusinessNo, HubID: req.ParentID, ChuteCode: req.ResourceID, ExpectedItems: req.Amount, Profile: req.Detail, RequestedBy: actor.UserID, ClosesAt: req.DueAt, IdempotencyKey: key})
	writeServiceResult(w, r, http.StatusCreated, value, err)
}

func (s *Server) transitionSorting(w http.ResponseWriter, r *http.Request) {
	req, actor, err := decodeTransition(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if err := s.require(actor, transitionAction(req.Target)); err != nil {
		writeError(w, r, err)
		return
	}
	value, err := s.sorting.Transition(r.Context(), sorting.TransitionRequest{ID: chi.URLParam(r, "id"), Target: req.Target, ExpectedVersion: req.ExpectedVersion, ActorID: actor.UserID, Note: req.Note})
	writeServiceResult(w, r, http.StatusOK, value, err)
}

func (s *Server) createIncident(w http.ResponseWriter, r *http.Request) {
	req, actor, key, err := decodeCreate(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if err := s.require(actor, "plan"); err != nil {
		writeError(w, r, err)
		return
	}
	value, err := s.incident.Create(r.Context(), incident.CreateRequest{IncidentNo: req.BusinessNo, HubID: req.ParentID, AssignedTo: req.ResourceID, RiskScore: req.Amount, Category: req.Detail, RequestedBy: actor.UserID, DueAt: req.DueAt, IdempotencyKey: key})
	writeServiceResult(w, r, http.StatusCreated, value, err)
}

func (s *Server) transitionIncident(w http.ResponseWriter, r *http.Request) {
	req, actor, err := decodeTransition(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if err := s.require(actor, transitionAction(req.Target)); err != nil {
		writeError(w, r, err)
		return
	}
	value, err := s.incident.Transition(r.Context(), incident.TransitionRequest{ID: chi.URLParam(r, "id"), Target: req.Target, ExpectedVersion: req.ExpectedVersion, ActorID: actor.UserID, Note: req.Note})
	writeServiceResult(w, r, http.StatusOK, value, err)
}

func (s *Server) createMaintenance(w http.ResponseWriter, r *http.Request) {
	req, actor, key, err := decodeCreate(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if err := s.require(actor, "plan"); err != nil {
		writeError(w, r, err)
		return
	}
	value, err := s.maintenance.Create(r.Context(), maintenance.CreateRequest{TaskNo: req.BusinessNo, HubID: req.ParentID, AssetID: req.ResourceID, EstimatedMinutes: req.Amount, Reason: req.Detail, RequestedBy: actor.UserID, DueAt: req.DueAt, IdempotencyKey: key})
	writeServiceResult(w, r, http.StatusCreated, value, err)
}

func (s *Server) transitionMaintenance(w http.ResponseWriter, r *http.Request) {
	req, actor, err := decodeTransition(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if err := s.require(actor, transitionAction(req.Target)); err != nil {
		writeError(w, r, err)
		return
	}
	value, err := s.maintenance.Transition(r.Context(), maintenance.TransitionRequest{ID: chi.URLParam(r, "id"), Target: req.Target, ExpectedVersion: req.ExpectedVersion, ActorID: actor.UserID, Note: req.Note})
	writeServiceResult(w, r, http.StatusOK, value, err)
}
