// Package web — көпшілік лендинг және сервер жағында рендерленетін әкімші панелі.
package web

import (
	"embed"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/aireply/ai-reply-back-end/config"
	"github.com/aireply/ai-reply-back-end/internal/admin"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/localization"
	"github.com/aireply/ai-reply-back-end/internal/middleware"
	"github.com/aireply/ai-reply-back-end/internal/notifications"
	"github.com/aireply/ai-reply-back-end/internal/payments"
	"github.com/aireply/ai-reply-back-end/internal/plans"
)

//go:embed templates/*.gohtml static/*
var assets embed.FS

// Server — веб қабаты.
type Server struct {
	cfg       config.Config
	admin     *admin.Service
	plans     *plans.Service
	payments  *payments.Service
	notify    *notifications.Service
	bundle    *localization.Bundle
	limiter   *middleware.Limiter
	log       *slog.Logger
	templates map[string]*template.Template
	// adminVersion — әкімші скрипті мен стилінің хэші (?v=…): жаңа нұсқа кэштен алынбайды.
	adminVersion string
}

// Deps — тәуелділіктер.
type Deps struct {
	Config config.Config
	Admin  *admin.Service
	Plans  *plans.Service
	// Payments — лендинг тариф сатылатын-сатылмайтынын осыдан біледі (nil — ештеңе сатылмайды).
	Payments      *payments.Service
	Notifications *notifications.Service
	Bundle        *localization.Bundle
	Limiter       *middleware.Limiter
	Log           *slog.Logger
}

// New — шаблондарды жинап, серверді құрады.
func New(d Deps) (*Server, error) {
	s := &Server{
		cfg: d.Config, admin: d.Admin, plans: d.Plans, payments: d.Payments, notify: d.Notifications,
		bundle: d.Bundle, limiter: d.Limiter, log: d.Log,
		templates: map[string]*template.Template{},
	}
	pages := map[string][]string{
		"landing": {"templates/public_layout.gohtml", "templates/landing.gohtml"},
		"legal":   {"templates/public_layout.gohtml", "templates/legal.gohtml"},
		// Қолдау және тіркелгіні жою — дүкендер сұрайтын көпшілік беттер.
		"support":        {"templates/public_layout.gohtml", "templates/support.gohtml"},
		"account_delete": {"templates/public_layout.gohtml", "templates/account_delete.gohtml"},
		"admin_login":    {"templates/admin_login.gohtml"},
		"admin_app":      {"templates/admin_app.gohtml"},
		// Интерактивті өнім симуляторы — бөлек қабық, әкімші панелін қозғамайды.
		"simulator_login": {"templates/simulator_login.gohtml"},
		"simulator_app":   {"templates/simulator_app.gohtml"},
	}
	for name, files := range pages {
		tpl, err := template.New(name).Funcs(funcs()).ParseFS(assets, files...)
		if err != nil {
			return nil, err
		}
		s.templates[name] = tpl
	}
	version, err := assetVersion("static/admin-app.js", "static/admin.css")
	if err != nil {
		return nil, err
	}
	s.adminVersion = version
	return s, nil
}

func funcs() template.FuncMap {
	return template.FuncMap{
		"raw": func(v string) template.HTML { return template.HTML(v) },
	}
}

// Register — веб маршруттары.
func (s *Server) Register(mux *http.ServeMux) {
	static, err := fs.Sub(assets, "static")
	if err != nil {
		panic("web: static assets missing: " + err.Error())
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", cacheStatic(http.FileServer(http.FS(static)))))

	mux.HandleFunc("GET /{$}", s.handleLanding)
	mux.HandleFunc("GET /offer", s.handleTerms)
	mux.HandleFunc("GET /terms", s.handleTerms)
	mux.HandleFunc("GET /privacy", s.handlePrivacy)
	mux.HandleFunc("GET /support", s.handleSupport)
	mux.HandleFunc("GET /account/delete", s.handleAccountDelete)
	mux.HandleFunc("GET /delete-account", s.handleDeleteAccountRedirect)

	ipKey := func(r *http.Request) string { return clientIP(r, s.cfg.App.TrustProxy) }
	loginLimit := middleware.RateLimit(s.limiter, "admin_login", s.cfg.Limits.AdminLoginPerHour, time.Hour, ipKey)

	mux.HandleFunc("GET /admin/login", s.handleLoginForm)
	mux.Handle("POST /admin/login", loginLimit(http.HandlerFunc(s.handleLogin)))
	mux.HandleFunc("POST /admin/logout", s.handleLogout)

	// Барлық /admin/* беті — бір Vue қосымшасы (деректер /api/v1/admin/* арқылы келеді).
	mux.Handle("GET /admin", s.guard(s.handleAdminApp))
	mux.Handle("GET /admin/{path...}", s.guard(s.handleAdminApp))

	// Интерактивті өнім симуляторы. Кіру — әкімші тіркелгісімен, сол шектеумен.
	// SIMULATOR_ENABLED=false (production әдепкісі) болса, маршруттар мүлде жоқ: 404.
	if !s.cfg.App.SimulatorEnabled {
		return
	}
	mux.HandleFunc("GET /simulator/login", s.handleSimulatorLoginForm)
	mux.Handle("POST /simulator/login", loginLimit(http.HandlerFunc(s.handleSimulatorLogin)))
	mux.HandleFunc("POST /simulator/logout", s.handleSimulatorLogout)
	mux.Handle("GET /simulator", s.simulatorGuard(s.handleSimulatorApp))
	mux.Handle("GET /simulator/{path...}", s.simulatorGuard(s.handleSimulatorApp))
}

func cacheStatic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		next.ServeHTTP(w, r)
	})
}

// baseView — барлық беттің ортақ деректері.
type baseView struct {
	T            func(string) string
	Locale       string
	Locales      []string
	Year         int
	ContactEmail string
}

func (s *Server) base(locale string) baseView {
	return baseView{
		T:            s.bundle.Translator(locale),
		Locale:       locale,
		Locales:      domain.Locales,
		Year:         time.Now().Year(),
		ContactEmail: s.cfg.App.ContactEmail(),
	}
}

func (s *Server) render(w http.ResponseWriter, name, entry string, data any) {
	tpl, ok := s.templates[name]
	if !ok {
		http.Error(w, "template not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tpl.ExecuteTemplate(w, entry, data); err != nil {
		s.log.Error("template render failed", "template", name, "error", err.Error())
	}
}
