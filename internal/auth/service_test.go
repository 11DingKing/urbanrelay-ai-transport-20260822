package auth

import (
	"context"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
	"time"
	"urbanrelay/internal/apperr"
	"urbanrelay/internal/clock"
	"urbanrelay/internal/domain"
	"urbanrelay/internal/platformdb"
	"urbanrelay/internal/requestmeta"
)

type authFixture struct {
	db      *platformdb.DB
	clock   *clock.Fake
	service *Service
}

func newAuthFixture(t *testing.T) *authFixture {
	t.Helper()
	ctx := context.Background()
	db, err := platformdb.Open(ctx, filepath.Join(t.TempDir(), "data"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	clk := clock.NewFake(time.Date(2026, 8, 21, 9, 0, 0, 0, time.UTC))
	require.NoError(t, SeedUsers(ctx, db.SQL, clk.Now()))
	return &authFixture{db: db, clock: clk, service: NewService(db.SQL, clk, 2*time.Hour)}
}

func TestSeedUsersCreatesEveryBusinessRole(t *testing.T) {
	fixture := newAuthFixture(t)
	rows, err := fixture.db.SQL.Query(`SELECT username,role,status FROM users ORDER BY username`)
	require.NoError(t, err)
	defer rows.Close()
	roles := map[string]string{}
	for rows.Next() {
		var username, role, status string
		require.NoError(t, rows.Scan(&username, &role, &status))
		require.Equal(t, "active", status)
		roles[username] = role
	}
	require.Equal(t, map[string]string{
		"platform_admin":       string(domain.RolePlatformAdmin),
		"transport_dispatcher": string(domain.RoleTransportDispatcher),
		"field_operator":       string(domain.RoleFieldOperator),
		"safety_auditor":       string(domain.RoleSafetyAuditor),
	}, roles)
}

func TestSeedUsersIsIdempotent(t *testing.T) {
	fixture := newAuthFixture(t)
	require.NoError(t, SeedUsers(context.Background(), fixture.db.SQL, fixture.clock.Now()))
	var count int
	require.NoError(t, fixture.db.SQL.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count))
	require.Equal(t, 4, count)
}

func TestLoginAuthenticateAndLogoutLifecycle(t *testing.T) {
	fixture := newAuthFixture(t)
	ctx := context.Background()
	token, expires, err := fixture.service.Login(ctx, "transport_dispatcher", "dispatcher-demo-password")
	require.NoError(t, err)
	require.NotEmpty(t, token)
	require.Equal(t, fixture.clock.Now().Add(2*time.Hour), expires)

	actor, err := fixture.service.Authenticate(ctx, token)
	require.NoError(t, err)
	require.NotEmpty(t, actor.UserID)
	require.Equal(t, "transport_dispatcher", actor.Username)
	require.Equal(t, string(domain.RoleTransportDispatcher), actor.Role)

	require.NoError(t, fixture.service.Logout(ctx, token))
	_, err = fixture.service.Authenticate(ctx, token)
	require.Error(t, err)
	require.Equal(t, apperr.CodeUnauthenticated, apperr.CodeOf(err))
}

func TestLoginRejectsWrongCredentialsWithoutCreatingSession(t *testing.T) {
	fixture := newAuthFixture(t)
	ctx := context.Background()
	cases := []struct{ username, password string }{
		{"transport_dispatcher", "wrong"},
		{"missing", "dispatcher-demo-password"},
		{"", ""},
	}
	for _, tc := range cases {
		t.Run(tc.username+tc.password, func(t *testing.T) {
			token, _, err := fixture.service.Login(ctx, tc.username, tc.password)
			require.Error(t, err)
			require.Empty(t, token)
			require.Equal(t, apperr.CodeUnauthenticated, apperr.CodeOf(err))
		})
	}
	var count int
	require.NoError(t, fixture.db.SQL.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&count))
	require.Zero(t, count)
}

