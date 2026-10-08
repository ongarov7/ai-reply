package web

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/admin"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/localization"
	"github.com/aireply/ai-reply-back-end/internal/repository"
	"github.com/aireply/ai-reply-back-end/internal/traits"
	"github.com/aireply/ai-reply-back-end/internal/transport/httpx"
)

type adminCtxKey string

const (
	adminKey   adminCtxKey = "admin"
	sessionKey adminCtxKey = "session"
	csrfCookie             = "aireply_csrf"
)

// adminView — әкімші беттерінің ортақ моделі.
type adminView struct {
	T         func(string) string
	Locale    string
	Locales   []string
	Title     string
	Active    string
	Admin     domain.AdminUser
	CSRF      string
	Flash     string
	FlashKind string
	Env       string
	Timezone  string
	Data      any
	// Dashboard-қа ғана қатысты.
	Ranges []string
	Series seriesJSON
	From   string
	To     string
}

type seriesJSON struct {
	Registrations string
	Generations   string
	Tokens        string
	Cost          string
	ActiveUsers   string
	PlanMix       string
	PlatformMix   string
	Errors        string
}

// guard — аутентификацияланған әкімші ғана өте алады.
func (s *Server) guard(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(s.cfg.Admin.CookieName)
		if err != nil {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}
		adminUser, session, err := s.admin.Authenticate(r.Context(), cookie.Value)
		if err != nil {
			s.clearCookie(w)
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}
		// Күй өзгертетін сұраныстарда CSRF токені міндетті.
		if r.Method == http.MethodPost {
			if err := r.ParseForm(); err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			if subtle.ConstantTimeCompare([]byte(r.FormValue("csrf")), []byte(session.CSRFToken)) != 1 {
				http.Error(w, "csrf token mismatch", http.StatusForbidden)
				return
			}
		}
		ctx := context.WithValue(r.Context(), adminKey, adminUser)
		ctx = context.WithValue(ctx, sessionKey, session)
		next(w, r.WithContext(ctx))
	})
}

// withAdmin — аутентификация нәтижесін контекстке салу (бір ғана жер).
func withAdmin(ctx context.Context, adminUser domain.AdminUser, session repository.AdminSession) context.Context {
	ctx = context.WithValue(ctx, adminKey, adminUser)
	return context.WithValue(ctx, sessionKey, session)
}

func adminFrom(ctx context.Context) domain.AdminUser {
	v, _ := ctx.Value(adminKey).(domain.AdminUser)
	return v
}

func sessionFrom(ctx context.Context) repository.AdminSession {
	v, _ := ctx.Value(sessionKey).(repository.AdminSession)
	return v
}

// view — ортақ модельді толтыру.
func (s *Server) view(r *http.Request, w http.ResponseWriter, active, titleKey string, data any) adminView {
	adminUser := adminFrom(r.Context())
	locale := adminUser.Locale
	if q := r.URL.Query().Get("lang"); q != "" {
		locale = domain.NormalizeLocale(q)
		_ = s.admin.SetLocale(r.Context(), adminUser.ID, locale)
		localization.SetCookie(w, locale, s.cfg.App.IsProduction())
	}
	flash, kind := readFlash(r, w)
	return adminView{
		T: s.bundle.Translator(locale), Locale: locale, Locales: domain.Locales,
		Title: s.bundle.T(locale, titleKey), Active: active, Admin: adminUser,
		CSRF: sessionFrom(r.Context()).CSRFToken, Flash: flash, FlashKind: kind,
		Env: s.cfg.App.Env, Timezone: s.cfg.App.Timezone, Data: data,
	}
}

// ---------------------------------------------------------------- login

// Бір әкімші поштасына кіру әрекеттері: IP шегіне қосымша (admin_login).
const (
	adminLoginPerEmail    = 5
	adminLoginEmailWindow = 15 * time.Minute
)

// allowLoginFor — бір поштаға 15 минутта 5 әрекет, қай IP-ден болса да; толса 429 жазады.
//
// The per-IP bucket alone lets anyone with many addresses keep guessing one
// admin's password; this caps the guesses at the account. Admin panel and
// simulator sign-in share it, as they share the account.
func (s *Server) allowLoginFor(w http.ResponseWriter, email string) bool {
	key := "admin_login_email:" + traits.Clamp(strings.ToLower(strings.TrimSpace(email)), 254)
	ok, retry := s.limiter.Allow(key, adminLoginPerEmail, adminLoginEmailWindow)
	if ok {
		return true
	}
	seconds := max(int(retry.Seconds()), 1)
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	httpx.Error(w, http.StatusTooManyRequests, httpx.CodeRateLimited, "Too many requests. Try again shortly.",
		map[string]any{"retry_after_seconds": seconds})
	return false
}

