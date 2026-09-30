// Package middleware — сұраныс идентификаторы, журнал, қорғаныс, лимиттер.
package middleware

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/logging"
	"github.com/aireply/ai-reply-back-end/internal/reqctx"
	"github.com/aireply/ai-reply-back-end/internal/transport/httpx"
)

// Chain — миддлварьлерді біріктіреді.
func Chain(h http.Handler, middlewares ...func(http.Handler) http.Handler) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}
	return h
}

// RequestID — корреляция идентификаторы және клиент метадерегі.
//
// A valid client X-Request-ID is kept (so an app's error report names the
// same request as the server log); anything else is replaced by a fresh id.
// The sanitized client headers and an Observed holder for the access log go
// into the context.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		client := reqctx.Parse(r)
		w.Header().Set(reqctx.HeaderRequestID, client.RequestID)
		ctx := logging.WithRequestID(r.Context(), client.RequestID)
		ctx = reqctx.With(ctx, client)
		ctx = reqctx.WithObserved(ctx, &reqctx.Observed{})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type statusWriter struct {
	http.ResponseWriter
	status    int
	bytes     int
	errorCode string
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

// RecordErrorCode — httpx.Error клиентке кеткен кодты осы арқылы хабарлайды.
func (w *statusWriter) RecordErrorCode(code string) { w.errorCode = code }

// Unwrap — http.ResponseController және httpx үшін.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Logging — тек метадерек жазады: дене де, тақырып мәндері де емес.
func Logging(log *slog.Logger) func(http.Handler) http.Handler {
	return AccessLog(log, AccessLogOptions{})
}

// AccessLogOptions — кіру журналының қосымша тұтынушылары.
type AccessLogOptions struct {
	// RecordAPIError — an /api/ request answered with an error worth keeping
	// (5xx, and 4xx except the routine 401, 404 and 405).
	RecordAPIError func(domain.APIError)
	// TouchInstallation — an /api/ request from an app that named its
	// installation; the callee throttles the write.
	TouchInstallation func(reqctx.Client)
}

// AccessLog — құрылымдық кіру журналы: бір сұраныс — бір жол.
//
// Fields: method, route (the matched pattern, never a path full of ids),
// status, bytes, duration_ms, request_id, user_id, platform, app_version,
// app_build, installation (a short prefix) and the error code the client
// received. No body, no header value, no query string.
func AccessLog(log *slog.Logger, opts AccessLogOptions) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			sw := &statusWriter{ResponseWriter: w}
			next.ServeHTTP(sw, r)
			if sw.status == 0 {
				sw.status = http.StatusOK
			}
			duration := time.Since(started)
			client := reqctx.From(r.Context())
			userID := reqctx.ObservedFrom(r.Context()).User()
			route := routeOf(r)

			level := slog.LevelInfo
			switch {
			case sw.status >= 500:
				level = slog.LevelError
			case sw.status == http.StatusTooManyRequests:
				level = slog.LevelWarn
			}
			attrs := []any{
				"method", r.Method, "route", route, "status", sw.status, "bytes", sw.bytes,
				"duration_ms", duration.Milliseconds(),
			}
			if userID != "" {
				attrs = append(attrs, "user_id", userID)
			}
			if client.Platform != "" {
				attrs = append(attrs, "platform", client.Platform, "app_version", client.AppVersion,
					"app_build", client.AppBuild)
			}
			if client.InstallationID != "" {
				attrs = append(attrs, "installation", shortID(client.InstallationID))
			}
			if client.TraceID != "" {
				attrs = append(attrs, "trace_id", client.TraceID)
			}
			if sw.errorCode != "" {
				attrs = append(attrs, "error_code", sw.errorCode)
			}
			logging.FromContext(r.Context(), log).Log(r.Context(), level, "http", attrs...)

			if !strings.HasPrefix(r.URL.Path, "/api/") {
				return
			}
			if opts.TouchInstallation != nil && client.InstallationID != "" {
				opts.TouchInstallation(client)
			}
			if opts.RecordAPIError != nil && sw.status >= 400 && sw.status != http.StatusUnauthorized &&
				sw.status != http.StatusNotFound && sw.status != http.StatusMethodNotAllowed {
				opts.RecordAPIError(domain.APIError{
					RequestID: client.RequestID, TraceID: client.TraceID, UserID: userID,
					InstallationID: client.InstallationID, SessionID: client.SessionID,
					Platform: client.Platform, AppVersion: client.AppVersion, AppBuild: client.AppBuild,
					OSVersion: client.OSVersion, Method: r.Method, Route: route, StatusCode: sw.status,
					ErrorCode: sw.errorCode, DurationMS: int(duration.Milliseconds()), OccurredAt: started.UTC(),
				})
			}
		})
	}
}

