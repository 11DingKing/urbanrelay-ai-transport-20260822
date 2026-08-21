package domain

import "fmt"

type Role string

const (
	ActionManageUsers       = "manage_users"
	ActionDispatchTransport = "dispatch"
	ActionOperateField      = "start"
	ActionReviewSafety      = "review"
	ActionReadAudit         = "audit"
)

const (
	RolePlatformAdmin       Role = "platform_admin"
	RoleTransportDispatcher Role = "transport_dispatcher"
	RoleFieldOperator       Role = "field_operator"
	RoleSafetyAuditor       Role = "safety_auditor"
)

func ParseRole(value string) (Role, error) {
	role := Role(value)
	switch role {
	case RolePlatformAdmin, RoleTransportDispatcher, RoleFieldOperator, RoleSafetyAuditor:
		return role, nil
	default:
		return "", fmt.Errorf("unknown role %q", value)
	}
}

func (r Role) Can(action string) bool {
	if r == RolePlatformAdmin {
		return true
	}
	permissions := map[Role]map[string]bool{
		RoleTransportDispatcher: {"plan": true, "reserve": true, "dispatch": true, "cancel": true, "read": true},
		RoleFieldOperator:       {"start": true, "report": true, "complete": true, "read": true},
		RoleSafetyAuditor:       {"review": true, "close": true, "read": true, "audit": true},
	}
	return permissions[r][action]
}
