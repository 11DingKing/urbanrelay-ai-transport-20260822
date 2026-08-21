package domain

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestDefaultGraphAcceptsBusinessTransitions(t *testing.T) {
	graph := DefaultGraph()
	cases := []struct {
		name string
		from Status
		to   Status
	}{
		{"plan reservation", StatusPlanned, StatusReserved},
		{"plan dispatch", StatusPlanned, StatusDispatched},
		{"reservation dispatch", StatusReserved, StatusDispatched},
		{"reservation activation", StatusReserved, StatusActive},
		{"dispatch activation", StatusDispatched, StatusActive},
		{"active review", StatusActive, StatusAwaitingReview},
		{"active completion", StatusActive, StatusCompleted},
		{"review completion", StatusAwaitingReview, StatusCompleted},
		{"failure retry", StatusFailed, StatusPlanned},
		{"planned cancel", StatusPlanned, StatusCanceled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, graph.Validate(tc.from, tc.to))
		})
	}
}

func TestDefaultGraphRejectsInvalidTransitions(t *testing.T) {
	graph := DefaultGraph()
	cases := []struct {
		from Status
		to   Status
	}{
		{StatusCompleted, StatusActive},
		{StatusCanceled, StatusPlanned},
		{StatusPlanned, StatusCompleted},
		{StatusReserved, StatusCompleted},
		{StatusAwaitingReview, StatusDispatched},
		{StatusFailed, StatusCompleted},
		{StatusActive, StatusReserved},
		{StatusDispatched, StatusCompleted},
	}
	for _, tc := range cases {
		t.Run(string(tc.from)+"_to_"+string(tc.to), func(t *testing.T) {
			require.Error(t, graph.Validate(tc.from, tc.to))
		})
	}
}

func TestGraphCopiesInputEdges(t *testing.T) {
	edges := map[Status][]Status{StatusPlanned: {StatusReserved}}
	graph := NewGraph(edges)
	edges[StatusPlanned][0] = StatusCompleted
	edges[StatusPlanned] = append(edges[StatusPlanned], StatusCanceled)

	require.NoError(t, graph.Validate(StatusPlanned, StatusReserved))
	require.Error(t, graph.Validate(StatusPlanned, StatusCompleted))
	require.Error(t, graph.Validate(StatusPlanned, StatusCanceled))
}

func TestPageNormalize(t *testing.T) {
	cases := []struct {
		name string
		in   Page
		want Page
	}{
		{"empty", Page{}, Page{Limit: 50}},
		{"negative", Page{Limit: -5, Offset: -8}, Page{Limit: 50}},
		{"too large", Page{Limit: 1000, Offset: 20}, Page{Limit: 50, Offset: 20}},
		{"valid", Page{Limit: 25, Offset: 50, Status: StatusActive, Query: " route "}, Page{Limit: 25, Offset: 50, Status: StatusActive, Query: "route"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.in.Normalize())
		})
	}
}

func TestParseRole(t *testing.T) {
	roles := []Role{RolePlatformAdmin, RoleTransportDispatcher, RoleFieldOperator, RoleSafetyAuditor}
	for _, role := range roles {
		parsed, err := ParseRole(string(role))
		require.NoError(t, err)
		require.Equal(t, role, parsed)
	}
	_, err := ParseRole("root")
	require.Error(t, err)
}

func TestRolePermissions(t *testing.T) {
	cases := []struct {
		role   Role
		action string
		want   bool
	}{
		{RolePlatformAdmin, ActionManageUsers, true},
		{RolePlatformAdmin, ActionDispatchTransport, true},
		{RoleTransportDispatcher, ActionDispatchTransport, true},
		{RoleTransportDispatcher, ActionOperateField, false},
		{RoleFieldOperator, ActionOperateField, true},
		{RoleFieldOperator, ActionReviewSafety, false},
		{RoleSafetyAuditor, ActionReviewSafety, true},
		{RoleSafetyAuditor, ActionReadAudit, true},
		{RoleSafetyAuditor, ActionManageUsers, false},
	}
	for _, tc := range cases {
		t.Run(string(tc.role)+"_"+tc.action, func(t *testing.T) {
			require.Equal(t, tc.want, tc.role.Can(tc.action))
		})
	}
}

func TestAllStatusesAreRecognized(t *testing.T) {
	statuses := []Status{StatusPlanned, StatusReserved, StatusDispatched, StatusActive, StatusAwaitingReview, StatusCompleted, StatusFailed, StatusCanceled}
	for _, status := range statuses {
		require.True(t, status.Valid(), string(status))
	}
	require.False(t, Status("unknown").Valid())
}
