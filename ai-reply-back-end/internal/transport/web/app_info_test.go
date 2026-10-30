package web

import (
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/aireply/ai-reply-back-end/config"
	"github.com/aireply/ai-reply-back-end/internal/localization"
	"github.com/aireply/ai-reply-back-end/internal/middleware"
)

func appInfoFixture(t *testing.T, prefix, storeURL string) http.Handler {
	t.Helper()
	bundle, err := localization.Load()
	if err != nil {
		t.Fatal(err)
	}
	var cfg config.Config
	cfg.App.Env = "test"
	cfg.App.PublicBaseURL = "https://example.test" + prefix
	cfg.App.Contact = "help@example.test"
	cfg.App.AppStoreURL = storeURL
	// This page must work without plans, billing, account services or a database.
	s, err := New(Deps{Config: cfg, Bundle: bundle, Limiter: middleware.NewLimiter(),
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.Register(mux)
	if prefix != "" {
		return http.StripPrefix(prefix, mux)
	}
	return mux
}

func TestAppInformationRouteInAllLocalesAndPrefixes(t *testing.T) {
	const storeURL = "https://apps.apple.com/kz/app/id123456789"
	for _, prefix := range []string{"", "/sandbox"} {
		handler := appInfoFixture(t, prefix, storeURL)
		for locale, title := range map[string]string{
			"ru": "AI Reply для iPhone", "kk": "iPhone үшін AI Reply",
			"en": "AI Reply for iPhone", "uz": "iPhone uchun AI Reply",
		} {
			t.Run(prefix+"/"+locale, func(t *testing.T) {
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, prefix+"/app?lang="+locale, nil))
				if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "text/html; charset=utf-8" {
					t.Fatalf("response = %d %q", w.Code, w.Header().Get("Content-Type"))
				}
				body := w.Body.String()
				for _, want := range []string{`<html lang="` + locale + `">`, "<h1>" + title + "</h1>",
					`href="` + prefix + `/support?lang=` + locale + `"`, `href="` + prefix + `/privacy?lang=` + locale + `"`,
					`href="` + prefix + `/offer?lang=` + locale + `"`, `href="mailto:help@example.test"`,
					`href="` + storeURL + `"`, `src="` + prefix + `/static/landing.js"`} {
					if !strings.Contains(body, want) {
						t.Errorf("page missing %q", want)
					}
				}
				for _, forbidden := range []string{"app_info.", "account.js", "account-data", "/account", "/payment", "#pricing", "/api/", "<form"} {
					if strings.Contains(body, forbidden) {
						t.Errorf("information page contains %q", forbidden)
					}
				}
				// Whitelist every rendered link, including the shared header/footer.
				for _, match := range regexp.MustCompile(`href="([^"]+)"`).FindAllStringSubmatch(body, -1) {
					link := html.UnescapeString(match[1])
					allowed := link == storeURL || link == "mailto:help@example.test" ||
						link == prefix+"/static/landing.css" || link == prefix+"/static/favicon.svg" ||
						strings.HasPrefix(link, "?lang=")
					for _, page := range []string{"support", "privacy", "offer"} {
						allowed = allowed || link == prefix+"/"+page+"?lang="+locale
					}
					if !allowed {
						t.Errorf("unexpected destination %q", link)
					}
				}
			})
		}
	}
}

func TestAppInformationDoesNotUseBrowserAccountOrPurchaseParameters(t *testing.T) {
	handler := appInfoFixture(t, "/sandbox", "")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/sandbox/app?lang=en", nil))
	r := httptest.NewRequest(http.MethodGet, "/sandbox/app?lang=en&order=untrusted&handoff=untrusted", nil)
	r.AddCookie(&http.Cookie{Name: "aireply_web_session", Value: "untrusted"})
	r.Header.Set("User-Agent", "AIReply/1.2.0")
	other := httptest.NewRecorder()
	handler.ServeHTTP(other, r)
	if other.Code != http.StatusOK || other.Body.String() != w.Body.String() {
		t.Fatal("information changed with account state or purchase parameters")
	}
	for _, cookie := range other.Result().Cookies() {
		if cookie.Name != "lang" {
			t.Fatalf("information page issued account cookie %q", cookie.Name)
		}
	}
	post := httptest.NewRecorder()
	handler.ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/sandbox/app", nil))
	if post.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /app = %d, want 405", post.Code)
	}
}

func TestAppInformationDownloadOnlyLinksToAppStore(t *testing.T) {
	for _, destination := range []string{"", "https://example.test/payment", "https://apps.apple.com.evil.test/app/id1", "https://user@apps.apple.com/app/id1", "javascript:alert(1)"} {
		t.Run(destination, func(t *testing.T) {
			handler := appInfoFixture(t, "", destination)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/app?lang=en", nil))
			if w.Code != http.StatusOK || strings.Contains(w.Body.String(), `class="btn btn-primary"`) {
				t.Fatalf("non-App-Store destination offered: %q", destination)
			}
		})
	}
}
