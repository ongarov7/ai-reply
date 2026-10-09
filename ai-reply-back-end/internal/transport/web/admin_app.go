package web

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"html/template"
	"net/http"
	"strings"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/localization"
)

// appView — Vue қосымшасының қабығы.
type appView struct {
	T       func(string) string
	Locale  string
	Boot    template.JS
	Version string
}

// handleAdminApp — SPA қабығын береді; аудармалар мен CSRF бірден кіріктіріледі.
func (s *Server) handleAdminApp(w http.ResponseWriter, r *http.Request) {
	adminUser := adminFrom(r.Context())
	locale := adminUser.Locale
	if q := r.URL.Query().Get("lang"); q != "" {
		locale = domain.NormalizeLocale(q)
		_ = s.admin.SetLocale(r.Context(), adminUser.ID, locale)
		localization.SetCookie(w, locale, s.cfg.App.IsProduction())
	}

	boot := map[string]any{
		"csrf": sessionFrom(r.Context()).CSRFToken,
		"admin": map[string]any{
			"id": adminUser.ID, "email": adminUser.Email, "name": adminUser.Name, "role": adminUser.Role,
		},
		"locale":       locale,
		"locales":      domain.Locales,
		"env":          s.cfg.App.Env,
		"timezone":     s.cfg.App.Timezone,
		"demo_mode":    s.cfg.Auth.DemoMode,
		"payment_mode": s.cfg.Payments.Mode,
		"i18n":         s.adminMessages(),
	}
	raw, err := json.Marshal(boot)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.render(w, "admin_app", "admin_app", appView{
		T: s.bundle.Translator(locale), Locale: locale, Boot: template.JS(raw), Version: s.adminVersion,
	})
}

// assetVersion — кіріктірілген файлдар мазмұнының қысқа хэші.
//
// Static files are cached for an hour; the hash in the script and stylesheet
// URLs changes with every deploy that touches them, so a browser never runs
// an old admin script against a newer API.
func assetVersion(names ...string) (string, error) {
	h := sha256.New()
	for _, name := range names {
		raw, err := assets.ReadFile(name)
		if err != nil {
			return "", err
		}
		h.Write(raw)
	}
	return hex.EncodeToString(h.Sum(nil))[:12], nil
}

// adminMessages — SPA-ға қажет кілттер ғана (лендинг мәтіндері жіберілмейді).
func (s *Server) adminMessages() map[string]map[string]string {
	out := map[string]map[string]string{}
	for _, locale := range domain.Locales {
		bucket := map[string]string{}
		for _, key := range s.bundle.Keys() {
			if strings.HasPrefix(key, "admin.") || strings.HasPrefix(key, "common.") ||
				strings.HasPrefix(key, "pricing.per_day") {
				bucket[key] = s.bundle.T(locale, key)
			}
		}
		out[locale] = bucket
	}
	return out
}
