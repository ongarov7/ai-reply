package admin

import "sort"

// Әкімші рұқсаттары. Рөл — рұқсаттар жиыны; тексеру тек серверде (батырманы жасыру — жай ыңғайлылық).
const (
	PermDashboardRead     = "dashboard.read"
	PermUsersRead         = "users.read"
	PermUsersWrite        = "users.write"
	PermPlansWrite        = "plans.write"
	PermSettingsRead      = "settings.read"
	PermSettingsWrite     = "settings.write"
	PermNotificationsRead = "notifications.read"
	PermNotificationsSend = "notifications.send"
	PermDiagnosticsRead   = "users.diagnostics.read"
	PermLogsRead          = "logs.read"
	PermAuditRead         = "audit_logs.read"
)

// Рөлдер (admin_users.role).
const (
	RoleAdmin  = "admin"
	RoleViewer = "viewer"
)

var allPermissions = []string{
	PermDashboardRead, PermUsersRead, PermUsersWrite, PermPlansWrite, PermSettingsRead, PermSettingsWrite,
	PermNotificationsRead, PermNotificationsSend, PermDiagnosticsRead, PermLogsRead, PermAuditRead,
}

// rolePermissions — viewer sees aggregates, masked user lists and campaign
// results; it cannot change anything, send pushes, open a person's
// diagnostics (full e-mail, phone, devices, sign-in history) or read logs.
var rolePermissions = map[string][]string{
	RoleAdmin:  allPermissions,
	RoleViewer: {PermDashboardRead, PermUsersRead, PermSettingsRead, PermNotificationsRead},
}

// Permissions — рөлдің рұқсаттары (белгісіз рөл — ештеңе).
func Permissions(role string) []string {
	out := append([]string(nil), rolePermissions[role]...)
	sort.Strings(out)
	return out
}

// Can — рөлде рұқсат бар ма.
func Can(role, permission string) bool {
	for _, p := range rolePermissions[role] {
		if p == permission {
			return true
		}
	}
	return false
}
