package api

import (
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/aireply/ai-reply-back-end/internal/alerts"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/logging"
	"github.com/aireply/ai-reply-back-end/internal/transport/httpx"
)

// clientErrorRequest — POST /api/v1/client-errors денесі (қатаң: белгісіз өріс — 400).
//
// The apps have no crash reporter of their own: they send a short summary of
// a crash found on the next launch, or of a critical error (a purchase the
// server could not confirm, a keyboard that cannot reach the API), and the
// server's Telegram alerts carry it to the operator. Enums and short codes
// only; never message text, clipboard content or tokens.
type clientErrorRequest struct {
	Platform   string `json:"platform"`
	AppVersion string `json:"app_version"`
	OSVersion  string `json:"os_version"`
	Kind       string `json:"kind"`
	Code       string `json:"code"`
	Detail     string `json:"detail"`
	Count      int    `json:"count"`
}

var (
	clientVersionPattern = regexp.MustCompile(`^[A-Za-z0-9 ._()+-]{1,40}$`)
	clientCodePattern    = regexp.MustCompile(`^[A-Za-z0-9_.:/-]{1,80}$`)
	clientPlatforms      = map[string]bool{"ios": true, "android": true, "macos": true, "windows": true}
	clientKinds          = map[string]bool{"crash": true, "purchase": true, "api": true, "keyboard": true, "other": true}
)

// handleClientError — қосымшаның маңызды қатесі: журналға және Telegram ескертуіне.
//
// Optional auth like the installation endpoints (a crash can happen before
// sign-in). A crash is CRITICAL, a purchase problem ERROR, the rest WARNING;
// the alert channel deduplicates and rate-limits, the per-address and
// per-account buckets in front of this handler stop floods earlier.
func (s *Server) handleClientError(w http.ResponseWriter, r *http.Request) {
	var body clientErrorRequest
	if err := httpx.Decode(w, r, 4096, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	body.Platform, body.Kind = strings.ToLower(strings.TrimSpace(body.Platform)), strings.ToLower(strings.TrimSpace(body.Kind))
	switch {
	case !clientPlatforms[body.Platform]:
		httpx.Fail(w, domain.InvalidField("platform", "unsupported"))
		return
	case !clientKinds[body.Kind]:
		httpx.Fail(w, domain.InvalidField("kind", "unsupported"))
		return
	case !clientCodePattern.MatchString(body.Code):
		httpx.Fail(w, domain.InvalidField("code", "format"))
		return
	case body.AppVersion != "" && !clientVersionPattern.MatchString(body.AppVersion),
		body.OSVersion != "" && !clientVersionPattern.MatchString(body.OSVersion):
		httpx.Fail(w, domain.InvalidField("app_version", "format"))
		return
	case body.Count < 0 || body.Count > 10000:
		httpx.Fail(w, domain.InvalidField("count", "range"))
		return
	}
	// Whatever the app put there, no address, token or long number reaches the log.
	detail := alerts.Scrub(body.Detail)
	if body.Count == 0 {
		body.Count = 1
	}
	attrs := []any{"platform", body.Platform, "kind", body.Kind, "code", body.Code,
		"app_version", body.AppVersion, "os_version", body.OSVersion, "count", body.Count}
	if detail != "" {
		attrs = append(attrs, "detail", detail)
	}
	if user, ok := UserFrom(r.Context()); ok {
		attrs = append(attrs, "user_id", user.ID)
	}
	log := logging.FromContext(r.Context(), s.log)
	switch body.Kind {
	case "crash":
		log.Log(r.Context(), logging.LevelCritical, "app crash reported", attrs...)
	case "purchase":
		log.Log(r.Context(), slog.LevelError, "app purchase error reported", attrs...)
	default:
		log.Log(r.Context(), slog.LevelWarn, "app error reported", append(attrs, logging.Alert())...)
	}
	httpx.JSON(w, http.StatusAccepted, map[string]any{"ok": true})
}
