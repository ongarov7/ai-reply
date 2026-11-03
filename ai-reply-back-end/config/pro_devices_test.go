package config

import (
	"testing"
	"time"
)

func TestProDeviceRolloutGates(t *testing.T) {
	base := ProDevices{ShadowMode: true, MaxMobileDevices: 1, MaxDesktopDevices: 1, NotificationCooldown: time.Hour}
	if p := base.validate(); len(p) != 0 {
		t.Fatal(p)
	}
	for _, tc := range []struct {
		name string
		edit func(*ProDevices)
	}{
		{"unreviewed clients", func(p *ProDevices) { p.Enabled = true; p.ShadowMode = false }},
		{"shadow notifications", func(p *ProDevices) { p.NotificationsEnabled = true }},
		{"relaxed mobile policy", func(p *ProDevices) { p.MaxMobileDevices = 2 }},
		{"relaxed desktop policy", func(p *ProDevices) { p.MaxDesktopDevices = 2 }},
		{"notification spam", func(p *ProDevices) {
			p.Enabled = true
			p.ShadowMode = false
			p.ClientsReady = true
			p.NotificationsEnabled = true
			p.NotificationCooldown = time.Second
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := base
			tc.edit(&p)
			if len(p.validate()) == 0 {
				t.Fatal("unsafe rollout accepted")
			}
		})
	}
	base.Enabled = true
	base.ShadowMode = false
	base.ClientsReady = true
	base.NotificationsEnabled = true
	if p := base.validate(); len(p) != 0 {
		t.Fatal(p)
	}
}

func TestProDeviceSafeEnvironmentDefaults(t *testing.T) {
	for _, key := range []string{"PRO_DEVICE_LIMIT_ENABLED", "PRO_DEVICE_LIMIT_SHADOW_MODE", "PRO_DEVICE_LIMIT_CLIENTS_READY", "PRO_DEVICE_LIMIT_NOTIFICATIONS_ENABLED", "PRO_MAX_MOBILE_DEVICES", "PRO_MAX_DESKTOP_DEVICES", "PRO_DEVICE_LIMIT_NOTIFICATION_COOLDOWN"} {
		t.Setenv(key, "")
	}
	p := proDevicesFromEnv()
	if p.Enabled || !p.ShadowMode || p.ClientsReady || p.NotificationsEnabled || p.MaxMobileDevices != 1 || p.MaxDesktopDevices != 1 {
		t.Fatalf("unsafe defaults: %+v", p)
	}
}
