// Package api — мобильді клиентке арналған REST қабаты (/api/v1 және ескі /v1).
package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/transport/httpx"
)

type ctxKey string

const (
	userKey     ctxKey = "user"
	deviceKey   ctxKey = "device"
	platformKey ctxKey = "platform"
)

// WithUser — контекстке қолданушыны салады.
func WithUser(ctx context.Context, u domain.User) context.Context {
	return context.WithValue(ctx, userKey, u)
}

// UserFrom — контекстен қолданушы.
func UserFrom(ctx context.Context) (domain.User, bool) {
	u, ok := ctx.Value(userKey).(domain.User)
	return u, ok
}

// DeviceFrom — токендегі құрылғы идентификаторы.
func DeviceFrom(ctx context.Context) string {
	v, _ := ctx.Value(deviceKey).(string)
	return v
}

// bearer — Authorization тақырыбынан токен.
func bearer(r *http.Request) string {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if header == "" {
		return ""
	}
	if !strings.HasPrefix(strings.ToLower(header), "bearer ") {
		return ""
	}
	return strings.TrimSpace(header[7:])
}

// requireUser — access токенді тексеретін миддлварь.
func (s *Server) requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearer(r)
		if token == "" {
			httpx.Error(w, http.StatusUnauthorized, httpx.CodeUnauthorized, "Authentication is required.", nil)
			return
		}
		user, claims, err := s.auth.Authenticate(r.Context(), token)
		if err != nil {
			httpx.Fail(w, err)
			return
		}
		ctx := WithUser(r.Context(), user)
		ctx = context.WithValue(ctx, deviceKey, claims.DeviceID)
		ctx = context.WithValue(ctx, platformKey, claims.Platform)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// optionalUser — токен болса тексереді, болмаса сұраныс анонимді өтеді.
//
// A token that is present but no longer valid is refused with 401 rather
// than treated as anonymous: for installation registration "anonymous"
// means "signed out" and detaches the device, which an expired access token
// must never do. The app refreshes and repeats the call.
func (s *Server) optionalUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimSpace(r.Header.Get("Authorization")) == "" {
			next.ServeHTTP(w, r)
			return
		}
		s.requireUser(next).ServeHTTP(w, r)
	})
}

// requireConsent — AI сұраныстары тек ағымдағы шарттарға келісім болса өтеді.
//
// Runs after requireUser. Without a consent row for the current terms and
// privacy versions (none yet, an older version, or withdrawn) the request is
// refused with 403 CONSENT_REQUIRED before any text reaches the provider; the
// app returns to its consent screen and posts /api/v1/me/consents.
func (s *Server) requireConsent(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, _ := UserFrom(r.Context())
		if err := s.users.RequireConsent(r.Context(), user.ID); err != nil {
			httpx.Fail(w, err)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireLegacy — ескі install-token миддлварі.
func (s *Server) requireLegacy(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearer(r)
		if token == "" {
			httpx.Error(w, http.StatusUnauthorized, httpx.CodeUnauthorized, "Authentication is required.", nil)
			return
		}
		user, err := s.auth.AuthenticateLegacy(r.Context(), token)
		if err != nil {
			// Жаңа access токенмен де жұмыс істей берсін (біртіндеп көшу).
			if u, claims, err2 := s.auth.Authenticate(r.Context(), token); err2 == nil {
				ctx := context.WithValue(WithUser(r.Context(), u), deviceKey, claims.DeviceID)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			httpx.Fail(w, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), user)))
	})
}
