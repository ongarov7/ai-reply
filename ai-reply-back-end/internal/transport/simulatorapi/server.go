// Package simulatorapi — интерактивті демонстрацияның JSON API-і.
//
// Аутентификация әкімші панелімен бірдей: сол cookie сессиясы, сол CSRF
// тақырыбы. Бөлек құпиясөз жоқ — демек, кодта да, фронтендте де сақталатын
// тіркелгі деректері жоқ.
package simulatorapi

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"time"

	"github.com/aireply/ai-reply-back-end/config"
	"github.com/aireply/ai-reply-back-end/internal/admin"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/middleware"
	"github.com/aireply/ai-reply-back-end/internal/repository"
	"github.com/aireply/ai-reply-back-end/internal/simulator"
	"github.com/aireply/ai-reply-back-end/internal/transport/httpx"
)

type ctxKey string

const (
	adminKey   ctxKey = "admin"
	sessionKey ctxKey = "session"
)

// Server — симулятор API.
type Server struct {
	cfg     config.Config
	admin   *admin.Service
	sim     *simulator.Service
	limiter *middleware.Limiter
	log     *slog.Logger
}

// Deps — тәуелділіктер.
type Deps struct {
	Config    config.Config
	Admin     *admin.Service
	Simulator *simulator.Service
	Limiter   *middleware.Limiter
	Log       *slog.Logger
}

// New — сервер.
func New(d Deps) *Server {
	return &Server{cfg: d.Config, admin: d.Admin, sim: d.Simulator, limiter: d.Limiter, log: d.Log}
}

// Register — маршруттар. SIMULATOR_ENABLED=false болса, ештеңе тіркелмейді (404).
func (s *Server) Register(mux *http.ServeMux) {
	if !s.cfg.App.SimulatorEnabled {
		return
	}
	generate := middleware.RateLimit(s.limiter, "simulator_ai", s.cfg.Limits.AIPerMinute, time.Minute,
		func(r *http.Request) string {
			if a := adminFrom(r.Context()); a.ID != "" {
				return a.ID
			}
			return httpx.ClientIP(r, s.cfg.App.TrustProxy)
		})

	mux.Handle("GET /api/v1/simulator/bootstrap", s.guard(s.handleBootstrap))
	mux.Handle("GET /api/v1/simulator/health", s.guard(s.handleHealth))
	mux.Handle("GET /api/v1/simulator/account", s.guard(s.handleAccount))
	mux.Handle("POST /api/v1/simulator/account/profile", s.guard(s.handleSaveProfile))
	mux.Handle("POST /api/v1/simulator/account/plan", s.guard(s.handleSetPlan))
	mux.Handle("POST /api/v1/simulator/account/reset", s.guard(s.handleReset))
	mux.Handle("POST /api/v1/simulator/account/reset-quota", s.guard(s.handleResetQuota))
	mux.Handle("POST /api/v1/simulator/generate", s.guardWith(generate, s.handleGenerate))
	mux.Handle("POST /api/v1/simulator/transcribe", s.guard(s.handleTranscribe))
	mux.Handle("GET /api/v1/simulator/admin/overview", s.guard(s.handleAdminOverview))
	mux.Handle("POST /api/v1/simulator/admin/plan-draft", s.guard(s.handleSaveDraft))
	mux.Handle("POST /api/v1/simulator/admin/plan-draft/reset", s.guard(s.handleResetDrafts))
}

// guard — әкімші сессиясы + күй өзгертетін сұраныстарда CSRF.
func (s *Server) guard(next http.HandlerFunc) http.Handler {
	return s.guardWith(nil, next)
}

// guardWith — guard, бірақ аутентификациядан кейін қосымша орта қабатпен
// (сондықтан жылдамдық шегі әкімші идентификаторы бойынша есептеледі).
func (s *Server) guardWith(extra func(http.Handler) http.Handler, next http.HandlerFunc) http.Handler {
	handler := http.Handler(next)
	if extra != nil {
		handler = extra(handler)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(s.cfg.Admin.CookieName)
		if err != nil {
			httpx.Error(w, http.StatusUnauthorized, httpx.CodeUnauthorized, "Sign in required.", nil)
			return
		}
		adminUser, session, err := s.admin.Authenticate(r.Context(), cookie.Value)
		if err != nil {
			httpx.Error(w, http.StatusUnauthorized, httpx.CodeUnauthorized, "Sign in required.", nil)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(session.CSRFToken)) != 1 {
				httpx.Error(w, http.StatusForbidden, "CSRF_MISMATCH", "CSRF token mismatch.", nil)
				return
			}
		}
		ctx := context.WithValue(r.Context(), adminKey, adminUser)
		ctx = context.WithValue(ctx, sessionKey, session)
		handler.ServeHTTP(w, r.WithContext(ctx))
	})
}

func adminFrom(ctx context.Context) domain.AdminUser {
	v, _ := ctx.Value(adminKey).(domain.AdminUser)
	return v
}

func sessionFrom(ctx context.Context) repository.AdminSession {
	v, _ := ctx.Value(sessionKey).(repository.AdminSession)
	return v
}
