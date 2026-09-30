package api

import (
	"net/http"

	"github.com/aireply/ai-reply-back-end/internal/auth"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/reqctx"
	"github.com/aireply/ai-reply-back-end/internal/transport/httpx"
)

// Кіру оқиғалары: әр әрекет — бір жазба (код, токен, құпиясөз ешқашан жазылмайды).

// authEvent — қауіпсіздік журналына жазба. subjectKind/subject — пошта не телефон
// (тек кілтті хэші сақталады); device — ескі қосымшалар тақырып жібермегенде.
func (s *Server) authEvent(r *http.Request, name, method, userID, subjectKind, subject string, device *deviceRequest, err error) {
	if s.telemetry == nil {
		return
	}
	client := reqctx.From(r.Context())
	event := domain.AuthEvent{
		Name: name, Method: method, Outcome: domain.OutcomeSuccess, UserID: userID,
		InstallationID: client.InstallationID, Platform: client.Platform, AppVersion: client.AppVersion,
		AppBuild: client.AppBuild, OSVersion: client.OSVersion,
		IP: httpx.ClientIP(r, s.cfg.App.TrustProxy), RequestID: client.RequestID,
	}
	if subject != "" {
		event.SubjectHash = s.telemetry.SubjectHash(subjectKind, subject)
	}
	if event.Platform == "" && device != nil {
		event.Platform = reqctx.Platform(device.Platform)
		event.AppVersion = reqctx.Version(device.AppVersion)
		event.OSVersion = reqctx.Version(device.OSVersion)
	}
	if err != nil {
		event.Outcome = domain.OutcomeFailure
		_, event.ErrorCode, _ = httpx.Translate(err)
	}
	s.telemetry.RecordAuth(r.Context(), event)
}

// loginOutcome — кіру нәтижесі: сәтті (жаңа тіркелгі болса signup), не сәтсіз.
func (s *Server) loginOutcome(r *http.Request, method string, session auth.Session, subjectKind, subject string,
	device *deviceRequest, err error) {
	if err != nil {
		s.authEvent(r, domain.AuthLoginFailed, method, "", subjectKind, subject, device, err)
		return
	}
	name := domain.AuthLoginSuccess
	if session.IsNewUser {
		name = domain.AuthSignupSuccess
	}
	s.authEvent(r, name, method, session.User.ID, subjectKind, subject, device, nil)
}