// sessionCSRF — форма (csrf) не тақырып (X-CSRF-Token) сессияның токенімен сәйкес пе.
func sessionCSRF(r *http.Request, session repository.AdminSession) bool {
	token := r.FormValue("csrf")
	if token == "" {
		token = r.Header.Get("X-CSRF-Token")
	}
	return token != "" && subtle.ConstantTimeCompare([]byte(token), []byte(session.CSRFToken)) == 1
}

// endSession — шығу: сессия болса, CSRF тексеріп жабады. false — 403 жазылды.
//
// Without the check any page could sign an admin out with a hidden form.
func (s *Server) endSession(w http.ResponseWriter, r *http.Request) bool {
	cookie, err := r.Cookie(s.cfg.Admin.CookieName)
	if err != nil {
		return true
	}
	_, session, err := s.admin.Authenticate(r.Context(), cookie.Value)
	if err != nil {
		return true // nothing open: clearing the cookie is all there is to do
	}
	if !sessionCSRF(r, session) {
		http.Error(w, "csrf token mismatch", http.StatusForbidden)
		return false
	}
	_ = s.admin.Logout(r.Context(), session.ID)
	return true
}

type loginView struct {
	T       func(string) string
	Locale  string
	Locales []string
	CSRF    string
	Error   string
}

func (s *Server) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	locale := s.locale(w, r)
	token := traits.RandomToken(16)
	http.SetCookie(w, &http.Cookie{
		Name: csrfCookie, Value: token, Path: "/admin", HttpOnly: true,
		Secure: s.cfg.Admin.SecureCookies, SameSite: http.SameSiteLaxMode, MaxAge: 3600,
	})
	message := ""
	if r.URL.Query().Get("error") != "" {
		message = s.bundle.T(locale, "admin.login.error")
	}
	s.render(w, "admin_login", "admin_login", loginView{
		T: s.bundle.Translator(locale), Locale: locale, Locales: domain.Locales,
		CSRF: token, Error: message,
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	cookie, err := r.Cookie(csrfCookie)
	if err != nil || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(r.FormValue("csrf"))) != 1 {
		http.Error(w, "csrf token mismatch", http.StatusForbidden)
		return
	}
	if !s.allowLoginFor(w, r.FormValue("email")) {
		return
	}

	session, err := s.admin.Login(r.Context(), r.FormValue("email"), r.FormValue("password"),
		clientIP(r, s.cfg.App.TrustProxy), r.UserAgent())
	if err != nil {
		s.log.Warn("admin login failed", "ip", clientIP(r, s.cfg.App.TrustProxy))
		http.Redirect(w, r, "/admin/login?error=1", http.StatusSeeOther)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name: s.cfg.Admin.CookieName, Value: session.Token, Path: "/", HttpOnly: true,
		Secure: s.cfg.Admin.SecureCookies, SameSite: http.SameSiteLaxMode,
		Expires: session.ExpiresAt,
	})
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if !s.endSession(w, r) {
		return
	}
	s.clearCookie(w)
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

func (s *Server) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: s.cfg.Admin.CookieName, Value: "", Path: "/", HttpOnly: true,
		Secure: s.cfg.Admin.SecureCookies, SameSite: http.SameSiteLaxMode, MaxAge: -1,
	})
}

// ---------------------------------------------------------------- dashboard

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	rng := admin.ParseRange(r.URL.Query().Get("range"), r.URL.Query().Get("from"),
		r.URL.Query().Get("to"), s.cfg.App.Location(), time.Now().UTC())

	dashboard, err := s.admin.Dashboard(r.Context(), rng)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	view := s.view(r, w, "dashboard", "admin.nav.dashboard", dashboard)
	view.Ranges = []string{"today", "7d", "30d", "month", "prev_month"}
	view.From = rng.From.In(s.cfg.App.Location()).Format("2006-01-02")
	view.To = rng.To.In(s.cfg.App.Location()).Format("2006-01-02")
	view.Series = seriesJSON{
		Registrations: encodePoints(dashboard.Registrations),
		Generations:   encodePoints(dashboard.Generations),
		Tokens:        encodePoints(dashboard.Tokens),
		Cost:          encodePoints(dashboard.Cost),
		ActiveUsers:   encodePoints(dashboard.ActiveUsers),
		PlanMix:       encodePoints(dashboard.PlanMix),
		PlatformMix:   encodePoints(dashboard.PlatformMix),
		Errors:        encodePoints(dashboard.Errors),
	}
	s.render(w, "admin_dashboard", "admin_layout", view)
}

