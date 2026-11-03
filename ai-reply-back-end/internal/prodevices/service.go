// Package prodevices controls device access to existing Pro entitlements.
package prodevices

import (
	"context"
	"crypto/hmac"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/aireply/ai-reply-back-end/config"
	"github.com/aireply/ai-reply-back-end/internal/auth"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/repository"
	"github.com/aireply/ai-reply-back-end/internal/subscriptions"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

type identityKey struct{}

// WithSessionIdentity accepts only the identity from a verified JWT, never an
// AI request header/body. The keyboard inherits its host application's session.
func WithSessionIdentity(ctx context.Context, identity string) context.Context {
	return context.WithValue(ctx, identityKey{}, identity)
}

type Notifications interface {
	QueueProDeviceLimit(context.Context, string, string, string, string, time.Time, time.Duration) error
}

type Service struct {
	repo           *repository.Store
	subs           *subscriptions.Service
	cfg            config.ProDevices
	log            *slog.Logger
	clock          traits.Clock
	identitySecret string
	notifications  Notifications
}

func New(repo *repository.Store, subs *subscriptions.Service, cfg config.ProDevices, log *slog.Logger) *Service {
	return &Service{repo: repo, subs: subs, cfg: cfg, log: log, clock: traits.SystemClock{}}
}
func (s *Service) WithClock(c traits.Clock) *Service          { s.clock = c; return s }
func (s *Service) WithIdentityKey(key string) *Service        { s.identitySecret = key; return s }
func (s *Service) WithNotifications(n Notifications) *Service { s.notifications = n; return s }

func Category(platform string) string {
	switch platform {
	case "ios", "android", "ipados":
		return domain.ProDeviceMobile
	case "macos", "windows":
		return domain.ProDeviceDesktop
	default:
		return ""
	}
}

func (s *Service) strict() bool { return s.cfg.Enabled && !s.cfg.ShadowMode && s.cfg.ClientsReady }

// Authorize uses only the entitlement already resolved by the subscription
// engine. It runs before provider work, reservations, or cached voice results.
func (s *Service) Authorize(ctx context.Context, user domain.User, deviceID string, ent domain.Entitlement) error {
	if !ent.Unlimited || (!s.cfg.Enabled && !s.cfg.ShadowMode) {
		return nil
	}
	mode := "shadow"
	if s.strict() {
		mode = "enforce"
	}
	device, err := s.repo.Device(ctx, deviceID)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return s.failure(err, mode)
	}
	category := Category(strings.ToLower(device.Platform))
	if err != nil || device.UserID != user.ID || category == "" {
		return s.reject(ctx, user, "", category, "", domain.ErrProDeviceIdentity, mode)
	}
	if device.RevokedAt != nil {
		return s.reject(ctx, user, deviceID, category, device.Platform, domain.ErrProDeviceRevoked, mode)
	}
	// Existing released clients may be observed, but cannot be certified from
	// mutable device metadata alone. Strict mode requires both a signed iid and
	// an explicit link to the existing installation registry.
	identity, _ := ctx.Value(identityKey{}).(string)
	raw, bindErr := s.repo.ProDeviceRawInstallationID(ctx, user.ID, deviceID)
	if bindErr != nil && !errors.Is(bindErr, domain.ErrNotFound) {
		return s.failure(bindErr, mode)
	}
	bound := bindErr == nil && s.identitySecret != "" && identity != "" &&
		hmac.Equal([]byte(identity), []byte(auth.InstallationIdentity(s.identitySecret, raw)))
	if !bound {
		if err := s.reject(ctx, user, deviceID, category, device.Platform, domain.ErrProDeviceIdentity, mode); err != nil {
			return err
		}
	}
	if !s.strict() {
		multiple, err := s.repo.ObserveProDevice(ctx, user.ID, deviceID, category, device.Platform, s.clock.Now())
		if err != nil {
			return s.failure(err, mode)
		}
		if multiple {
			return s.reject(ctx, user, deviceID, category, device.Platform, domain.ErrProDeviceLimit, mode)
		}
		return nil
	}
	previous, err := s.repo.ProDeviceAuthorization(ctx, user.ID, deviceID)
	if err == nil {
		if previous.RevokedAt != nil {
			return s.reject(ctx, user, deviceID, category, device.Platform, domain.ErrProDeviceRevoked, mode)
		}
		if previous.Category != category || previous.Platform != device.Platform {
			return s.reject(ctx, user, deviceID, category, device.Platform, domain.ErrProDeviceIdentity, mode)
		}
		// The read is authoritative for this request. A concurrent revocation
		// takes effect for all subsequent requests; it cannot recall provider work
		// already admitted. Last-seen writes happen at most every 15 minutes.
		if s.clock.Now().Sub(previous.LastSeenAt) >= 15*time.Minute {
			if err := s.repo.TouchProDevice(ctx, user.ID, deviceID, s.clock.Now()); err != nil {
				return s.failure(err, mode)
			}
		}
		return nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return s.failure(err, mode)
	}
	_, created, err := s.repo.ClaimProDevice(ctx, user.ID, deviceID, category, device.Platform, traits.Clamp(device.Model, 64), s.clock.Now())
	if err != nil {
		if errors.Is(err, domain.ErrProDeviceLimit) || errors.Is(err, domain.ErrProDeviceRevoked) || errors.Is(err, domain.ErrProDeviceIdentity) {
			return s.reject(ctx, user, deviceID, category, device.Platform, err, mode)
		}
		return s.failure(err, mode)
	}
	if created {
		s.log.Info("pro device authorized", "user_id", user.ID, "device_category", category, "platform", device.Platform)
	}
	return nil
}

