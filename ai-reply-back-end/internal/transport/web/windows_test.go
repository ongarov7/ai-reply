package web

import (
	"html"
	"html/template"
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

func TestWindowsDownloadExplainsPreviewAndRetainsLocale(t *testing.T) {
	const installer = "https://example.test/downloads/windows/AIReply-Setup-1.0.1.exe"
	bundle, err := localization.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, prefix := range []string{"", "/sandbox"} {
		for _, release := range []string{"", installer, "https://other.test/setup.exe"} {
			var cfg config.Config
			cfg.App.PublicBaseURL = "https://example.test" + prefix
			cfg.App.WindowsDownloadURL = release
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
				handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, prefix+"/windows?lang="+locale, nil))
				body := html.UnescapeString(w.Body.String())
				if w.Code != http.StatusOK || !strings.Contains(body, bundle.T(locale, "windows.title")) ||
					!strings.Contains(body, `href="`+prefix+`/payment?lang=`+locale+`"`) {
					t.Fatalf("Windows page missing for %s/%s: HTTP %d", prefix, locale, w.Code)
				}
				available := release == installer
				if strings.Contains(body, `href="`+installer+`"`) != available {
					t.Fatal("unvalidated Windows installer exposed")
				}
				if available {
					for _, key := range []string{"windows.preview", "windows.preview_body", "windows.signature", "windows.requirements", "windows.install_step3"} {
						translated := bundle.T(locale, key)
						if translated == key || !strings.Contains(body, translated) {
							t.Fatalf("missing truthful release information: %s/%s", locale, key)
						}
					}
					if strings.Index(body, bundle.T(locale, "windows.signature")) > strings.Index(body, `href="`+installer+`"`) {
						t.Fatal("unsigned notice appears only after the download")
					}
				} else if !strings.Contains(body, bundle.T(locale, "windows.soon")) {
					t.Fatal("unavailable installer is not explained")
				}
				for _, forbidden := range []string{"other.test", "account.js", "<form", "windows.title", "windows.signature"} {
					if strings.Contains(body, forbidden) {
						t.Fatalf("Windows page contains %q", forbidden)
					}
				}
				base := s.base(locale)
				base.CommercialPage = true
				landing := httptest.NewRecorder()
				s.render(landing, "landing", "public_layout", landingView{baseView: base, Boot: template.JS("{}"),
					WindowsDownloadURL: s.cfg.App.WindowsInstallerURL()})
				if landing.Code != http.StatusOK || strings.Contains(landing.Body.String(), `href="`+prefix+`/windows?lang=`+locale+`"`) != available {
					t.Fatalf("landing download availability/locale incorrect: %s/%s", prefix, locale)
				}
			}
		}
	}
}
