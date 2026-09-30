package adminapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/admin"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/redact"
	"github.com/aireply/ai-reply-back-end/internal/repository"
	"github.com/aireply/ai-reply-back-end/internal/telemetry"
	"github.com/aireply/ai-reply-back-end/internal/traits"
	"github.com/aireply/ai-reply-back-end/internal/transport/httpx"
)

// Диагностика: қолданушы, оқиғалар, кіру журналы, API қателері, нұсқалар, операциялық тақта.
//
// Nothing here returns a secret: no token (a fingerprint at most), no code,
// no password hash, no message text. IP addresses are shortened. Full e-mail
// and phone appear only in a person's diagnostics, which needs its own
// permission and leaves an audit record.

func (s *Server) registerDiagnostics(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/admin/users/{id}/diagnostics", s.can(admin.PermDiagnosticsRead, s.handleUserDiagnostics))
	mux.Handle("GET /api/v1/admin/logs/events", s.can(admin.PermLogsRead, s.handleLogEvents))
	mux.Handle("GET /api/v1/admin/logs/auth", s.can(admin.PermLogsRead, s.handleLogAuth))
	mux.Handle("GET /api/v1/admin/logs/errors", s.can(admin.PermLogsRead, s.handleLogErrors))
	mux.Handle("GET /api/v1/admin/logs/versions", s.can(admin.PermLogsRead, s.handleVersions))
	mux.Handle("GET /api/v1/admin/logs/meta", s.can(admin.PermLogsRead, s.handleLogMeta))
	mux.Handle("GET /api/v1/admin/ops", s.can(admin.PermDashboardRead, s.handleOps))
}

func (s *Server) handleUserDiagnostics(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	d, err := s.admin.UserDiagnostics(r.Context(), userID)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	adminUser := adminFrom(r.Context())
	// Opening a person's diagnostics shows their e-mail, phone and devices:
	// who looked, and when, is kept.
	s.admin.Audit(r.Context(), adminUser, s.ip(r), "user.diagnostics.view", "user", userID, nil)

	loc := s.cfg.App.Location()
	installations := make([]map[string]any, 0, len(d.Installations))
	for _, i := range d.Installations {
		installations = append(installations, s.installationDTO(i))
	}
	sessions := make([]map[string]any, 0, len(d.Sessions))
	for _, sess := range d.Sessions {
		sessions = append(sessions, map[string]any{
			"session_id": domain.ShortID(sess.SessionID), "installation_id": domain.ShortID(sess.InstallationID),
			"platform": sess.Platform, "app_version": sess.AppVersion, "app_build": sess.AppBuild,
			"os_version": sess.OSVersion, "device": admin.DeviceName(sess.Platform, "", sess.DeviceModel),
			"started_at":       sess.StartedAt.In(loc).Format("2006-01-02 15:04"),
			"last_activity_at": sess.LastActivityAt.In(loc).Format("2006-01-02 15:04"),
			"ended_at":         optionalTime(sess.EndedAt, loc), "events": sess.EventCount,
			"duration_s": int(sess.LastActivityAt.Sub(sess.StartedAt).Seconds()),
		})
	}
	deliveries := make([]map[string]any, 0, len(d.Deliveries))
	for _, row := range d.Deliveries {
		deliveries = append(deliveries, s.deliveryDTO(row))
	}
	subscription := map[string]any{"plan_code": d.Entitlement.Plan.Code,
		"plan_name": d.Entitlement.Plan.LocalizedName(adminUser.Locale), "paid": !d.Entitlement.Plan.IsFree}
	if sub := d.Entitlement.Subscription; sub != nil {
		subscription["status"] = sub.Status
		if sub.ExpiresAt != nil {
			subscription["expires_at"] = sub.ExpiresAt.In(loc).Format("2006-01-02")
		}
	}
	methods := d.SignInMethods
	if methods == nil {
		methods = []string{}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"user": map[string]any{
			"id": d.User.ID, "email": d.User.Email, "phone": d.User.Phone, "status": d.User.Status,
			"locale": d.User.Locale, "timezone": d.User.Timezone, "sign_in_methods": methods,
			"created_at":  d.User.CreatedAt.In(loc).Format("2006-01-02 15:04"),
			"last_active": optionalTime(d.User.LastActiveAt, loc),
		},
		"subscription":  subscription,
		"installations": installations,
		"sessions":      sessions,
		"auth_events":   s.authEventDTOs(d.AuthEvents),
		"api_errors":    s.apiErrorDTOs(d.APIErrors),
		"app_errors":    s.appEventDTOs(d.AppErrors),
		"notifications": deliveries,
	})
}

