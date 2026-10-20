package adminapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/notifications"
	"github.com/aireply/ai-reply-back-end/internal/repository"
	"github.com/aireply/ai-reply-back-end/internal/traits"
	"github.com/aireply/ai-reply-back-end/internal/transport/httpx"
)

// Пернетақта мен рұқсаттар диагностикасы, еске салулар және олардың баптаулары.

func (s *Server) registerDiagnostics(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/admin/diagnostics", s.guard(s.handleDiagnostics))
	mux.Handle("GET /api/v1/admin/users/{id}/diagnostics", s.guard(s.handleUserDiagnostics))
	mux.Handle("POST /api/v1/admin/notifications/reminders", s.guard(s.handleReminderSend))
	mux.Handle("GET /api/v1/admin/settings/reminders", s.guard(s.handleReminderSettings))
	mux.Handle("POST /api/v1/admin/settings/reminders", s.guard(s.handleSaveReminderSettings))
}

// diagnosticsDTO — бір орнатудың күйлері (nil — орнату әлі ештеңе хабарламаған).
func (s *Server) diagnosticsDTO(i domain.Installation, d *domain.InstallationDiagnostics) map[string]any {
	loc := s.cfg.App.Location()
	out := map[string]any{
		"notification_authorization_status": domain.NotificationAuthorizationFrom(i.PushPermission, i.NotificationsEnabled),
		"reported":                          d != nil,
	}
	if d == nil {
		return out
	}
	for key, value := range map[string]string{
		"keyboard_setup_status": d.KeyboardSetupStatus, "keyboard_selected_status": d.KeyboardSelectedStatus,
		"full_access_status": d.FullAccessStatus, "clipboard_status": d.ClipboardStatus,
		"onboarding_status": d.OnboardingStatus, "status_source": d.StatusSource,
		"last_successful_paste_at": optionalTime(d.LastSuccessfulPasteAt, loc),
		"last_keyboard_check_at":   optionalTime(d.LastKeyboardCheckAt, loc),
		"keyboard_verified_at":     optionalTime(d.KeyboardVerifiedAt, loc),
		"full_access_verified_at":  optionalTime(d.FullAccessVerifiedAt, loc),
		"clipboard_denied_at":      optionalTime(d.ClipboardDeniedAt, loc),
		"onboarding_completed_at":  optionalTime(d.OnboardingCompletedAt, loc),
		"reported_at":              d.ReportedAt.In(loc).Format("2006-01-02 15:04"),
	} {
		out[key] = value
	}
	// Older than the reminder evidence window: shown, but no longer evidence.
	out["stale"] = time.Since(d.ReportedAt) > domain.DiagnosticsFreshness
	return out
}

// handleDiagnostics — «Пернетақта мен рұқсаттар» тізімі: сүзгі чиптері, платформа, іздеу, беттеу.
func (s *Server) handleDiagnostics(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	page, limit := pageParams(r, 50)
	filter := strings.TrimSpace(query.Get("filter"))
	if filter == "all" {
		filter = ""
	}
	if filter != "" && !traits.OneOf(filter, domain.DiagnosticFilters...) {
		httpx.Fail(w, domain.InvalidField("filter", "unknown"))
		return
	}
	platform := query.Get("platform")
	if platform != "" && platform != domain.PlatformAndroid && platform != domain.PlatformIOS {
		httpx.Fail(w, domain.InvalidField("platform", "unknown"))
		return
	}
	rows, total, counts, err := s.admin.Diagnostics(r.Context(), repository.DiagnosticsFilter{
		Filter: filter, Platform: platform, Search: traits.Clamp(query.Get("q"), 64),
		Page: traits.NewPage(limit, (page-1)*limit),
	})
	if err != nil {
		s.fail(w, err)
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		dto := s.installationDTO(row.Installation)
		dto["user"], dto["attached"] = "", row.Installation.UserID != ""
		if row.Installation.UserID != "" {
			dto["user"] = maskIdentifier(domain.User{ID: row.Installation.UserID, Email: row.UserEmail, Phone: row.UserPhone})
		}
		dto["diagnostics"] = s.diagnosticsDTO(row.Installation, row.Diagnostics)
		out = append(out, dto)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"rows": out, "total": total, "page": page, "limit": limit, "counts": counts,
		"filters": domain.DiagnosticFilters, "statuses": domain.DiagnosticStatuses,
	})
}

