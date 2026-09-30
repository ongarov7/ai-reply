package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/auth"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/phone"
	"github.com/aireply/ai-reply-back-end/internal/reqctx"
	"github.com/aireply/ai-reply-back-end/internal/traits"
	"github.com/aireply/ai-reply-back-end/internal/transport/httpx"
)

type deviceRequest struct {
	DeviceID   string `json:"device_id"`
	Platform   string `json:"platform"`
	AppVersion string `json:"app_version"`
	OSVersion  string `json:"os_version"`
	Model      string `json:"model"`
	Locale     string `json:"locale"`
	Timezone   string `json:"timezone"`
}

func (d deviceRequest) toInfo(r *http.Request) auth.DeviceInfo {
	return auth.DeviceInfo{
		DeviceID:   traits.Clamp(d.DeviceID, 64),
		Platform:   traits.Clamp(d.Platform, 16),
		AppVersion: traits.Clamp(d.AppVersion, 32),
		OSVersion:  traits.Clamp(d.OSVersion, 32),
		Model:      traits.Clamp(d.Model, 64),
		Locale:     traits.Clamp(d.Locale, 8),
		Timezone:   traits.Clamp(d.Timezone, 64),
		UserAgent:  traits.Clamp(r.UserAgent(), 200),
	}
}

type requestOTPRequest struct {
	Identifier string `json:"identifier"`
	Locale     string `json:"locale"`
}

type requestOTPResponse struct {
	Kind      string `json:"kind"`
	Masked    string `json:"masked_identifier"`
	Channel   string `json:"channel"`
	ExpiresIn int    `json:"expires_in"`
	DemoMode  bool   `json:"demo_mode"`
}

// handleRequestOTP — кодты сұрау.
func (s *Server) handleRequestOTP(w http.ResponseWriter, r *http.Request) {
	var body requestOTPRequest
	if err := httpx.Decode(w, r, s.cfg.Limits.RequestBodyBytes, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	challenge, err := s.auth.RequestOTP(r.Context(), body.Identifier, body.Locale)
	kind, subject := identitySubject(body.Identifier)
	s.authEvent(r, domain.AuthOTPRequested, kind, "", kind, subject, nil, err)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, requestOTPResponse{
		Kind:      challenge.Kind,
		Masked:    challenge.Masked,
		Channel:   challenge.Channel,
		ExpiresIn: challenge.ExpiresIn,
		DemoMode:  challenge.DemoMode,
	})
}

type verifyOTPRequest struct {
	Identifier string        `json:"identifier"`
	Code       string        `json:"code"`
	Device     deviceRequest `json:"device"`
}

type sessionResponse struct {
	AccessToken      string           `json:"access_token"`
	RefreshToken     string           `json:"refresh_token"`
	TokenType        string           `json:"token_type"`
	ExpiresIn        int              `json:"expires_in"`
	RefreshExpiresAt string           `json:"refresh_expires_at"`
	DeviceID         string           `json:"device_id"`
	IsNewUser        bool             `json:"is_new_user"`
	User             userDTO          `json:"user"`
	Profile          profileDTO       `json:"profile"`
	Subscription     subscriptionDTO  `json:"subscription"`
	Usage            usageDTO         `json:"usage"`
	LegalConsent     *legalConsentDTO `json:"legal_consent,omitempty"`
}

