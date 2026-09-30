package api

import (
	"errors"
	"net/http"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/installations"
	"github.com/aireply/ai-reply-back-end/internal/reqctx"
	"github.com/aireply/ai-reply-back-end/internal/telemetry"
	"github.com/aireply/ai-reply-back-end/internal/transport/httpx"
)

// Орнатулар, push баптаулары және қосымша оқиғалары.

type pushTokenRequest struct {
	Provider    string `json:"provider"`
	Token       string `json:"token"`
	Environment string `json:"environment"`
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
		reg.Push = &installations.PushToken{
			Provider: body.Push.Provider, Token: body.Push.Token, Environment: body.Push.Environment,
		}
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
		if prefs, err := s.notifications.Preferences(r.Context(), userID); err == nil {
			res.Preferences = prefs
		}
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

type preferencesRequest struct {
	Preferences map[string]bool `json:"preferences"`
}

func optionalCategories() []string {
	out := []string{}
	for _, c := range domain.NotificationCategories {
		if domain.CategoryOptional(c) {
			out = append(out, c)
		}
	}
	return out
}

// handleNotificationPreferences — санаттар бойынша баптау.
func (s *Server) handleNotificationPreferences(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	prefs, err := s.notifications.Preferences(r.Context(), user.ID)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"preferences": prefs, "optional": optionalCategories()})
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
	httpx.JSON(w, http.StatusOK, map[string]any{"preferences": prefs, "optional": optionalCategories()})
}

type eventRequest struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	OccurredAt string         `json:"occurred_at"`
	Properties map[string]any `json:"properties"`
}

type eventsRequest struct {
	InstallationID string         `json:"installation_id"`
	SessionID      string         `json:"session_id"`
	Events         []eventRequest `json:"events"`
}

// maxEventsBody — бір топтың ең үлкен денесі (50 оқиға, әрқайсысы шектеулі қасиеттермен).
const maxEventsBody = 64 * 1024

// handleEvents — қосымша оқиғаларын қабылдайды (рұқсат етілген тізім, шектеулі қасиеттер).
//
// 202 means "taken": the events are written in the background, and a full
// queue drops them rather than slowing the app down. Unknown events and
// properties are refused one by one, never stored.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	var body eventsRequest
	if err := httpx.Decode(w, r, maxEventsBody, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	client := reqctx.From(r.Context())
	batch := telemetry.Batch{
		InstallationID: body.InstallationID, SessionID: body.SessionID, Platform: client.Platform,
		AppVersion: client.AppVersion, AppBuild: client.AppBuild, OSVersion: client.OSVersion,
		RequestID: client.RequestID,
	}
	if user, ok := UserFrom(r.Context()); ok {
		batch.UserID = user.ID
	}
	if installations.ValidInstallationID(body.InstallationID) {
		if inst, err := s.installations.Lookup(r.Context(), body.InstallationID); err == nil {
			batch.DeviceModel = inst.DeviceModel
			if batch.Platform == "" {
				batch.Platform = inst.Platform
			}
		} else if !errors.Is(err, domain.ErrNotFound) {
			s.log.Warn("installation lookup failed", "error", err.Error())
		}
	}
	for _, e := range body.Events {
		batch.Events = append(batch.Events, telemetry.IncomingEvent{
			ID: e.ID, Name: e.Name, OccurredAt: e.OccurredAt, Properties: e.Properties,
		})
	}
	result, err := s.telemetry.Ingest(r.Context(), batch)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, result)
}
