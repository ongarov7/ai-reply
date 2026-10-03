package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/aireply/ai-reply-back-end/config"
	"github.com/aireply/ai-reply-back-end/internal/ai"
	"github.com/aireply/ai-reply-back-end/internal/auth"
	"github.com/aireply/ai-reply-back-end/internal/limits"
	"github.com/aireply/ai-reply-back-end/internal/middleware"
	"github.com/aireply/ai-reply-back-end/internal/payments"
	"github.com/aireply/ai-reply-back-end/internal/plans"
	"github.com/aireply/ai-reply-back-end/internal/subscriptions"
	"github.com/aireply/ai-reply-back-end/internal/transport/httpx"
	"github.com/aireply/ai-reply-back-end/internal/users"
)

// Pinger — дерекқордың дайындығын тексеру.
type Pinger func(context.Context) error

// Server — мобильді API.
type Server struct {
	cfg      config.Config
	auth     *auth.Service
	users    *users.Service
	plans    *plans.Service
	subs     *subscriptions.Service
	ai       *ai.Service
	limits   *limits.Service
	payments *payments.Service
	limiter  *middleware.Limiter
	ping     Pinger
	log      *slog.Logger
}

// Deps — сервер тәуелділіктері.
type Deps struct {
	Config   config.Config
	Auth     *auth.Service
	Users    *users.Service
	Plans    *plans.Service
	Subs     *subscriptions.Service
	AI       *ai.Service
	Limits   *limits.Service
	Payments *payments.Service
	Limiter  *middleware.Limiter
	Ping     Pinger
	Log      *slog.Logger
}

// New — API сервері.
func New(d Deps) *Server {
	return &Server{
		cfg: d.Config, auth: d.Auth, users: d.Users, plans: d.Plans, subs: d.Subs,
		ai: d.AI, limits: d.Limits, payments: d.Payments, limiter: d.Limiter, ping: d.Ping, log: d.Log,
	}
}

// Register — маршруттарды негізгі mux-қа қосады.
func (s *Server) Register(mux *http.ServeMux) {
	// Денсаулық тексерулері — балансерге арналған, аутентификациясыз.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "env": s.cfg.App.Env})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if s.ping != nil {
			if err := s.ping(r.Context()); err != nil {
				httpx.JSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false})
				return
			}
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	ip := func(r *http.Request) string { return httpx.ClientIP(r, s.cfg.App.TrustProxy) }
	limits := s.cfg.Limits

	otpRequest := middleware.RateLimit(s.limiter, "otp_request", limits.OTPRequestPerHour, time.Hour, ip)
	otpVerify := middleware.RateLimit(s.limiter, "otp_verify", limits.OTPVerifyPerHour, time.Hour, ip)
	generic := middleware.RateLimit(s.limiter, "generic", limits.GenericPerMinute, time.Minute, ip)
	aiLimit := middleware.RateLimit(s.limiter, "ai", limits.AIPerMinute, time.Minute, func(r *http.Request) string {
		if u, ok := UserFrom(r.Context()); ok {
			return u.ID
		}
		return httpx.ClientIP(r, s.cfg.App.TrustProxy)
	})

	// --- аутентификация: пошта OTP (Resend), Google, Apple
	mux.Handle("POST /api/v1/auth/email/otp/request", otpRequest(http.HandlerFunc(s.handleEmailOTPRequest)))
	mux.Handle("POST /api/v1/auth/email/otp/verify", otpVerify(http.HandlerFunc(s.handleEmailOTPVerify)))
	mux.Handle("POST /api/v1/auth/google", generic(http.HandlerFunc(s.handleGoogleSignIn)))
	mux.Handle("POST /api/v1/auth/apple", generic(http.HandlerFunc(s.handleAppleSignIn)))

	// Бұрынғы идентификатор ағыны: шыққан build-тер (телефон және пошта) үшін сақталған.
	mux.Handle("POST /api/v1/auth/request-otp", otpRequest(http.HandlerFunc(s.handleRequestOTP)))
	mux.Handle("POST /api/v1/auth/verify-otp", otpVerify(http.HandlerFunc(s.handleVerifyOTP)))
	mux.Handle("POST /api/v1/auth/refresh", generic(http.HandlerFunc(s.handleRefresh)))
	mux.Handle("POST /api/v1/auth/logout", generic(http.HandlerFunc(s.handleLogout)))

	// --- профиль және қолданыс
	mux.Handle("GET /api/v1/me", s.requireUser(http.HandlerFunc(s.handleMe)))
	mux.Handle("PATCH /api/v1/me", s.requireUser(http.HandlerFunc(s.handleUpdateMe)))
	// Android alias: HttpURLConnection refuses PATCH outright, and a reflection
	// hack on a platform class is a worse thing to ship than one extra route.
	mux.Handle("POST /api/v1/me", s.requireUser(http.HandlerFunc(s.handleUpdateMe)))
	mux.Handle("GET /api/v1/me/usage", s.requireUser(http.HandlerFunc(s.handleUsage)))
	mux.Handle("GET /api/v1/me/subscription", s.requireUser(http.HandlerFunc(s.handleSubscription)))
	mux.Handle("GET /api/v1/me/devices", s.requireUser(http.HandlerFunc(s.handleListDevices)))
	mux.Handle("POST /api/v1/me/consents", s.requireUser(http.HandlerFunc(s.handleSaveLegalConsent)))
	// Поштасы жоқ тіркелгіге (телефонмен ашылған) пошта қосу — кейін сол поштамен кіру үшін.
	mux.Handle("POST /api/v1/me/email/otp/request", s.requireUser(otpRequest(http.HandlerFunc(s.handleLinkEmailRequest))))
	mux.Handle("POST /api/v1/me/email/otp/verify", s.requireUser(otpVerify(http.HandlerFunc(s.handleLinkEmailVerify))))

	// --- каталог
	mux.Handle("GET /api/v1/plans", generic(http.HandlerFunc(s.handlePlans)))
	mux.Handle("GET /api/v1/config", generic(http.HandlerFunc(s.handleConfig)))

	// --- құрылғылар
	mux.Handle("POST /api/v1/devices", s.requireUser(http.HandlerFunc(s.handleRegisterDevice)))
	mux.Handle("DELETE /api/v1/devices/{id}", s.requireUser(http.HandlerFunc(s.handleDeleteDevice)))

	// --- AI
	mux.Handle("POST /api/v1/ai/reply", s.requireUser(aiLimit(http.HandlerFunc(s.handleReply))))
	// Нұсқау бойынша жаңа хабарлама («Create»). Сол квота, сол rate limit.
	mux.Handle("POST /api/v1/ai/compose", s.requireUser(aiLimit(http.HandlerFunc(s.handleCompose))))

	// --- төлемдер (демо адаптер)
	mux.Handle("POST /api/v1/payments/checkout", s.requireUser(http.HandlerFunc(s.handleCheckout)))
	mux.Handle("POST /api/v1/payments/{id}/confirm", s.requireUser(http.HandlerFunc(s.handleConfirmPayment)))

	// --- ескі мобильді build-тер (өзгеріссіз жұмыс істейді)
	if s.cfg.Auth.LegacyEnabled {
		mux.Handle("POST /v1/auth/register", otpRequest(http.HandlerFunc(s.handleLegacyRegister)))
		mux.Handle("POST /v1/reply/generate", s.requireLegacy(aiLimit(http.HandlerFunc(s.handleLegacyGenerate))))
	}
}
