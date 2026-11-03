package config

import "time"

// ProDevices is a rollout gate, never a source of subscription entitlement.
// ClientsReady is an operator acknowledgement after the documented client and
// existing-customer migration checks. It is intentionally false by default.
type ProDevices struct {
	Enabled              bool
	ShadowMode           bool
	ClientsReady         bool
	NotificationsEnabled bool
	MaxMobileDevices     int
	MaxDesktopDevices    int
	NotificationCooldown time.Duration
}

func proDevicesFromEnv() ProDevices {
	return ProDevices{
		Enabled:              boolean("PRO_DEVICE_LIMIT_ENABLED", false),
		ShadowMode:           boolean("PRO_DEVICE_LIMIT_SHADOW_MODE", true),
		ClientsReady:         boolean("PRO_DEVICE_LIMIT_CLIENTS_READY", false),
		NotificationsEnabled: boolean("PRO_DEVICE_LIMIT_NOTIFICATIONS_ENABLED", false),
		MaxMobileDevices:     num("PRO_MAX_MOBILE_DEVICES", 1),
		MaxDesktopDevices:    num("PRO_MAX_DESKTOP_DEVICES", 1),
		NotificationCooldown: dur("PRO_DEVICE_LIMIT_NOTIFICATION_COOLDOWN", time.Hour),
	}
}

func (p ProDevices) validate() []string {
	var problems []string
	// The database enforces precisely one slot per category. Configuration may
	// not relax this invariant. A zero value keeps older in-process callers off.
	if (p.Enabled || p.ShadowMode) && (p.MaxMobileDevices != 1 || p.MaxDesktopDevices != 1) {
		problems = append(problems, "PRO_MAX_MOBILE_DEVICES and PRO_MAX_DESKTOP_DEVICES must both be 1")
	}
	if p.Enabled && !p.ShadowMode && !p.ClientsReady {
		problems = append(problems, "strict PRO_DEVICE_LIMIT_ENABLED requires PRO_DEVICE_LIMIT_CLIENTS_READY=true after client compatibility and existing-account migration review")
	}
	if p.NotificationsEnabled && (!p.Enabled || p.ShadowMode || !p.ClientsReady) {
		problems = append(problems, "PRO_DEVICE_LIMIT_NOTIFICATIONS_ENABLED requires ready strict enforcement; shadow mode never sends blocked-device notifications")
	}
	if p.NotificationsEnabled && (p.NotificationCooldown < time.Minute || p.NotificationCooldown > 7*24*time.Hour) {
		problems = append(problems, "PRO_DEVICE_LIMIT_NOTIFICATION_COOLDOWN must be between 1m and 168h")
	}
	return problems
}