// handleVerifyOTP — кодты тексеріп, сессия ашу.
func (s *Server) handleVerifyOTP(w http.ResponseWriter, r *http.Request) {
	var body verifyOTPRequest
	if err := httpx.Decode(w, r, s.cfg.Limits.RequestBodyBytes, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	session, err := s.auth.VerifyOTP(r.Context(), body.Identifier, body.Code, body.Device.toInfo(r))
	kind, subject := identitySubject(body.Identifier)
	otpEvent := domain.AuthOTPVerified
	if err != nil {
		otpEvent = domain.AuthOTPFailed
	}
	s.authEvent(r, otpEvent, kind, session.User.ID, kind, subject, &body.Device, err)
	s.loginOutcome(r, kind, session, kind, subject, &body.Device, err)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	s.writeSession(w, r, session)
}

type refreshRequest struct {
	RefreshToken string        `json:"refresh_token"`
	Device       deviceRequest `json:"device"`
}

// handleRefresh — токенді жаңарту (ротациямен).
func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	var body refreshRequest
	if err := httpx.Decode(w, r, s.cfg.Limits.RequestBodyBytes, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	session, err := s.auth.Refresh(r.Context(), body.RefreshToken, body.Device.toInfo(r))
	if err != nil {
		// Successful refreshes happen every few minutes for every active app and
		// are visible as sessions; only the refusals belong in the security log.
		s.authEvent(r, domain.AuthSessionExpired, "refresh", "", "", "", &body.Device, err)
		httpx.Fail(w, err)
		return
	}
	s.writeSession(w, r, session)
}

type logoutRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// handleLogout — сессияны жабу.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	var body logoutRequest
	if err := httpx.Decode(w, r, s.cfg.Limits.RequestBodyBytes, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	userID, err := s.auth.LogoutSession(r.Context(), body.RefreshToken)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	if userID != "" {
		s.authEvent(r, domain.AuthLogout, "", userID, "", "", nil, nil)
	}
	// The app names its installation in X-Installation-ID: from this moment the
	// account's notifications no longer go to this phone. An already expired
	// refresh token still signs the phone out, exactly like the anonymous
	// registration the app sends next.
	if installationID := reqctx.From(r.Context()).InstallationID; installationID != "" && s.installations != nil {
		var detachErr error
		if userID != "" {
			_, detachErr = s.installations.Detach(r.Context(), installationID, userID)
		} else {
			_, detachErr = s.installations.DetachAny(r.Context(), installationID)
		}
		if detachErr != nil {
			s.log.Warn("installation detach on logout failed", "error", detachErr.Error())
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// identitySubject — ескі ағындағы идентификатор: пошта не телефон (E.164, әкімші іздеуімен бірдей).
func identitySubject(identifier string) (kind, value string) {
	if strings.Contains(identifier, "@") {
		return "email", strings.ToLower(strings.TrimSpace(identifier))
	}
	if number, err := phone.Parse(identifier); err == nil {
		return "phone", number.E164
	}
	return "phone", strings.TrimSpace(identifier)
}

// writeSession — сессия жауабын жинау (профиль, тариф, квота бірден келеді).
func (s *Server) writeSession(w http.ResponseWriter, r *http.Request, session auth.Session) {
	ctx := r.Context()
	profile, err := s.users.Profile(ctx, session.User.ID)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	entitlement, err := s.subs.Entitlement(ctx, session.User.ID)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	consent, err := s.currentLegalConsent(r, session.User.ID)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, sessionResponse{
		AccessToken:      session.AccessToken,
		RefreshToken:     session.RefreshToken,
		TokenType:        "Bearer",
		ExpiresIn:        session.AccessExpiresIn,
		RefreshExpiresAt: session.RefreshExpiresAt.Format(time.RFC3339),
		DeviceID:         session.DeviceID,
		IsNewUser:        session.IsNewUser,
		User:             s.userDTO(ctx, session.User, profile),
		Profile:          toProfileDTO(profile),
		Subscription:     s.subscriptionDTO(entitlement),
		Usage:            toUsageDTO(entitlement, s.cfg.App.Timezone),
		LegalConsent:     consent,
	})
}

func (s *Server) subscriptionDTO(e domain.Entitlement) subscriptionDTO {
	dto := subscriptionDTO{Status: domain.SubActive, Plan: toPlanDTO(e.Plan)}
	if e.Subscription != nil {
		dto.ID = e.Subscription.ID
		dto.Status = e.Subscription.Status
		dto.Source = e.Subscription.Source
		dto.StartedAt = e.Subscription.StartedAt.Format(time.RFC3339)
		if e.Subscription.ExpiresAt != nil {
			dto.ExpiresAt = e.Subscription.ExpiresAt.Format(time.RFC3339)
		}
	}
	return dto
}
