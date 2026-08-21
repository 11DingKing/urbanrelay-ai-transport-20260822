package middleware

import (
	"context"
	"github.com/google/uuid"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"
	"urbanrelay/internal/auth"
	"urbanrelay/internal/requestmeta"
)

func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = uuid.NewString()
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(requestmeta.WithRequestID(r.Context(), id)))
	})
}
func Logging(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		logger.InfoContext(r.Context(), "http request", "method", r.Method, "path", r.URL.Path, "request_id", requestmeta.RequestID(r.Context()), "duration_ms", time.Since(started).Milliseconds())
	})
}
func Recover(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if value := recover(); value != nil {
				logger.Error("http panic", "value", value, "stack", string(debug.Stack()), "request_id", requestmeta.RequestID(r.Context()))
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
func Authenticate(service *auth.Service, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		if len(token) > 7 && token[:7] == "Bearer " {
			token = token[7:]
		} else {
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}
		actor, err := service.Authenticate(r.Context(), token)
		if err != nil {
			http.Error(w, "invalid session", http.StatusUnauthorized)
			return
		}
		ctx := requestmeta.WithActor(r.Context(), actor)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
func WithTimeout(timeout time.Duration, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