func (s *Service) failure(err error, mode string) error {
	s.log.Error("device authorization transaction failed", "mode", mode, "error", err)
	if s.strict() {
		return err
	}
	return nil // a shadow-storage outage must not break released clients
}

func (s *Service) reject(ctx context.Context, user domain.User, deviceID, category, platform string, reason error, mode string) error {
	kind := "identity_missing"
	if errors.Is(reason, domain.ErrProDeviceLimit) {
		kind = "limit_reached"
	}
	if errors.Is(reason, domain.ErrProDeviceRevoked) {
		kind = "revoked_attempt"
	}
	err := s.repo.RecordProDeviceEvent(ctx, domain.ProDeviceEvent{UserID: user.ID, DeviceID: deviceID, Category: category, Platform: platform, Kind: kind, Mode: mode, Reason: reason.Error(), CreatedAt: s.clock.Now(), NotificationRequested: s.strict() && s.cfg.NotificationsEnabled && errors.Is(reason, domain.ErrProDeviceLimit)})
	if err != nil {
		s.log.Error("pro device audit failed", "user_id", user.ID, "error", err)
	}
	s.log.Info("pro device evaluation", "user_id", user.ID, "device_category", category, "platform", platform, "result", kind, "mode", mode)
	if !s.strict() {
		return nil
	}
	if errors.Is(reason, domain.ErrProDeviceLimit) && s.cfg.NotificationsEnabled && s.notifications != nil {
		// This creates a durable local job only. The existing worker owns all
		// network I/O and retries. A notification failure never changes access.
		if err := s.notifications.QueueProDeviceLimit(ctx, user.ID, deviceID, category, platform, s.clock.Now(), s.cfg.NotificationCooldown); err != nil {
			s.log.Error("pro device notification queue failed", "user_id", user.ID, "error", err)
		}
	}
	return &Error{Cause: reason, Category: category, Locale: user.Locale}
}

func (s *Service) BindInstallation(ctx context.Context, userID, deviceID, installationID string) {
	if !s.cfg.Enabled && !s.cfg.ShadowMode {
		return
	}
	if err := s.repo.BindProDeviceInstallation(ctx, userID, deviceID, installationID); err != nil {
		s.log.Info("pro installation binding unavailable", "user_id", userID, "reason", "missing_or_conflicting_registration")
		return
	}
	// First verified registration claims the free slot; an additional device
	// can still finish registration and use account management.
	if s.subs != nil {
		user, err := s.repo.UserByID(ctx, userID)
		if err != nil {
			return
		}
		ent, err := s.subs.Entitlement(ctx, userID)
		if err != nil {
			s.failure(err, "registration")
			return
		}
		_ = s.Authorize(ctx, user, deviceID, ent)
	}
}
func (s *Service) List(ctx context.Context, userID string) ([]domain.ProDeviceAuthorization, error) {
	return s.repo.ProDeviceAuthorizations(ctx, userID)
}
func (s *Service) Revoke(ctx context.Context, userID, authorizationID, callerDeviceID, reason string) error {
	list, err := s.repo.ProDeviceAuthorizations(ctx, userID)
	if err != nil {
		return err
	}
	for _, a := range list {
		if a.ID == authorizationID && a.RevokedAt != nil {
			return nil
		}
	}
	// A copied legacy did without a signed installation cannot release a slot.
	identity, _ := ctx.Value(identityKey{}).(string)
	raw, err := s.repo.ProDeviceRawInstallationID(ctx, userID, callerDeviceID)
	if err != nil || identity == "" || !hmac.Equal([]byte(identity), []byte(auth.InstallationIdentity(s.identitySecret, raw))) {
		return domain.ErrProDeviceManagementForbidden
	}
	return s.repo.RevokeProDevice(ctx, userID, authorizationID, callerDeviceID, traits.Clamp(reason, 200), false, s.clock.Now())
}

// RevokeRegisteredDevice closes the older management route as a way around
// trusted-device release. Unrelated legacy devices keep their previous API.
func (s *Service) RevokeRegisteredDevice(ctx context.Context, userID, targetDeviceID, callerDeviceID string) (bool, error) {
	a, err := s.repo.ProDeviceAuthorization(ctx, userID, targetDeviceID)
	if errors.Is(err, domain.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	return true, s.Revoke(ctx, userID, a.ID, callerDeviceID, "user_release")
}

// Error carries public safe details; its localized text never includes an ID.
type Error struct {
	Cause            error
	Category, Locale string
}

func (e *Error) Error() string { return e.Cause.Error() }
func (e *Error) Unwrap() error { return e.Cause }
func (e *Error) Details() map[string]any {
	return map[string]any{"device_category": e.Category, "allowed_devices": 1}
}
func (e *Error) PublicMessage() string {
	if errors.Is(e.Cause, domain.ErrProDeviceRevoked) {
		switch e.Locale {
		case "ru":
			return "Доступ Pro на этом устройстве отозван."
		case "kk":
			return "Бұл құрылғының Pro қолжетімділігі қайтарып алынды."
		default:
			return "Pro access for this device has been revoked."
		}
	}
	if errors.Is(e.Cause, domain.ErrProDeviceIdentity) {
		switch e.Locale {
		case "ru":
			return "Для доступа Pro требуется подтверждённая установка приложения. Обратитесь в поддержку."
		case "kk":
			return "Pro үшін расталған қолданба орнатылымы қажет. Қолдау қызметіне хабарласыңыз."
		default:
			return "Pro requires a verified app installation. Contact support."
		}
	}
	switch e.Locale {
	case "ru":
		return "В вашей подписке Pro уже занято место для устройства этой категории."
	case "kk":
		return "Pro жазылымыңызда осы санаттағы құрылғы орны бос емес."
	default:
		return "Your Pro subscription already has an authorized device in this category."
	}
}