func TestExpiredSessionCannotAuthenticate(t *testing.T) {
	fixture := newAuthFixture(t)
	ctx := context.Background()
	token, _, err := fixture.service.Login(ctx, "field_operator", "field-demo-password")
	require.NoError(t, err)
	fixture.clock.Advance(2*time.Hour + time.Nanosecond)

	_, err = fixture.service.Authenticate(ctx, token)
	require.Error(t, err)
	require.Equal(t, apperr.CodeUnauthenticated, apperr.CodeOf(err))
}

func TestDisabledUserCannotLoginOrKeepSession(t *testing.T) {
	fixture := newAuthFixture(t)
	ctx := context.Background()
	token, _, err := fixture.service.Login(ctx, "safety_auditor", "auditor-demo-password")
	require.NoError(t, err)
	_, err = fixture.db.SQL.Exec(`UPDATE users SET status='disabled' WHERE username='safety_auditor'`)
	require.NoError(t, err)

	_, _, err = fixture.service.Login(ctx, "safety_auditor", "auditor-demo-password")
	require.Error(t, err)
	_, err = fixture.service.Authenticate(ctx, token)
	require.Error(t, err)
	require.Equal(t, apperr.CodeUnauthenticated, apperr.CodeOf(err))
}

func TestUnknownAndRepeatedLogoutAreRejected(t *testing.T) {
	fixture := newAuthFixture(t)
	ctx := context.Background()
	require.Error(t, fixture.service.Logout(ctx, "unknown-token"))

	token, _, err := fixture.service.Login(ctx, "platform_admin", "admin-demo-password")
	require.NoError(t, err)
	require.NoError(t, fixture.service.Logout(ctx, token))
	require.Error(t, fixture.service.Logout(ctx, token))
}

func TestLoginCreatesDistinctTokens(t *testing.T) {
	fixture := newAuthFixture(t)
	ctx := context.Background()
	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		token, _, err := fixture.service.Login(ctx, "field_operator", "field-demo-password")
		require.NoError(t, err)
		require.False(t, seen[token])
		seen[token] = true
	}
	var count int
	require.NoError(t, fixture.db.SQL.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&count))
	require.Equal(t, 20, count)
}

func TestRequireAppliesRolePermissions(t *testing.T) {
	fixture := newAuthFixture(t)
	cases := []struct {
		actor  requestmeta.Actor
		action string
		code   apperr.Code
	}{
		{requestmeta.Actor{Role: string(domain.RolePlatformAdmin)}, domain.ActionManageUsers, ""},
		{requestmeta.Actor{Role: string(domain.RoleTransportDispatcher)}, domain.ActionDispatchTransport, ""},
		{requestmeta.Actor{Role: string(domain.RoleTransportDispatcher)}, domain.ActionOperateField, apperr.CodeForbidden},
		{requestmeta.Actor{Role: string(domain.RoleFieldOperator)}, domain.ActionOperateField, ""},
		{requestmeta.Actor{Role: string(domain.RoleSafetyAuditor)}, domain.ActionReadAudit, ""},
		{requestmeta.Actor{Role: "unknown"}, "read", apperr.CodeForbidden},
	}
	for _, tc := range cases {
		err := fixture.service.Require(tc.actor, tc.action)
		if tc.code == "" {
			require.NoError(t, err)
		} else {
			require.Equal(t, tc.code, apperr.CodeOf(err))
		}
	}
}

func TestHashPasswordIsStableAndDoesNotReturnPlaintext(t *testing.T) {
	first := HashPassword("secret-value")
	second := HashPassword("secret-value")
	other := HashPassword("other-value")
	require.Equal(t, first, second)
	require.NotEqual(t, first, other)
	require.NotEqual(t, "secret-value", first)
	require.Len(t, first, 64)
}

func TestAuthenticateHonorsCanceledContext(t *testing.T) {
	fixture := newAuthFixture(t)
	ctx := context.Background()
	token, _, err := fixture.service.Login(ctx, "field_operator", "field-demo-password")
	require.NoError(t, err)
	canceled, cancel := context.WithCancel(ctx)
	cancel()

	_, err = fixture.service.Authenticate(canceled, token)
	require.Error(t, err)
}