// logFilter — ортақ сүзгі параметрлері: ?user= (id, пошта, телефон), ?from=&to= (YYYY-MM-DD).
func (s *Server) logFilter(r *http.Request) (admin.UserQuery, repository.TimeRange, error) {
	query := r.URL.Query()
	userQuery, err := s.admin.ResolveUser(r.Context(), traits.Clamp(query.Get("user"), 120))
	if err != nil {
		return userQuery, repository.TimeRange{}, err
	}
	var rng repository.TimeRange
	loc := s.cfg.App.Location()
	if v := query.Get("from"); v != "" {
		t, err := time.ParseInLocation("2006-01-02", v, loc)
		if err != nil {
			return userQuery, rng, domain.InvalidField("from", "use YYYY-MM-DD")
		}
		rng.From = t
	}
	if v := query.Get("to"); v != "" {
		t, err := time.ParseInLocation("2006-01-02", v, loc)
		if err != nil {
			return userQuery, rng, domain.InvalidField("to", "use YYYY-MM-DD")
		}
		rng.To = t.AddDate(0, 0, 1)
	}
	return userQuery, rng, nil
}

func (s *Server) handleLogEvents(w http.ResponseWriter, r *http.Request) {
	userQuery, rng, err := s.logFilter(r)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	query := r.URL.Query()
	outcome := query.Get("outcome")
	if outcome != "" && outcome != domain.OutcomeSuccess && outcome != domain.OutcomeFailure {
		httpx.Fail(w, domain.InvalidField("outcome", "success or failure"))
		return
	}
	page, limit := pageParams(r, 50)
	events, total, err := s.admin.AppEvents(r.Context(), repository.EventFilter{
		UserIDs: userQuery.IDs, InstallationID: traits.Clamp(query.Get("installation_id"), 64),
		Name: traits.Clamp(query.Get("name"), 64), Platform: traits.Clamp(query.Get("platform"), 16),
		AppVersion: traits.Clamp(query.Get("app_version"), 32), AppBuild: traits.Clamp(query.Get("app_build"), 32),
		OSVersion: traits.Clamp(query.Get("os_version"), 32), DeviceModel: traits.Clamp(query.Get("device_model"), 64),
		Outcome: outcome, Range: rng, Page: traits.NewPage(limit, (page-1)*limit),
	})
	if err != nil {
		s.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"events": s.appEventDTOs(events), "total": total, "page": page, "limit": limit,
	})
}

func (s *Server) handleLogAuth(w http.ResponseWriter, r *http.Request) {
	userQuery, rng, err := s.logFilter(r)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	query := r.URL.Query()
	page, limit := pageParams(r, 50)
	events, total, err := s.admin.AuthEvents(r.Context(), repository.AuthEventFilter{
		UserIDs: userQuery.IDs, SubjectHash: userQuery.SubjectHash,
		Name: traits.Clamp(query.Get("name"), 64), Method: traits.Clamp(query.Get("method"), 16),
		Outcome: traits.Clamp(query.Get("outcome"), 16), Range: rng, Page: traits.NewPage(limit, (page-1)*limit),
	})
	if err != nil {
		s.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"events": s.authEventDTOs(events), "total": total, "page": page, "limit": limit,
	})
}

func (s *Server) handleLogErrors(w http.ResponseWriter, r *http.Request) {
	userQuery, rng, err := s.logFilter(r)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	query := r.URL.Query()
	status, _ := strconv.Atoi(query.Get("status"))
	page, limit := pageParams(r, 50)
	errs, total, err := s.admin.APIErrors(r.Context(), repository.APIErrorFilter{
		UserIDs: userQuery.IDs, Platform: traits.Clamp(query.Get("platform"), 16),
		AppVersion: traits.Clamp(query.Get("app_version"), 32), AppBuild: traits.Clamp(query.Get("app_build"), 32),
		Route: traits.Clamp(query.Get("route"), 128), StatusCode: status,
		RequestID: traits.Clamp(query.Get("request_id"), 64), Range: rng, Page: traits.NewPage(limit, (page-1)*limit),
	})
	if err != nil {
		s.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"errors": s.apiErrorDTOs(errs), "total": total, "page": page, "limit": limit,
	})
}

