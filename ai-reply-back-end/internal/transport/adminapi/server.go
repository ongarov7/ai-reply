// Package adminapi — әкімші панелінің JSON API-і (Vue қосымшасы осыны шақырады).
package adminapi

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aireply/ai-reply-back-end/config"
	"github.com/aireply/ai-reply-back-end/internal/admin"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/limits"
	"github.com/aireply/ai-reply-back-end/internal/middleware"
	"github.com/aireply/ai-reply-back-end/internal/notifications"
	"github.com/aireply/ai-reply-back-end/internal/repository"
	"github.com/aireply/ai-reply-back-end/internal/traits"
	"github.com/aireply/ai-reply-back-end/internal/transport/httpx"
)

type ctxKey string

const (
	adminKey   ctxKey = "admin"
	sessionKey ctxKey = "session"
)

// Server — әкімші API.
type Server struct {
	cfg     config.Config
	admin   *admin.Service
	limits  *limits.Service
	notify  *notifications.Service
	limiter *middleware.Limiter
	log     *slog.Logger
}

// Deps — тәуелділіктер.
type Deps struct {
	Config        config.Config
	Admin         *admin.Service
	Limits        *limits.Service
	Notifications *notifications.Service
	Limiter       *middleware.Limiter
	Log           *slog.Logger
}

// New — сервер.
func New(d Deps) *Server {
	limiter := d.Limiter
	if limiter == nil {
		limiter = middleware.NewLimiter()
	}
	return &Server{cfg: d.Config, admin: d.Admin, limits: d.Limits, notify: d.Notifications, limiter: limiter, log: d.Log}
}

// Register — маршруттар. Әр маршрут рұқсатты серверде тексереді.
func (s *Server) Register(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/admin/session", s.guard(s.handleSession))
	mux.Handle("POST /api/v1/admin/locale", s.guard(s.handleSetLocale))
	mux.Handle("GET /api/v1/admin/dashboard", s.can(admin.PermDashboardRead, s.handleDashboard))
	mux.Handle("GET /api/v1/admin/users", s.can(admin.PermUsersRead, s.handleUsers))
	mux.Handle("GET /api/v1/admin/users/{id}", s.can(admin.PermUsersRead, s.handleUserDetail))
	mux.Handle("POST /api/v1/admin/users/{id}/status", s.can(admin.PermUsersWrite, s.handleUserStatus))
	mux.Handle("POST /api/v1/admin/users/{id}/plan", s.can(admin.PermUsersWrite, s.handleUserPlan))
	mux.Handle("POST /api/v1/admin/users/{id}/reset-quota", s.can(admin.PermUsersWrite, s.handleResetQuota))
	mux.Handle("POST /api/v1/admin/users/{id}/revoke-sessions", s.can(admin.PermUsersWrite, s.handleRevokeSessions))
	mux.Handle("GET /api/v1/admin/plans", s.can(admin.PermDashboardRead, s.handlePlans))
	mux.Handle("POST /api/v1/admin/plans", s.can(admin.PermPlansWrite, s.handlePlanCreate))
	mux.Handle("PATCH /api/v1/admin/plans/{id}", s.can(admin.PermPlansWrite, s.handlePlanUpdate))
	mux.Handle("POST /api/v1/admin/plans/{id}/archive", s.can(admin.PermPlansWrite, s.handlePlanArchive))
	mux.Handle("GET /api/v1/admin/audit", s.can(admin.PermAuditRead, s.handleAudit))
	mux.Handle("GET /api/v1/admin/settings", s.can(admin.PermSettingsRead, s.handleSettings))
	mux.Handle("POST /api/v1/admin/settings/pricing", s.can(admin.PermSettingsWrite, s.handleSavePricing))
	mux.Handle("POST /api/v1/admin/settings/limits", s.can(admin.PermSettingsWrite, s.handleSaveLimits))
	s.registerNotifications(mux)
	s.registerDiagnostics(mux)
}

// can — сессия + CSRF (guard) және рөлдің рұқсаты. Батырманы жасыру жеткіліксіз: тексеру осында.
func (s *Server) can(permission string, next http.HandlerFunc) http.Handler {
	return s.guard(func(w http.ResponseWriter, r *http.Request) {
		if !admin.Can(adminFrom(r.Context()).Role, permission) {
			httpx.Error(w, http.StatusForbidden, "FORBIDDEN", "This role cannot do that.",
				map[string]any{"permission": permission})
			return
		}
		next(w, r)
	})
}

// guard — cookie сессиясы + күй өзгертетін сұраныстарда CSRF тақырыбы.
func (s *Server) guard(next http.HandlerFunc) http.Handler {
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
		next(w, r.WithContext(ctx))
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

func (s *Server) ip(r *http.Request) string { return httpx.ClientIP(r, s.cfg.App.TrustProxy) }

// ---------------------------------------------------------------- session

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	adminUser := adminFrom(r.Context())
	httpx.JSON(w, http.StatusOK, map[string]any{
		"admin": map[string]any{
			"id": adminUser.ID, "email": adminUser.Email, "name": adminUser.Name,
			"role": adminUser.Role, "locale": adminUser.Locale,
			"permissions": admin.Permissions(adminUser.Role),
		},
		"csrf":         sessionFrom(r.Context()).CSRFToken,
		"env":          s.cfg.App.Env,
		"timezone":     s.cfg.App.Timezone,
		"demo_mode":    s.cfg.Auth.DemoMode,
		"payment_mode": s.cfg.Payments.Mode,
	})
}

