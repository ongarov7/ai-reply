package domain

import (
	"errors"
	"time"
)

const (
	ProDeviceMobile  = "mobile"
	ProDeviceDesktop = "desktop"
)

var (
	ErrProDeviceLimit               = errors.New("pro device limit reached")
	ErrProDeviceRevoked             = errors.New("pro device authorization revoked")
	ErrProDeviceIdentity            = errors.New("pro device identity unavailable or inconsistent")
	ErrProDeviceManagementForbidden = errors.New("pro device management requires an authorized device")
)

// ProDeviceAuthorization refers to the existing device row, never a second
// client identity. The opaque authorization ID is safe for management APIs.
// DeviceID stays server-side because existing device IDs originate on clients.
type ProDeviceAuthorization struct {
	ID                string     `json:"id"`
	UserID            string     `json:"user_id"`
	DeviceID          string     `json:"-"`
	Category          string     `json:"device_category"`
	Platform          string     `json:"platform"`
	DisplayName       string     `json:"display_name"`
	FirstAuthorizedAt time.Time  `json:"first_authorized_at"`
	LastSeenAt        time.Time  `json:"last_seen_at"`
	RevokedAt         *time.Time `json:"revoked_at,omitempty"`
	RevokedReason     string     `json:"revoked_reason,omitempty"`
}

// ProDeviceEvent contains security metadata only, never request contents,
// tokens, hardware identifiers or the raw installation identifier.
type ProDeviceEvent struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	DeviceID  string    `json:"-"`
	Category  string    `json:"device_category"`
	Platform  string    `json:"platform"`
	Kind      string    `json:"kind"`
	Mode      string    `json:"mode"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
	// NotificationRequested is a durable delivery intent, set only for an
	// actual enforced denial while security notifications are enabled.
	NotificationRequested bool `json:"-"`
}
