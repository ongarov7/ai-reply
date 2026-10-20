package installations

import (
	"context"
	"strings"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/repository"
)

// futureSkew — қосымша сағатының алда тұруына рұқсат.
const futureSkew = 5 * time.Minute

// DiagnosticsReport — қосымша жіберген диагностика: тек enum мен уақыт (буфер мазмұны ешқашан).
type DiagnosticsReport struct {
	KeyboardSetupStatus    string
	KeyboardSelectedStatus string
	FullAccessStatus       string
	ClipboardStatus        string
	OnboardingStatus       string
	StatusSource           string
	LastSuccessfulPasteAt  *string // RFC 3339, nil — not known
	LastKeyboardCheckAt    *string
}

// DiagnosticsResult — сақталған күй және орнатудан шығарылған хабарлама рұқсаты.
type DiagnosticsResult struct {
	Diagnostics               domain.InstallationDiagnostics
	NotificationAuthorization string
}

// ReportDiagnostics — орнатудың диагностикасын тексеріп, сақтайды.
//
// Every status must be a known value (an empty one is "unknown"), the source
// is required, and the timestamps are clamped to [the installation's first
// sighting, now + 5 min]. The installation must exist and must not be
// attached to another account: a phone reports only for itself.
func (s *Service) ReportDiagnostics(ctx context.Context, installationID, userID string, r DiagnosticsReport) (DiagnosticsResult, error) {
	if !ValidInstallationID(installationID) {
		return DiagnosticsResult{}, domain.InvalidField("installation_id", "format")
	}
	statuses := []struct {
		field string
		value *string
		all   []string
	}{
		{"keyboard_setup_status", &r.KeyboardSetupStatus, domain.DiagnosticStatuses},
		{"keyboard_selected_status", &r.KeyboardSelectedStatus, domain.DiagnosticStatuses},
		{"full_access_status", &r.FullAccessStatus, domain.DiagnosticStatuses},
		{"clipboard_status", &r.ClipboardStatus, domain.DiagnosticStatuses},
		{"onboarding_status", &r.OnboardingStatus, domain.OnboardingStatuses},
	}
	for _, st := range statuses {
		v := strings.TrimSpace(*st.value)
		if v == "" {
			v = domain.DiagUnknown
		}
		if !oneOf(v, st.all) {
			return DiagnosticsResult{}, domain.InvalidField(st.field, "unknown")
		}
		*st.value = v
	}
	source := strings.TrimSpace(r.StatusSource)
	if !oneOf(source, domain.DiagnosticSources) {
		return DiagnosticsResult{}, domain.InvalidField("status_source", "app_check, keyboard_check or user_report")
	}

	inst, err := s.repo.InstallationByClientID(ctx, installationID)
	if err != nil {
		return DiagnosticsResult{}, err
	}
	if inst.UserID != userID {
		return DiagnosticsResult{}, repository.ErrInstallationOwner
	}
	now := s.clock.Now()
	paste, err := clampTime("last_successful_paste_at", r.LastSuccessfulPasteAt, inst.FirstSeenAt, now)
	if err != nil {
		return DiagnosticsResult{}, err
	}
	check, err := clampTime("last_keyboard_check_at", r.LastKeyboardCheckAt, inst.FirstSeenAt, now)
	if err != nil {
		return DiagnosticsResult{}, err
	}
	stored, err := s.repo.UpsertInstallationDiagnostics(ctx, repository.DiagnosticsReport{
		InstallationID: installationID, UserID: userID,
		KeyboardSetupStatus: r.KeyboardSetupStatus, KeyboardSelectedStatus: r.KeyboardSelectedStatus,
		FullAccessStatus: r.FullAccessStatus, ClipboardStatus: r.ClipboardStatus,
		OnboardingStatus: r.OnboardingStatus, StatusSource: source,
		LastSuccessfulPasteAt: paste, LastKeyboardCheckAt: check, Now: now,
	})
	if err != nil {
		return DiagnosticsResult{}, err
	}
	return DiagnosticsResult{
		Diagnostics:               stored,
		NotificationAuthorization: domain.NotificationAuthorizationFrom(inst.PushPermission, inst.NotificationsEnabled),
	}, nil
}

// clampTime — RFC 3339 уақыты [from, now+5 мин] аралығына қысылады; nil — белгісіз.
func clampTime(field string, raw *string, from, now time.Time) (*time.Time, error) {
	if raw == nil {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(*raw))
	if err != nil {
		return nil, domain.InvalidField(field, "RFC 3339 time")
	}
	t = t.UTC()
	if t.Before(from) {
		t = from
	}
	if latest := now.Add(futureSkew); t.After(latest) {
		t = latest
	}
	return &t, nil
}

func oneOf(v string, all []string) bool {
	for _, a := range all {
		if a == v {
			return true
		}
	}
	return false
}
