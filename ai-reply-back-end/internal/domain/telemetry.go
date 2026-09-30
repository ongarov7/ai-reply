package domain

import "time"

// Кіру оқиғалары (auth_events). Құпия мән (код, токен, құпиясөз) ешқашан жазылмайды.
const (
	AuthOTPRequested     = "otp_requested"
	AuthOTPVerified      = "otp_verified"
	AuthOTPFailed        = "otp_failed"
	AuthGoogleSuccess    = "google_auth_success"
	AuthGoogleFailed     = "google_auth_failed"
	AuthAppleSuccess     = "apple_auth_success"
	AuthAppleFailed      = "apple_auth_failed"
	AuthLoginSuccess     = "auth_login_success"
	AuthLoginFailed      = "auth_login_failed"
	AuthSignupSuccess    = "auth_signup_success"
	AuthSessionRefreshed = "session_refreshed"
	AuthSessionExpired   = "session_expired"
	AuthLogout           = "auth_logout"
)

// AuthEventNames — әкімші сүзгісіне арналған тізім.
var AuthEventNames = []string{
	AuthOTPRequested, AuthOTPVerified, AuthOTPFailed, AuthGoogleSuccess, AuthGoogleFailed,
	AuthAppleSuccess, AuthAppleFailed, AuthLoginSuccess, AuthLoginFailed, AuthSignupSuccess,
	AuthSessionRefreshed, AuthSessionExpired, AuthLogout,
}

// Оқиға нәтижесі.
const (
	OutcomeSuccess = "success"
	OutcomeFailure = "failure"
)

// AuthEvent — кіру/шығу әрекетінің қауіпсіздік жазбасы.
//
// SubjectHash is a keyed hash of the e-mail or phone the attempt named, so a
// failed sign-in for an address can be found later without storing the
// address itself; UserID is set whenever the account is known.
type AuthEvent struct {
	ID             string
	Name           string
	Method         string
	Outcome        string
	ErrorCode      string
	UserID         string
	SubjectHash    string
	InstallationID string
	Platform       string
	AppVersion     string
	AppBuild       string
	OSVersion      string
	IP             string
	RequestID      string
	CreatedAt      time.Time
}

// AppEvent — қосымша жіберген операциялық оқиға (хабарлама мәтінінсіз).
type AppEvent struct {
	ID             string
	ClientEventID  string
	Name           string
	UserID         string
	InstallationID string
	SessionID      string
	Platform       string
	AppVersion     string
	AppBuild       string
	OSVersion      string
	DeviceModel    string
	Outcome        string
	ErrorCode      string
	RequestID      string
	Properties     map[string]any
	OccurredAt     time.Time
	ReceivedAt     time.Time
}

// AppSession — қосымшаның бір алдыңғы жоспардағы сессиясы.
type AppSession struct {
	ID             string
	SessionID      string
	InstallationID string
	UserID         string
	Platform       string
	AppVersion     string
	AppBuild       string
	OSVersion      string
	DeviceModel    string
	StartedAt      time.Time
	LastActivityAt time.Time
	EndedAt        *time.Time
	EventCount     int
}

// APIError — сервер қайтарған қате жауаптың метадерегі (дене мен тақырыптарсыз).
type APIError struct {
	ID             string
	RequestID      string
	TraceID        string
	UserID         string
	InstallationID string
	SessionID      string
	Platform       string
	AppVersion     string
	AppBuild       string
	OSVersion      string
	Method         string
	Route          string
	StatusCode     int
	ErrorCode      string
	DurationMS     int
	OccurredAt     time.Time
}
