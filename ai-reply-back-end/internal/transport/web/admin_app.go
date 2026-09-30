package web

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strings"

	"github.com/aireply/ai-reply-back-end/internal/admin"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/localization"
)

// appView — Vue қосымшасының қабығы.
type appView struct {
	T      func(string) string
	Locale string
	Boot   template.JS
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
			// Only hides what the role cannot use; every endpoint checks again.
			"permissions": admin.Permissions(adminUser.Role),
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
		T: s.bundle.Translator(locale), Locale: locale, Boot: template.JS(raw),
	})
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