func encodePoints(points []repository.Point) string {
	if len(points) == 0 {
		return "[]"
	}
	raw, err := json.Marshal(points)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

// ---------------------------------------------------------------- users

type userRowView struct {
	ID          string
	ShortID     string
	Identifier  string
	PlanCode    string
	SubStatus   string
	UsedToday   int
	DailyLimit  int
	TokensMonth int
	Platform    string
	AppVersion  string
	CreatedAt   string
	LastActive  string
	IsActive    bool
}

type usersView struct {
	Rows       []userRowView
	Total      int
	Filter     repository.UserFilter
	Plans      []domain.Plan
	PageNumber int
	HasPrev    bool
	HasNext    bool
	PrevURL    string
	NextURL    string
}

func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	limit := 25
	page := 1
	if v, err := strconv.Atoi(query.Get("page")); err == nil && v > 1 {
		page = v
	}
	filter := repository.UserFilter{
		Search:   traits.Clamp(query.Get("q"), 80),
		Status:   query.Get("status"),
		Platform: query.Get("platform"),
		PlanID:   query.Get("plan"),
		Page:     traits.NewPage(limit, (page-1)*limit),
		SortBy:   "created_at",
		SortDesc: true,
	}

	rows, total, err := s.admin.Users(r.Context(), filter)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	planList, err := s.admin.Plans(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}

	data := usersView{Total: total, Filter: filter, Plans: planList, PageNumber: page}
	for _, row := range rows {
		data.Rows = append(data.Rows, userRowView{
			ID:          row.User.ID,
			ShortID:     row.User.ID[:8],
			Identifier:  identifierLabel(row.User),
			PlanCode:    row.PlanCode,
			SubStatus:   row.SubStatus,
			UsedToday:   row.UsedToday,
			DailyLimit:  row.DailyLimit,
			TokensMonth: row.TokensMonth,
			Platform:    row.User.Platform,
			AppVersion:  row.User.AppVersion,
			CreatedAt:   row.User.CreatedAt.In(s.cfg.App.Location()).Format("2006-01-02 15:04"),
			LastActive:  formatOptional(row.User.LastActiveAt, s.cfg.App.Location()),
			IsActive:    row.User.Status == domain.UserActive,
		})
	}
	data.HasPrev = page > 1
	data.HasNext = page*limit < total
	data.PrevURL = pageURL(r, page-1)
	data.NextURL = pageURL(r, page+1)

	s.render(w, "admin_users", "admin_layout", s.view(r, w, "users", "admin.users.title", data))
}

// identifierLabel — телефон/пошта жабық түрде көрсетіледі.
func identifierLabel(u domain.User) string {
	if u.Phone != "" {
		return traits.MaskIdentifier(u.Phone)
	}
	if u.Email != "" {
		return traits.MaskIdentifier(u.Email)
	}
	if u.LegacyClient != "" {
		return "legacy:" + u.LegacyClient[:8]
	}
	return u.ID[:8]
}

func formatOptional(t *time.Time, loc *time.Location) string {
	if t == nil || t.IsZero() {
		return "—"
	}
	return t.In(loc).Format("2006-01-02 15:04")
}

func pageURL(r *http.Request, page int) string {
	if page < 1 {
		page = 1
	}
	values := url.Values{}
	for key, list := range r.URL.Query() {
		if key == "page" {
			continue
		}
		values[key] = list
	}
	values.Set("page", strconv.Itoa(page))
	return r.URL.Path + "?" + values.Encode()
}

func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	s.log.Error("admin page failed", "path", r.URL.Path, "error", err.Error())
	http.Error(w, "internal error", http.StatusInternalServerError)
}

// ---------------------------------------------------------------- flash

func setFlash(w http.ResponseWriter, kind, message string) {
	http.SetCookie(w, &http.Cookie{
		Name: "flash", Value: url.QueryEscape(kind + "|" + message), Path: "/admin",
		HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 20,
	})
}

func readFlash(r *http.Request, w http.ResponseWriter) (string, string) {
	cookie, err := r.Cookie("flash")
	if err != nil || cookie.Value == "" {
		return "", ""
	}
	http.SetCookie(w, &http.Cookie{Name: "flash", Value: "", Path: "/admin", MaxAge: -1})
	decoded, err := url.QueryUnescape(cookie.Value)
	if err != nil {
		return "", ""
	}
	for i := 0; i < len(decoded); i++ {
		if decoded[i] == '|' {
			return decoded[i+1:], decoded[:i]
		}
	}
	return "", ""
}
