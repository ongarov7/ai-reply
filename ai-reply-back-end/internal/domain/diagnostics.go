package domain

import "time"

// Рұқсат диагностикасының күйлері (пернетақта, толық қатынас, буфер, хабарламалар).
//
// unknown is "nothing observed"; it is never evidence of a problem. A
// confirmed failure is observed_denied (the app or keyboard saw it) or
// user_reported_disabled. unsupported means the platform has no such thing
// (Full Access on Android, the selected keyboard on iOS).
const (
	DiagUnknown              = "unknown"
	DiagNotVerified          = "not_verified"
	DiagVerifiedEnabled      = "verified_enabled"
	DiagObservedDenied       = "observed_denied"
	DiagUserReportedDisabled = "user_reported_disabled"
	DiagUnsupported          = "unsupported"
)

// DiagnosticStatuses — барлық күй, көрсету ретімен.
var DiagnosticStatuses = []string{
	DiagUnknown, DiagNotVerified, DiagVerifiedEnabled, DiagObservedDenied, DiagUserReportedDisabled, DiagUnsupported,
}

// Онбординг күйі.
const (
	OnboardingUnknown    = "unknown"
	OnboardingNotStarted = "not_started"
	OnboardingInProgress = "in_progress"
	OnboardingCompleted  = "completed"
	OnboardingSkipped    = "skipped"
)

// OnboardingStatuses — барлық онбординг күйі.
var OnboardingStatuses = []string{
	OnboardingUnknown, OnboardingNotStarted, OnboardingInProgress, OnboardingCompleted, OnboardingSkipped,
}

// Диагностиканың көзі. Еске салуға тек app_check пен keyboard_check жарайды.
const (
	SourceAppCheck      = "app_check"
	SourceKeyboardCheck = "keyboard_check"
	SourceUserReport    = "user_report"
)

// DiagnosticSources — клиент жібере алатын көздер.
var DiagnosticSources = []string{SourceAppCheck, SourceKeyboardCheck, SourceUserReport}

// DiagnosticsFreshness — осыдан ескі диагностика еске салуға дәлел емес.
const DiagnosticsFreshness = 14 * 24 * time.Hour

// InstallationDiagnostics — бір орнатудың соңғы байқалған күйі (тек enum мен уақыт).
type InstallationDiagnostics struct {
	InstallationID         string // app_installations.id
	KeyboardSetupStatus    string
	KeyboardSelectedStatus string
	FullAccessStatus       string
	ClipboardStatus        string
	OnboardingStatus       string
	StatusSource           string
	LastSuccessfulPasteAt  *time.Time
	LastKeyboardCheckAt    *time.Time
	KeyboardVerifiedAt     *time.Time
	FullAccessVerifiedAt   *time.Time
	ClipboardDeniedAt      *time.Time
	OnboardingCompletedAt  *time.Time
	ReportedAt             time.Time
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

// NotificationAuthorizationFrom — хабарлама рұқсаты орнатудың өз өрістерінен.
func NotificationAuthorizationFrom(permission string, enabled bool) string {
	switch {
	case !enabled:
		return DiagUserReportedDisabled
	case permission == PermissionDenied:
		return DiagObservedDenied
	case permission == PermissionNotDetermined:
		return DiagNotVerified
	case permission == PermissionAuthorized || permission == PermissionProvisional || permission == PermissionEphemeral:
		return DiagVerifiedEnabled
	default:
		return DiagUnknown
	}
}

// Әкімшінің диагностика сүзгілері (тізім мен еске салу аудиториясы бірдей SQL-ді қолданады).
const (
	DiagFilterSetupIncomplete       = "setup_incomplete"
	DiagFilterKeyboardNotVerified   = "keyboard_not_verified"
	DiagFilterPasteProblems         = "paste_problems"
	DiagFilterFullAccessDisabled    = "full_access_disabled"
	DiagFilterNotificationsDisabled = "notifications_disabled"
	DiagFilterUnknown               = "unknown"
)

// DiagnosticFilters — әкімші тізімінің сүзгі чиптері ("" — барлығы).
var DiagnosticFilters = []string{
	DiagFilterSetupIncomplete, DiagFilterKeyboardNotVerified, DiagFilterPasteProblems,
	DiagFilterFullAccessDisabled, DiagFilterNotificationsDisabled, DiagFilterUnknown,
}
