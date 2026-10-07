package api

import (
	"net/http"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/auth"
	"github.com/aireply/ai-reply-back-end/internal/domain"
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
	// The app names its installation in X-Installation-ID: from this moment
	// the account's notifications no longer go to this phone.
	s.detachOnLogout(r, userID)
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
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
