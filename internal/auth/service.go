package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"time"
	"urbanrelay/internal/apperr"
	"urbanrelay/internal/clock"
	"urbanrelay/internal/domain"
	"urbanrelay/internal/platformdb"
	"urbanrelay/internal/requestmeta"
)

type User struct {
	ID, Username, PasswordHash, Status string
	Role                               domain.Role
	CreatedAt, UpdatedAt               time.Time
}
type Session struct {
	TokenHash, UserID string
	ExpiresAt         time.Time
	RevokedAt         *time.Time
	CreatedAt         time.Time
}
type Service struct {
	db    *sql.DB
	clock clock.Clock
	ttl   time.Duration
}

func NewService(db *sql.DB, clk clock.Clock, ttl time.Duration) *Service {
	return &Service{db: db, clock: clk, ttl: ttl}
}

func HashPassword(password string) string {
	sum := sha256.Sum256([]byte("urbanrelay-password-v1:" + password))
	return hex.EncodeToString(sum[:])
}
func (s *Service) Login(ctx context.Context, username, password string) (string, time.Time, error) {
	var u User
	var role, created, updated string
	err := s.db.QueryRowContext(ctx, `SELECT id,username,password_hash,role,status,created_at,updated_at FROM users WHERE username=?`, username).Scan(&u.ID, &u.Username, &u.PasswordHash, &role, &u.Status, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return "", time.Time{}, apperr.New(apperr.CodeUnauthenticated, "invalid credentials")
	}
	if err != nil {
		return "", time.Time{}, fmt.Errorf("find login user: %w", err)
	}
	candidate := HashPassword(password)
	if subtle.ConstantTimeCompare([]byte(candidate), []byte(u.PasswordHash)) != 1 || u.Status != "active" {
		return "", time.Time{}, apperr.New(apperr.CodeUnauthenticated, "invalid credentials")
	}
	token, hash, err := newToken()
	if err != nil {
		return "", time.Time{}, err
	}
	now := s.clock.Now()
	expires := now.Add(s.ttl)
	_, err = s.db.ExecContext(context.Background(), `INSERT INTO sessions(token_hash,user_id,expires_at,created_at) VALUES(?,?,?,?)`, hash, u.ID, platformdb.Timestamp(expires), platformdb.Timestamp(now))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("save session: %w", err)
	}
	return token, expires, nil
}
func (s *Service) Authenticate(ctx context.Context, token string) (requestmeta.Actor, error) {
	hash := hashToken(token)
	var actor requestmeta.Actor
	var role, status, expires string
	var revoked sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT u.id,u.username,u.role,u.status,s.expires_at,s.revoked_at FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=?`, hash).Scan(&actor.UserID, &actor.Username, &role, &status, &expires, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return requestmeta.Actor{}, apperr.New(apperr.CodeUnauthenticated, "session not found")
	}
	if err != nil {
		return requestmeta.Actor{}, fmt.Errorf("authenticate session: %w", err)
	}
	expiry, err := time.Parse(time.RFC3339Nano, expires)
	if err != nil {
		return requestmeta.Actor{}, fmt.Errorf("parse session expiry: %w", err)
	}
	if revoked.Valid || !expiry.After(s.clock.Now()) || status != "active" {
		return requestmeta.Actor{}, apperr.New(apperr.CodeUnauthenticated, "session expired or revoked")
	}
	actor.Role = role
	return actor, nil
}
func (s *Service) Logout(ctx context.Context, token string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE sessions SET revoked_at=? WHERE token_hash=? AND revoked_at IS NULL`, platformdb.Timestamp(s.clock.Now()), hashToken(token))
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return apperr.New(apperr.CodeUnauthenticated, "session not found or revoked")
	}
	return nil
}
func (s *Service) Require(actor requestmeta.Actor, action string) error {
	role, err := domain.ParseRole(actor.Role)
	if err != nil || !role.Can(action) {
		return apperr.New(apperr.CodeForbidden, "role cannot perform this action")
	}
	return nil
}
func newToken() (string, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("generate session token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	return token, hashToken(token), nil
}
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
func SeedUsers(ctx context.Context, db *sql.DB, now time.Time) error {
	users := []struct {
		username, password string
		role               domain.Role
	}{{"platform_admin", "admin-demo-password", domain.RolePlatformAdmin}, {"transport_dispatcher", "dispatcher-demo-password", domain.RoleTransportDispatcher}, {"field_operator", "field-demo-password", domain.RoleFieldOperator}, {"safety_auditor", "auditor-demo-password", domain.RoleSafetyAuditor}}
	for _, u := range users {
		_, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO users(id,username,password_hash,role,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, uuid.NewString(), u.username, HashPassword(u.password), u.role, "active", platformdb.Timestamp(now), platformdb.Timestamp(now))
		if err != nil {
			return err
		}
	}
	return nil
}
