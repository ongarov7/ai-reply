package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/installations"
	"github.com/aireply/ai-reply-back-end/internal/middleware"
	"github.com/aireply/ai-reply-back-end/internal/notifications"
	"github.com/aireply/ai-reply-back-end/internal/transport/httpx"
)

// Орнатулар, push баптаулары және хабарламаның ашылғаны.

// installationIDHeader — шығу (logout) сұранысында қосымша өз орнатуын атайды.
const installationIDHeader = "X-Installation-ID"

// sharedAddressFactor — бір IP мекенжайы қанша орнатудың үлесін жібере алады
// (оператордың NAT-ы, кеңсе Wi-Fi-ы).
const sharedAddressFactor = 20

// installationHeader — X-Installation-ID тақырыбы, пішімі дұрыс болса.
func installationHeader(r *http.Request) string {
	id := strings.TrimSpace(r.Header.Get(installationIDHeader))
	if installations.ValidInstallationID(id) {
		return id
	}
	return ""
}

// installationLimit — орнату сұраныстарының IP бойынша жұмсақ шегі.
//
// The real budget is per installation (allowInstallation): behind carrier
// NAT many phones share one address, and a per-IP budget alone would
// throttle unrelated people together. This looser per-IP budget on top still
// stops a client that invents a new id per request.
func (s *Server) installationLimit(next http.Handler) http.Handler {
	limit := s.cfg.Limits.GenericPerMinute * sharedAddressFactor
	return middleware.RateLimit(s.limiter, "installation_ip", limit, time.Minute,
		func(r *http.Request) string { return httpx.ClientIP(r, s.cfg.App.TrustProxy) })(next)
}

// allowInstallation — бір орнатудың минуттық шегі; толса 429 жазады.
//
// The installation is known only once the handler has read it: from the
// body for registration and "opened", from the path for detach. Apps send no
// installation header on these requests. A malformed id is not counted: the
// handler refuses it with 400.
func (s *Server) allowInstallation(w http.ResponseWriter, installationID string) bool {
	if !installations.ValidInstallationID(installationID) {
		return true
	}
	ok, retry := s.limiter.Allow("installation:"+installationID, s.cfg.Limits.GenericPerMinute, time.Minute)
	if ok {
		return true
	}
	seconds := int(retry.Seconds())
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	httpx.Error(w, http.StatusTooManyRequests, httpx.CodeRateLimited, "Too many requests. Try again shortly.",
		map[string]any{"retry_after_seconds": seconds})
	return false
}

type pushTokenRequest struct {
	Provider string `json:"provider"`
	Token    string `json:"token"`
}

type installationRequest struct {
	InstallationID         string            `json:"installation_id"`
	Platform               string            `json:"platform"`
	AppVersion             string            `json:"app_version"`
	AppBuild               string            `json:"app_build"`
	OSName                 string            `json:"os_name"`
	OSVersion              string            `json:"os_version"`
	DeviceModel            string            `json:"device_model"`
	Manufacturer           string            `json:"manufacturer"`
	Locale                 string            `json:"locale"`
	Timezone               string            `json:"timezone"`
	NotificationPermission string            `json:"notification_permission"`
	NotificationsEnabled   *bool             `json:"notifications_enabled"`
	Push                   *pushTokenRequest `json:"push"`
}

type installationResponse struct {
	InstallationID       string          `json:"installation_id"`
	Attached             bool            `json:"attached"`
	PushStatus           string          `json:"push_status"`
	PushAvailable        bool            `json:"push_available"`
	NotificationsEnabled bool            `json:"notifications_enabled"`
	Preferences          map[string]bool `json:"preferences,omitempty"`
}