func (s *Server) handleSetLocale(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Locale string `json:"locale"`
	}
	if err := httpx.Decode(w, r, 1024, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	if err := s.admin.SetLocale(r.Context(), adminFrom(r.Context()).ID, body.Locale); err != nil {
		httpx.Fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"locale": domain.NormalizeLocale(body.Locale)})
}

// ---------------------------------------------------------------- dashboard

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	rng := admin.ParseRange(query.Get("range"), query.Get("from"), query.Get("to"),
		s.cfg.App.Location(), s.admin.Now())

	dashboard, err := s.admin.Dashboard(r.Context(), rng)
	if err != nil {
		s.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"range": map[string]string{
			"key":  dashboard.Range.Key,
			"from": dashboard.Range.From.In(s.cfg.App.Location()).Format("2006-01-02"),
			"to":   dashboard.Range.To.In(s.cfg.App.Location()).Format("2006-01-02"),
		},
		"stats": map[string]any{
			"total_users": dashboard.Stats.TotalUsers, "new_today": dashboard.Stats.NewUsersToday,
			"new_month": dashboard.Stats.NewUsersMonth, "active_30d": dashboard.Stats.ActiveUsers30d,
			"paid_users": dashboard.Stats.PaidUsers, "free_users": dashboard.Stats.FreeUsers,
			"requests_today": dashboard.Stats.RequestsToday, "requests_range": dashboard.Stats.RequestsMonth,
			"input_tokens": dashboard.Stats.InputTokens, "output_tokens": dashboard.Stats.OutputTokens,
			"total_tokens": dashboard.Stats.TotalTokens, "cost_usd": dashboard.CostUSD,
			"succeeded": dashboard.Stats.Succeeded, "failed": dashboard.Stats.Failed,
			"avg_latency_ms": dashboard.Stats.AvgLatencyMS,
			"ios_users":      dashboard.Stats.IOSUsers, "android_users": dashboard.Stats.AndroidUsers,
		},
		"series": map[string]any{
			"registrations": dashboard.Registrations,
			"generations":   dashboard.Generations,
			"tokens":        dashboard.Tokens,
			"cost":          dashboard.Cost,
			"active_users":  dashboard.ActiveUsers,
			"plan_mix":      dashboard.PlanMix,
			"platform_mix":  dashboard.PlatformMix,
			"errors":        dashboard.Errors,
			"app_versions":  dashboard.AppVersions,
			"top_cost":      dashboard.TopCost,
		},
	})
}

// ---------------------------------------------------------------- users

func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	limit := 25
	if v, err := strconv.Atoi(query.Get("limit")); err == nil && v > 0 && v <= 100 {
		limit = v
	}
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
		SortBy:   query.Get("sort"),
		SortDesc: query.Get("dir") != "asc",
	}

	rows, total, err := s.admin.Users(r.Context(), filter)
	if err != nil {
		s.fail(w, err)
		return
	}
	loc := s.cfg.App.Location()
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{
			"id":           row.User.ID,
			"identifier":   maskIdentifier(row.User),
			"plan_code":    row.PlanCode,
			"sub_status":   row.SubStatus,
			"used_today":   row.UsedToday,
			"daily_limit":  row.DailyLimit,
			"tokens_month": row.TokensMonth,
			"platform":     row.User.Platform,
			"app_version":  row.User.AppVersion,
			"locale":       row.User.Locale,
			"created_at":   row.User.CreatedAt.In(loc).Format("2006-01-02 15:04"),
			"last_active":  optionalTime(row.User.LastActiveAt, loc),
			"status":       row.User.Status,
		})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"users": out, "total": total, "page": page, "limit": limit,
	})
}