// handleUserDiagnostics — қолданушы бетіндегі карточка: әр құрылғының күйі және еске салу журналы.
func (s *Server) handleUserDiagnostics(w http.ResponseWriter, r *http.Request) {
	rows, states, err := s.admin.UserDiagnostics(r.Context(), r.PathValue("id"))
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	loc := s.cfg.App.Location()
	devices := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		dto := s.installationDTO(row.Installation)
		dto["diagnostics"] = s.diagnosticsDTO(row.Installation, row.Diagnostics)
		devices = append(devices, dto)
	}
	reminders := make([]map[string]any, 0, len(states))
	for _, st := range states {
		reminders = append(reminders, map[string]any{
			"type": st.Type, "send_count": st.SendCount, "last_source": st.LastSource,
			"last_sent_at": optionalTime(st.LastSentAt, loc), "resolved_at": optionalTime(st.ResolvedAt, loc),
		})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"devices": devices, "reminders": reminders})
}

type reminderRequest struct {
	Type     string                `json:"type"`
	Audience domain.AudienceFilter `json:"audience"`
}

// handleReminderSend — «Еске салу жіберу»: сүзгіленген аудиторияға еске салу түріндегі науқан.
//
// Idempotency-Key is required, like any campaign: a double click or a retry
// returns the first campaign. The audience must carry a diagnostics filter
// or name people. History and errors are the campaign's deliveries.
func (s *Server) handleReminderSend(w http.ResponseWriter, r *http.Request) {
	var body reminderRequest
	if err := httpx.Decode(w, r, campaignBodyBytes, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	adminUser := adminFrom(r.Context())
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	existing, err := s.notify.CampaignByIdempotencyKey(r.Context(), key)
	if (err != nil || existing.Status == domain.CampaignDraft) && !s.allowSend(w, adminUser) {
		return
	}
	result, err := s.notify.CreateReminderCampaign(r.Context(), adminUser.ID, body.Type, body.Audience, key)
	campaign := result.Campaign
	if result.Created {
		s.admin.Audit(r.Context(), adminUser, s.ip(r), "campaign.create", "notification_campaign", campaign.ID,
			map[string]any{"category": campaign.Category, "audience": auditAudience(campaign.Audience),
				"link": campaign.Link, "reminder_type": campaign.NotificationType, "send": true})
	}
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	if result.Queued {
		s.admin.Audit(r.Context(), adminUser, s.ip(r), "campaign.send", "notification_campaign", campaign.ID,
			map[string]any{"category": campaign.Category, "status": campaign.Status, "reminder_type": campaign.NotificationType})
	}
	view, err := s.notify.Campaign(r.Context(), campaign.ID)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	dto := s.campaignDTO(view)
	dto["created"], dto["notification_type"] = result.Created, campaign.NotificationType
	httpx.JSON(w, status, dto)
}

type reminderSettingsDTO struct {
	Current  notifications.ReminderSettings         `json:"current"`
	Defaults notifications.ReminderSettings         `json:"defaults"`
	Ranges   map[string]notifications.ReminderRange `json:"ranges"`
	Types    []string                               `json:"types"`
}

func (s *Server) reminderSettingsPayload(r *http.Request) reminderSettingsDTO {
	return reminderSettingsDTO{
		Current: s.notify.ReminderSettings(r.Context()), Defaults: notifications.DefaultReminderSettings(),
		Ranges: map[string]notifications.ReminderRange{
			"cooldown_hours": notifications.ReminderCooldownRange, "max_sends": notifications.ReminderMaxSendsRange,
			"min_account_age_hours": notifications.ReminderMinAgeRange, "hour": notifications.ReminderHourRange,
		},
		Types: domain.ReminderTypes,
	}
}

func (s *Server) handleReminderSettings(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, s.reminderSettingsPayload(r))
}

// handleSaveReminderSettings — еске салу баптаулары (аудитпен, ескі/жаңа мәндерімен).
func (s *Server) handleSaveReminderSettings(w http.ResponseWriter, r *http.Request) {
	var body notifications.ReminderSettings
	if err := httpx.Decode(w, r, 2048, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	before := s.notify.ReminderSettings(r.Context())
	saved, err := s.notify.UpdateReminderSettings(r.Context(), body)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	s.admin.Audit(r.Context(), adminFrom(r.Context()), s.ip(r), "settings.reminders.update", "system_settings",
		"reminders", map[string]any{"before": before, "after": saved})
	httpx.JSON(w, http.StatusOK, s.reminderSettingsPayload(r))
}
