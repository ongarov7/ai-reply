package api

import (
	"net/http"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/installations"
	"github.com/aireply/ai-reply-back-end/internal/transport/httpx"
)

// diagnosticsRequest — PUT /installations/{id}/diagnostics денесі (қатаң: белгісіз өріс — 400).
type diagnosticsRequest struct {
	KeyboardSetupStatus    string  `json:"keyboard_setup_status"`
	KeyboardSelectedStatus string  `json:"keyboard_selected_status"`
	FullAccessStatus       string  `json:"full_access_status"`
	ClipboardStatus        string  `json:"clipboard_status"`
	OnboardingStatus       string  `json:"onboarding_status"`
	StatusSource           string  `json:"status_source"`
	LastSuccessfulPasteAt  *string `json:"last_successful_paste_at"`
	LastKeyboardCheckAt    *string `json:"last_keyboard_check_at"`
}

type diagnosticsResponse struct {
	KeyboardSetupStatus             string  `json:"keyboard_setup_status"`
	KeyboardSelectedStatus          string  `json:"keyboard_selected_status"`
	FullAccessStatus                string  `json:"full_access_status"`
	ClipboardStatus                 string  `json:"clipboard_status"`
	OnboardingStatus                string  `json:"onboarding_status"`
	StatusSource                    string  `json:"status_source"`
	LastSuccessfulPasteAt           *string `json:"last_successful_paste_at"`
	LastKeyboardCheckAt             *string `json:"last_keyboard_check_at"`
	NotificationAuthorizationStatus string  `json:"notification_authorization_status"`
	UpdatedAt                       string  `json:"updated_at"`
}

func rfc3339(t *time.Time) *string {
	if t == nil {
		return nil
	}
	v := t.UTC().Format(time.RFC3339)
	return &v
}

// handleInstallationDiagnostics — қосымша байқаған пернетақта, толық қатынас, буфер және онбординг күйі.
//
// Enums and timestamps only, never clipboard content. Optional auth like the
// other installation endpoints: the installation must exist and must not be
// attached to another account. Apps send it only when /api/v1/config
// features.permission_diagnostics is true.
func (s *Server) handleInstallationDiagnostics(w http.ResponseWriter, r *http.Request) {
	installationID := r.PathValue("installation_id")
	if !installations.ValidInstallationID(installationID) {
		httpx.Fail(w, domain.InvalidField("installation_id", "format"))
		return
	}
	if !s.allowInstallation(w, installationID) {
		return
	}
	var body diagnosticsRequest
	if err := httpx.Decode(w, r, 2048, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	userID := ""
	if user, ok := UserFrom(r.Context()); ok {
		userID = user.ID
	}
	res, err := s.installations.ReportDiagnostics(r.Context(), installationID, userID, installations.DiagnosticsReport{
		KeyboardSetupStatus: body.KeyboardSetupStatus, KeyboardSelectedStatus: body.KeyboardSelectedStatus,
		FullAccessStatus: body.FullAccessStatus, ClipboardStatus: body.ClipboardStatus,
		OnboardingStatus: body.OnboardingStatus, StatusSource: body.StatusSource,
		LastSuccessfulPasteAt: body.LastSuccessfulPasteAt, LastKeyboardCheckAt: body.LastKeyboardCheckAt,
	})
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	d := res.Diagnostics
	httpx.JSON(w, http.StatusOK, diagnosticsResponse{
		KeyboardSetupStatus: d.KeyboardSetupStatus, KeyboardSelectedStatus: d.KeyboardSelectedStatus,
		FullAccessStatus: d.FullAccessStatus, ClipboardStatus: d.ClipboardStatus,
		OnboardingStatus: d.OnboardingStatus, StatusSource: d.StatusSource,
		LastSuccessfulPasteAt: rfc3339(d.LastSuccessfulPasteAt), LastKeyboardCheckAt: rfc3339(d.LastKeyboardCheckAt),
		NotificationAuthorizationStatus: res.NotificationAuthorization,
		UpdatedAt:                       d.UpdatedAt.UTC().Format(time.RFC3339),
	})
}
