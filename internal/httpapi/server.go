package httpapi

import (
	"encoding/json"
	"errors"
	"github.com/go-chi/chi/v5"
	"log/slog"
	"net/http"
	"urbanrelay/internal/apperr"
	"urbanrelay/internal/auth"
	"urbanrelay/internal/incident"
	"urbanrelay/internal/inspection"
	"urbanrelay/internal/maintenance"
	"urbanrelay/internal/middleware"
	"urbanrelay/internal/mission"
	"urbanrelay/internal/platformdb"
	"urbanrelay/internal/requestmeta"
	"urbanrelay/internal/sorting"
	"urbanrelay/internal/transfer"
)

type Server struct {
	router      *chi.Mux
	db          *platformdb.DB
	auth        *auth.Service
	mission     *mission.Service
	inspection  *inspection.Service
	transfer    *transfer.Service
	sorting     *sorting.Service
	incident    *incident.Service
	maintenance *maintenance.Service
	logger      *slog.Logger
}

type BusinessServices struct {
	Mission     *mission.Service
	Inspection  *inspection.Service
	Transfer    *transfer.Service
	Sorting     *sorting.Service
	Incident    *incident.Service
	Maintenance *maintenance.Service
}

func New(db *platformdb.DB, authService *auth.Service, services BusinessServices, logger *slog.Logger) *Server {
	s := &Server{router: chi.NewRouter(), db: db, auth: authService, mission: services.Mission, inspection: services.Inspection, transfer: services.Transfer, sorting: services.Sorting, incident: services.Incident, maintenance: services.Maintenance, logger: logger}
	s.routes()
	return s
}
func (s *Server) Handler() http.Handler {
	return middleware.Recover(s.logger, middleware.RequestID(middleware.Logging(s.logger, s.router)))
}
func (s *Server) routes() {
	s.router.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	s.router.Get("/readyz", s.ready)
	s.router.Post("/api/v1/auth/login", s.login)
	s.router.Group(func(r chi.Router) {
		r.Use(func(next http.Handler) http.Handler { return middleware.Authenticate(s.auth, next) })
		r.Post("/api/v1/auth/logout", s.logout)
		r.Get("/api/v1/me", s.me)
		s.businessRoutes(r)
	})
}
func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	if err := s.db.Ping(r.Context()); err != nil {
		writeError(w, r, apperr.Wrap(apperr.CodeUnavailable, "database unavailable", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, apperr.Wrap(apperr.CodeInvalid, "invalid request", err))
		return
	}
	token, expires, err := s.auth.Login(r.Context(), req.Username, req.Password)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "expires_at": expires})
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get("Authorization")[7:]
	if err := s.auth.Logout(r.Context(), token); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	actor, _ := requestmeta.ActorFrom(r.Context())
	writeJSON(w, http.StatusOK, actor)
}
func decodeJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return nil
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	code := apperr.CodeOf(err)
	status := http.StatusInternalServerError
	switch code {
	case apperr.CodeInvalid:
		status = http.StatusBadRequest
	case apperr.CodeUnauthenticated:
		status = http.StatusUnauthorized
	case apperr.CodeForbidden:
		status = http.StatusForbidden
	case apperr.CodeNotFound:
		status = http.StatusNotFound
	case apperr.CodeConflict:
		status = http.StatusConflict
	case apperr.CodeUnavailable:
		status = http.StatusServiceUnavailable
	}
	message := "internal error"
	var appErr *apperr.Error
	if errors.As(err, &appErr) {
		message = appErr.Message
	}
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": string(code), "message": message, "request_id": requestmeta.RequestID(r.Context())}})
}