// routeOf — сәйкескен маршрут үлгісі ("POST /api/v1/devices/{id}"), болмаса жол үлгісі.
func routeOf(r *http.Request) string {
	if r.Pattern != "" {
		if i := strings.IndexByte(r.Pattern, ' '); i >= 0 {
			return r.Pattern[i+1:]
		}
		return r.Pattern
	}
	// Unmatched paths (404/405) are not stored; keep the log line short and
	// free of anything a scanner put in the URL.
	path := r.URL.Path
	if len(path) > 64 {
		path = path[:64]
	}
	return path
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// Recover — панинканы 500-ге айналдырады, стек журналда қалады, клиентке кетпейді.
func Recover(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logging.FromContext(r.Context(), log).Error("panic recovered",
						"path", r.URL.Path, "panic", rec)
					httpx.Error(w, http.StatusInternalServerError, httpx.CodeInternal, "Something went wrong.", nil)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// SecurityHeaders — базалық қорғаныс тақырыптары.
func SecurityHeaders(production bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			// Микрофон тек симуляторда ашылады: Android демонстрациясындағы
			// дауыспен нұсқау беру браузердің өз танушысын пайдаланады, дәл
			// қосымшадағыдай — құрылғының өзінде. Қалған беттерде жабық.
			permissions := "camera=(), microphone=(), geolocation=()"
			if strings.HasPrefix(r.URL.Path, "/simulator") {
				permissions = "camera=(), microphone=(self), geolocation=()"
			}
			h.Set("Permissions-Policy", permissions)
			// Барлық стиль мен скрипт өз доменімізден: сыртқы CDN жоқ.
			//
			// The admin panel and the product simulator get one extra source,
			// and only they: Vue's runtime
			// template compiler builds render functions with `new Function`,
			// which 'unsafe-eval' is what permits. The trade is deliberate and
			// contained — the landing page, the mobile API and everything a
			// signed-out visitor can reach keep the strict policy, and the
			// admin panel and the simulator are same-origin, behind a session,
			// and render every value through Vue's escaped interpolation rather
			// than raw HTML.
			// Precompiling the templates at build time removes this line.
			script := "script-src 'self'"
			if strings.HasPrefix(r.URL.Path, "/admin") || strings.HasPrefix(r.URL.Path, "/simulator") {
				script = "script-src 'self' 'unsafe-eval'"
			}
			h.Set("Content-Security-Policy",
				"default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; "+
					script+"; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'")
			if production {
				h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// CORS — мобильді клиентке қажет емес, бірақ веб-клиент үшін баптаулы.
func CORS(origins []string) func(http.Handler) http.Handler {
	allowed := map[string]bool{}
	for _, o := range origins {
		allowed[strings.TrimRight(o, "/")] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := strings.TrimRight(r.Header.Get("Origin"), "/")
			if origin != "" && allowed[origin] {
				h := w.Header()
				h.Set("Access-Control-Allow-Origin", origin)
				h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-ID, Idempotency-Key, "+
					"X-Platform, X-App-Version, X-App-Build, X-OS-Version, X-Installation-ID, X-Session-ID")
				h.Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
				h.Set("Vary", "Origin")
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