// handleRegisterInstallation — орнатуды тіркеу/жаңарту: метадерек, push токені, тіркелгі.
//
// With a valid access token the installation is attached to that account;
// without one it is anonymous, which also detaches it from whoever used the
// phone before. The account never comes from the body.
func (s *Server) handleRegisterInstallation(w http.ResponseWriter, r *http.Request) {
	var body installationRequest
	if err := httpx.Decode(w, r, s.cfg.Limits.RequestBodyBytes, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	if !s.allowInstallation(w, strings.TrimSpace(body.InstallationID)) {
		return
	}
	userID := ""
	if user, ok := UserFrom(r.Context()); ok {
		userID = user.ID
	}
	reg := installations.Registration{
		InstallationID: body.InstallationID, Platform: body.Platform, AppVersion: body.AppVersion,
		AppBuild: body.AppBuild, OSName: body.OSName, OSVersion: body.OSVersion, DeviceModel: body.DeviceModel,
		Manufacturer: body.Manufacturer, Locale: body.Locale, Timezone: body.Timezone,
		Permission: body.NotificationPermission, NotificationsEnabled: body.NotificationsEnabled,
	}
	if body.Push != nil {
		reg.Push = &installations.PushToken{Provider: body.Push.Provider, Token: body.Push.Token}
	}
	inst, err := s.installations.Register(r.Context(), reg, userID)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	res := installationResponse{
		InstallationID: inst.InstallationID, Attached: inst.UserID != "", PushStatus: inst.PushStatus,
		PushAvailable: s.notifications.Ready(), NotificationsEnabled: inst.NotificationsEnabled,
	}
	if userID != "" {
		prefs, err := s.notifications.Preferences(r.Context(), userID)
		if err != nil {
			httpx.Fail(w, err)
			return
		}
		res.Preferences = prefs
	}
	httpx.JSON(w, http.StatusOK, res)
}

// handleDetachInstallation — шығу кезінде: құрылғы бұдан былай тіркелгінің хабарламаларын алмайды.
func (s *Server) handleDetachInstallation(w http.ResponseWriter, r *http.Request) {
	installationID := r.PathValue("installation_id")
	if !installations.ValidInstallationID(installationID) {
		httpx.Fail(w, domain.InvalidField("installation_id", "format"))
		return
	}
	if !s.allowInstallation(w, installationID) {
		return
	}
	var detached bool
	var err error
	if user, ok := UserFrom(r.Context()); ok {
		detached, err = s.installations.Detach(r.Context(), installationID, user.ID)
	} else {
		// Signed out already (the tokens are gone): an anonymous
		// registration is what detaches, so do exactly that.
		detached, err = s.installations.DetachAny(r.Context(), installationID)
	}
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true, "detached": detached})
}

// detachOnLogout — шыққан қосымшаның орнатуы тіркелгіден ажырайды (X-Installation-ID).
//
// An already expired refresh token still signs the phone out, exactly like
// the anonymous registration the app sends next. A failure is only logged:
// the logout itself has succeeded.
func (s *Server) detachOnLogout(r *http.Request, userID string) {
	installationID := installationHeader(r)
	if installationID == "" || s.installations == nil {
		return
	}
	var err error
	if userID != "" {
		_, err = s.installations.Detach(r.Context(), installationID, userID)
	} else {
		_, err = s.installations.DetachAny(r.Context(), installationID)
	}
	if err != nil {
		s.log.Warn("installation detach on logout failed", "error", err.Error())
	}
}

type preferencesRequest struct {
	Preferences map[string]bool `json:"preferences"`
}

// handleNotificationPreferences — санаттар бойынша баптау.
func (s *Server) handleNotificationPreferences(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	prefs, err := s.notifications.Preferences(r.Context(), user.ID)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"preferences": prefs, "optional": notifications.OptionalCategories()})
}

// handleUpdateNotificationPreferences — санатты қосу/өшіру (қауіпсіздік хабарлары өшпейді).
func (s *Server) handleUpdateNotificationPreferences(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	var body preferencesRequest
	if err := httpx.Decode(w, r, 4096, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	if len(body.Preferences) == 0 {
		httpx.Fail(w, domain.InvalidField("preferences", "required"))
		return
	}
	prefs, err := s.notifications.SetPreferences(r.Context(), user.ID, body.Preferences)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"preferences": prefs, "optional": notifications.OptionalCategories()})
}

type openedRequest struct {
	InstallationID string `json:"installation_id"`
	DeliveryID     string `json:"delivery_id"`
}

// handleNotificationOpened — адам push-ты басып ашты (did бар хабарлама).
//
// Recorded only when the delivery went to that very installation, so one
// app cannot mark another device's notifications as opened.
func (s *Server) handleNotificationOpened(w http.ResponseWriter, r *http.Request) {
	var body openedRequest
	if err := httpx.Decode(w, r, 1024, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	installationID := strings.TrimSpace(body.InstallationID)
	if !s.allowInstallation(w, installationID) {
		return
	}
	recorded, err := s.notifications.MarkOpened(r.Context(), installationID, strings.TrimSpace(body.DeliveryID))
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true, "recorded": recorded})
}
