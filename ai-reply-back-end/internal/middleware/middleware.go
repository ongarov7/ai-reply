// Package middleware — сұраныс идентификаторы, журнал, қорғаныс, лимиттер.
package middleware

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/logging"
	"github.com/aireply/ai-reply-back-end/internal/traits"
	"github.com/aireply/ai-reply-back-end/internal/transport/httpx"
)

// Chain — миддлварьлерді біріктіреді.
func Chain(h http.Handler, middlewares ...func(http.Handler) http.Handler) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}
	return h
}

// RequestID — әр сұранысқа корреляция идентификаторы.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = traits.RandomToken(8)
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(logging.WithRequestID(r.Context(), id)))
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
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

// Logging — тек метадерек жазады: дене де, тақырып мәндері де емес.
func Logging(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			sw := &statusWriter{ResponseWriter: w}
			next.ServeHTTP(sw, r)
			if sw.status == 0 {
				sw.status = http.StatusOK
			}
			logging.FromContext(r.Context(), log).Info("http",
				"method", r.Method,
				"path", r.URL.Path,
				"status", sw.status,
				"bytes", sw.bytes,
				"duration_ms", time.Since(started).Milliseconds(),
			)
		})
	}
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
				h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-ID")
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