func (s *Server) handleVersions(w http.ResponseWriter, r *http.Request) {
	rows, err := s.admin.Versions(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	loc := s.cfg.App.Location()
	out := make([]map[string]any, 0, len(rows))
	for _, v := range rows {
		last := ""
		if !v.LastSeenAt.IsZero() && v.LastSeenAt.Unix() > 0 {
			last = v.LastSeenAt.In(loc).Format("2006-01-02 15:04")
		}
		out = append(out, map[string]any{
			"platform": v.Platform, "app_version": v.AppVersion, "app_build": v.AppBuild,
			"installations": v.Installations, "active_30d": v.Active30d, "push_reachable": v.PushReachable,
			"api_errors_7d": v.APIErrors7d, "push_registration_failures_7d": v.PushRegFailures7d,
			"last_seen": last,
		})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"versions": out})
}

func (s *Server) handleLogMeta(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]any{
		"app_events":  telemetry.EventNames(),
		"auth_events": domain.AuthEventNames,
		"retention": map[string]int{
			"app_events_days":    s.cfg.Telemetry.RetentionAppEventsDays,
			"api_errors_days":    s.cfg.Telemetry.RetentionAPIErrorsDays,
			"auth_events_days":   s.cfg.Telemetry.RetentionAuthEventsDays,
			"notifications_days": s.cfg.Telemetry.RetentionDeliveriesDays,
			"audit_log_days":     s.cfg.Telemetry.RetentionAuditLogDays,
		},
	})
}

func (s *Server) handleOps(w http.ResponseWriter, r *http.Request) {
	view, err := s.admin.Ops(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"summary":       view.Summary,
		"app_versions":  view.AppVersions,
		"os_versions":   view.OSVersions,
		"recent_errors": s.apiErrorDTOs(view.RecentErrors),
		"push":          s.notify.Status(),
	})
}

// ---------------------------------------------------------------- DTOs

func (s *Server) appEventDTOs(events []domain.AppEvent) []map[string]any {
	loc := s.cfg.App.Location()
	out := make([]map[string]any, 0, len(events))
	for _, e := range events {
		props := e.Properties
		if props == nil {
			props = map[string]any{}
		}
		out = append(out, map[string]any{
			"id": e.ID, "name": e.Name, "user_id": e.UserID, "installation_id": domain.ShortID(e.InstallationID),
			"session_id": domain.ShortID(e.SessionID), "platform": e.Platform, "app_version": e.AppVersion,
			"app_build": e.AppBuild, "os_version": e.OSVersion,
			"device": admin.DeviceName(e.Platform, "", e.DeviceModel), "device_model": e.DeviceModel,
			"outcome": e.Outcome, "error_code": e.ErrorCode, "request_id": e.RequestID,
			"properties":  redact.Map(props),
			"occurred_at": e.OccurredAt.In(loc).Format("2006-01-02 15:04:05"),
		})
	}
	return out
}

func (s *Server) authEventDTOs(events []domain.AuthEvent) []map[string]any {
	loc := s.cfg.App.Location()
	out := make([]map[string]any, 0, len(events))
	for _, e := range events {
		out = append(out, map[string]any{
			"id": e.ID, "name": e.Name, "method": e.Method, "outcome": e.Outcome, "error_code": e.ErrorCode,
			"user_id": e.UserID, "installation_id": domain.ShortID(e.InstallationID), "platform": e.Platform,
			"app_version": e.AppVersion, "app_build": e.AppBuild, "os_version": e.OSVersion,
			"ip": redact.IP(e.IP), "request_id": e.RequestID,
			"at": e.CreatedAt.In(loc).Format("2006-01-02 15:04:05"),
		})
	}
	return out
}

func (s *Server) apiErrorDTOs(errs []domain.APIError) []map[string]any {
	loc := s.cfg.App.Location()
	out := make([]map[string]any, 0, len(errs))
	for _, e := range errs {
		out = append(out, map[string]any{
			"id": e.ID, "request_id": e.RequestID, "trace_id": e.TraceID, "user_id": e.UserID,
			"installation_id": domain.ShortID(e.InstallationID), "platform": e.Platform,
			"app_version": e.AppVersion, "app_build": e.AppBuild, "os_version": e.OSVersion,
			"method": e.Method, "route": e.Route, "status": e.StatusCode, "error_code": e.ErrorCode,
			"duration_ms": e.DurationMS,
			"at":          e.OccurredAt.In(loc).Format("2006-01-02 15:04:05"),
		})
	}
	return out
}
