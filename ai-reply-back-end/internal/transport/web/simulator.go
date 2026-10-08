package web

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"strings"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/localization"
	"github.com/aireply/ai-reply-back-end/internal/simulator"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// simulatorCSRFCookie — кіру формасының бір реттік токені.
const simulatorCSRFCookie = "aireply_sim_csrf"

// handleSimulatorLoginForm — демонстрацияға кіру беті.
//
// Тіркелгі деректері әкімші панелімен бірдей. Олар кодта да, фронтендте де
// жоқ: сервер тек енгізілгенді тексереді.
func (s *Server) handleSimulatorLoginForm(w http.ResponseWriter, r *http.Request) {
	// Сессиясы барды бірден ішке жіберу — көрсетілім алдында артық қадам жоқ.
	if cookie, err := r.Cookie(s.cfg.Admin.CookieName); err == nil {
		if _, _, err := s.admin.Authenticate(r.Context(), cookie.Value); err == nil {
			http.Redirect(w, r, "/simulator", http.StatusSeeOther)
			return
		}
	}
	locale := s.locale(w, r)
	token := traits.RandomToken(16)
	http.SetCookie(w, &http.Cookie{
		Name: simulatorCSRFCookie, Value: token, Path: "/simulator", HttpOnly: true,
		Secure: s.cfg.Admin.SecureCookies, SameSite: http.SameSiteLaxMode, MaxAge: 3600,
	})
	message := ""
	if r.URL.Query().Get("error") != "" {
		message = s.bundle.T(locale, "admin.login.error")
	}
	s.render(w, "simulator_login", "simulator_login", loginView{
		T: s.bundle.Translator(locale), Locale: locale, Locales: domain.Locales,
		CSRF: token, Error: message,
	})
}

// handleSimulatorLogin — кіру. Жылдамдық шегі әкімші кіруімен ортақ.
func (s *Server) handleSimulatorLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	cookie, err := r.Cookie(simulatorCSRFCookie)
	if err != nil || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(r.FormValue("csrf"))) != 1 {
		http.Error(w, "csrf token mismatch", http.StatusForbidden)
		return
	}
	if !s.allowLoginFor(w, r, r.FormValue("email")) {
		return
	}
	session, err := s.admin.Login(r.Context(), r.FormValue("email"), r.FormValue("password"),
		clientIP(r, s.cfg.App.TrustProxy), r.UserAgent())
	if err != nil {
		if errors.Is(err, domain.ErrUnauthorized) {
			s.loginFailed(r, r.FormValue("email"))
		}
		s.log.Warn("simulator login failed", "ip", clientIP(r, s.cfg.App.TrustProxy))
		http.Redirect(w, r, "/simulator/login?error=1", http.StatusSeeOther)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: s.cfg.Admin.CookieName, Value: session.Token, Path: "/", HttpOnly: true,
		Secure: s.cfg.Admin.SecureCookies, SameSite: http.SameSiteLaxMode,
		Expires: session.ExpiresAt,
	})
	http.Redirect(w, r, "/simulator", http.StatusSeeOther)
}

// handleSimulatorLogout — шығу.
func (s *Server) handleSimulatorLogout(w http.ResponseWriter, r *http.Request) {
	if !s.endSession(w, r) {
		return
	}
	s.clearCookie(w)
	http.Redirect(w, r, "/simulator/login", http.StatusSeeOther)
}

// simulatorGuard — сессиясы жоқты кіру бетіне жібереді (JSON емес, бет).
func (s *Server) simulatorGuard(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(s.cfg.Admin.CookieName)
		if err != nil {
			http.Redirect(w, r, "/simulator/login", http.StatusSeeOther)
			return
		}
		adminUser, session, err := s.admin.Authenticate(r.Context(), cookie.Value)
		if err != nil {
			s.clearCookie(w)
			http.Redirect(w, r, "/simulator/login", http.StatusSeeOther)
			return
		}
		ctx := withAdmin(r.Context(), adminUser, session)
		next(w, r.WithContext(ctx))
	})
}

// handleSimulatorApp — SPA қабығы. Бастапқы дерек бетпен бірге келеді,
// сондықтан ашылған бойда бос экран көрінбейді.
func (s *Server) handleSimulatorApp(w http.ResponseWriter, r *http.Request) {
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
		"locale":  locale,
		"locales": domain.Locales,
		"env":     s.cfg.App.Env,
		"account": map[string]any{"label": simulator.AccountLabel},
		"i18n":    s.simulatorMessages(),
	}
	raw, err := json.Marshal(boot)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.render(w, "simulator_app", "simulator_app", appView{
		T: s.bundle.Translator(locale), Locale: locale, Boot: template.JS(raw),
	})
}

// simulatorMessages — SPA-ға тек қажет кілттер: sim.* және common.*.
func (s *Server) simulatorMessages() map[string]map[string]string {
	out := map[string]map[string]string{}
	for _, locale := range domain.Locales {
		bucket := map[string]string{}
		for _, key := range s.bundle.Keys() {
			if strings.HasPrefix(key, "sim.") || strings.HasPrefix(key, "common.") ||
				strings.HasPrefix(key, "pricing.per_day") {
				bucket[key] = s.bundle.T(locale, key)
			}
		}
		out[locale] = bucket
	}
	return out
}
