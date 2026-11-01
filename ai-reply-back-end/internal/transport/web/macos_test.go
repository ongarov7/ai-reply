package web

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aireply/ai-reply-back-end/config"
	"github.com/aireply/ai-reply-back-end/internal/localization"
	"github.com/aireply/ai-reply-back-end/internal/middleware"
)

func TestMacInformationDownloadAndAccount(t *testing.T) {
	const installer = "https://example.test/downloads/macos/AI-Reply-1.0.0.dmg"
	bundle, err := localization.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, prefix := range []string{"", "/sandbox"} {
		for _, release := range []string{"", installer, "https://evil.test/untrusted.dmg"} {
			var cfg config.Config
			cfg.App.PublicBaseURL = "https://example.test" + prefix
			cfg.App.MacOSDownloadURL = release
			s, err := New(Deps{Config: cfg, Bundle: bundle, Limiter: middleware.NewLimiter(),
				Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
			if err != nil {
				t.Fatal(err)
			}
			mux := http.NewServeMux()
			s.Register(mux)
			var handler http.Handler = mux
			if prefix != "" {
				handler = http.StripPrefix(prefix, mux)
			}
			for _, locale := range []string{"ru", "kk", "en", "uz"} {
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, prefix+"/mac?lang="+locale, nil))
				body := w.Body.String()
				if w.Code != http.StatusOK || !strings.Contains(body, bundle.T(locale, "mac.title")) ||
					!strings.Contains(body, `href="`+prefix+`/payment?lang=`+locale+`"`) {
					t.Fatalf("Mac information missing for %s/%s: HTTP %d", prefix, locale, w.Code)
				}
				if strings.Contains(body, `href="`+installer+`"`) != (release == installer) {
					t.Fatal("installer availability differs from the validated release setting")
				}
				for _, forbidden := range []string{"mac.title", "mac.account_body", "evil.test", "account.js", "<form"} {
					if strings.Contains(body, forbidden) {
						t.Fatalf("Mac page contains %q", forbidden)
					}
				}
			}
		}
	}
}