func (s *Server) handleUserDetail(w http.ResponseWriter, r *http.Request) {
	detail, err := s.admin.UserDetail(r.Context(), r.PathValue("id"))
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	loc := s.cfg.App.Location()
	locale := adminFrom(r.Context()).Locale

	events := make([]map[string]any, 0, len(detail.Events))
	for _, e := range detail.Events {
		events = append(events, map[string]any{
			"at": e.CreatedAt.In(loc).Format("2006-01-02 15:04"), "status": e.Status,
			"error_code": e.ErrorCode, "model": e.Model,
			"input_tokens": e.InputTokens, "output_tokens": e.OutputTokens,
			"cost_usd": float64(e.CostMicros) / 1_000_000, "latency_ms": e.LatencyMS,
			"platform": e.Platform, "language": e.Language,
		})
	}
	devices := make([]map[string]any, 0, len(detail.Devices))
	for _, d := range detail.Devices {
		devices = append(devices, map[string]any{
			"id": d.ID, "platform": d.Platform, "app_version": d.AppVersion,
			"os_version": d.OSVersion, "push_enabled": d.PushOn,
			"last_seen": d.LastSeenAt.In(loc).Format("2006-01-02 15:04"),
		})
	}
	payments := make([]map[string]any, 0, len(detail.Payments))
	for _, p := range detail.Payments {
		payments = append(payments, map[string]any{
			"at":     p.CreatedAt.In(loc).Format("2006-01-02 15:04"),
			"amount": traits.FormatMoney(p.Amount, p.Currency), "status": p.Status,
			"provider": p.Provider,
		})
	}
	sessions := make([]map[string]any, 0, len(detail.Sessions))
	for _, session := range detail.Sessions {
		sessions = append(sessions, map[string]any{
			"id": session.ID, "device_id": session.DeviceID,
			"issued_at":  session.IssuedAt.In(loc).Format("2006-01-02 15:04"),
			"expires_at": session.ExpiresAt.In(loc).Format("2006-01-02 15:04"),
		})
	}

	subscription := map[string]any{"status": domain.SubActive}
	if sub := detail.Entitlement.Subscription; sub != nil {
		subscription["status"] = sub.Status
		subscription["source"] = sub.Source
		subscription["started_at"] = sub.StartedAt.In(loc).Format("2006-01-02")
		if sub.ExpiresAt != nil {
			subscription["expires_at"] = sub.ExpiresAt.In(loc).Format("2006-01-02")
		}
	}

	httpx.JSON(w, http.StatusOK, map[string]any{
		"user": map[string]any{
			"id": detail.User.ID, "identifier": maskIdentifier(detail.User),
			"status": detail.User.Status, "locale": detail.User.Locale,
			"platform": detail.User.Platform, "app_version": detail.User.AppVersion,
			"os_version":  detail.User.OSVersion,
			"created_at":  detail.User.CreatedAt.In(loc).Format("2006-01-02 15:04"),
			"last_active": optionalTime(detail.User.LastActiveAt, loc),
		},
		"profile": map[string]any{
			"role": detail.Profile.Role, "tone": detail.Profile.PreferredTone,
			"onboarding_completed": detail.Profile.OnboardingCompleted,
			"has_business":         detail.Profile.BusinessOffering != "" || detail.Profile.BusinessSummary != "",
		},
		"entitlement": map[string]any{
			"plan_id": detail.Entitlement.Plan.ID, "plan_code": detail.Entitlement.Plan.Code,
			"plan_name":   detail.Entitlement.Plan.LocalizedName(locale),
			"daily_limit": detail.Entitlement.DailyLimit, "used_today": detail.Entitlement.UsedToday,
			"monthly_limit": detail.Entitlement.MonthlyLimit, "used_month": detail.Entitlement.UsedMonth,
			"resets_at": detail.Entitlement.ResetsAt.In(loc).Format("2006-01-02 15:04"),
		},
		"subscription": subscription,
		"events":       events,
		"devices":      devices,
		"payments":     payments,
		"sessions":     sessions,
	})
}

func (s *Server) handleUserStatus(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Status string `json:"status"`
	}
	if err := httpx.Decode(w, r, 1024, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	if err := s.admin.SetUserStatus(r.Context(), adminFrom(r.Context()), s.ip(r),
		r.PathValue("id"), body.Status); err != nil {
		httpx.Fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleUserPlan(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PlanID    string `json:"plan_id"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := httpx.Decode(w, r, 2048, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	var expires *time.Time
	if raw := strings.TrimSpace(body.ExpiresAt); raw != "" {
		parsed, err := time.ParseInLocation("2006-01-02", raw, s.cfg.App.Location())
		if err != nil {
			httpx.Fail(w, domain.ErrInvalidRequest)
			return
		}
		end := parsed.Add(24*time.Hour - time.Second).UTC()
		expires = &end
	}
	if err := s.admin.AssignPlan(r.Context(), adminFrom(r.Context()), s.ip(r),
		r.PathValue("id"), body.PlanID, expires); err != nil {
		httpx.Fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleResetQuota(w http.ResponseWriter, r *http.Request) {
	if err := s.admin.ResetDailyQuota(r.Context(), adminFrom(r.Context()), s.ip(r), r.PathValue("id")); err != nil {
		httpx.Fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleRevokeSessions(w http.ResponseWriter, r *http.Request) {
	if err := s.admin.RevokeSessions(r.Context(), adminFrom(r.Context()), s.ip(r), r.PathValue("id")); err != nil {
		httpx.Fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func maskIdentifier(u domain.User) string {
	if u.Phone != "" {
		return traits.MaskIdentifier(u.Phone)
	}
	if u.Email != "" {
		return traits.MaskIdentifier(u.Email)
	}
	if u.LegacyClient != "" {
		return "legacy:" + domain.ShortID(u.LegacyClient)
	}
	return domain.ShortID(u.ID)
}

func optionalTime(t *time.Time, loc *time.Location) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return t.In(loc).Format("2006-01-02 15:04")
}

func (s *Server) fail(w http.ResponseWriter, err error) {
	s.log.Error("admin api failed", "error", err.Error())
	httpx.Error(w, http.StatusInternalServerError, httpx.CodeInternal, "Something went wrong.", nil)
}
