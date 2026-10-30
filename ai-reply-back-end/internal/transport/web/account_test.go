package web

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/config"
	"github.com/aireply/ai-reply-back-end/internal/catalog"
	"github.com/aireply/ai-reply-back-end/internal/database"
	"github.com/aireply/ai-reply-back-end/internal/localization"
	"github.com/aireply/ai-reply-back-end/internal/middleware"
	"github.com/aireply/ai-reply-back-end/internal/traits"
	"github.com/aireply/ai-reply-back-end/migrations"
)

func accountWebFixture(t *testing.T, prefix string) (*Server, *catalog.Service) {
	t.Helper()
	db, err := database.Open(database.Options{Path: filepath.Join(t.TempDir(), "web-account.sqlite")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := database.Migrate(context.Background(), db, migrations.FS); err != nil {
		t.Fatal(err)
	}
	prices := catalog.New(db)
	if err := prices.EnsureInitial(context.Background()); err != nil {
		t.Fatal(err)
	}
	bundle, err := localization.Load()
	if err != nil {
		t.Fatal(err)
	}
	var cfg config.Config
	cfg.App.Env = "test"
	cfg.App.PublicBaseURL = "https://example.test" + prefix
	cfg.Auth.OTPTTL = 5 * time.Minute
	s, err := New(Deps{Config: cfg, Catalog: prices, Bundle: bundle,
		Limiter: middleware.NewLimiter(), Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	return s, prices
}

func TestPublicPaymentPageShowsPublishedPackBeforeSignIn(t *testing.T) {
	for _, prefix := range []string{"", "/sandbox"} {
		s, prices := accountWebFixture(t, prefix)
		mux := http.NewServeMux()
		s.Register(mux)
		var handler http.Handler = mux
		if prefix != "" {
			handler = http.StripPrefix(prefix, mux)
		}
		for _, locale := range []string{"ru", "kk", "en", "uz"} {
			t.Run(prefix+"/"+locale, func(t *testing.T) {
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, prefix+"/payment?lang="+locale, nil))
				if w.Code != http.StatusOK {
					t.Fatalf("status = %d: %s", w.Code, w.Body.String())
				}
				body := w.Body.String()
				for _, want := range []string{`data-public-offer="pack30"`, `<strong data-offer-units>30</strong>`, traits.FormatMoney(10000, "KZT"),
					`href="` + prefix + `/static/account.css?v=`, `src="` + prefix + `/static/account.js?v=`, `"base_path":"` + prefix + `"`,
					`id="account-purchase-terms" type="checkbox"`, `id="account-login" class="account-card account-login" hidden`} {
					if !strings.Contains(body, want) {
						t.Fatalf("payment page missing %q", want)
					}
				}
				if strings.Index(body, `data-public-offer="pack30"`) > strings.Index(body, `id="account-login"`) {
					t.Fatal("public offer is hidden behind sign-in")
				}
				if w.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("account shell must not be cached")
				}
			})
		}
		// Public pricing follows a newly published snapshot without a UI release.
		products := catalog.InitialProducts()
		products[len(products)-1].PriceMinor = 20000
		draft, err := prices.SaveDraft(context.Background(), "fixture-admin", 0, products)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := prices.Publish(context.Background(), "fixture-admin", draft.ID, draft.Revision); err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, prefix+"/payment?lang=en", nil))
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), traits.FormatMoney(20000, "KZT")) {
			t.Fatal("published price was not reflected in the public payment offer")
		}
	}
}

func TestPublicInformationPagesKeepNavigationAwayFromCheckout(t *testing.T) {
	s, _ := accountWebFixture(t, "/sandbox")
	for name, handler := range map[string]http.HandlerFunc{
		"privacy": s.handlePrivacy, "offer": s.handleTerms, "support": s.handleSupport, "account/delete": s.handleAccountDelete,
	} {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			handler(w, httptest.NewRequest(http.MethodGet, "/"+name+"?lang=en", nil))
			if w.Code != http.StatusOK {
				t.Fatal(w.Code)
			}
			body := w.Body.String()
			for _, forbidden := range []string{`href="/sandbox/payment`, `href="/sandbox/account?`, `href="/sandbox/#pricing`, `href="/?lang=`, `href="/sandbox/?lang=`} {
				if strings.Contains(body, forbidden) {
					t.Fatalf("informational page links to purchasing surface: %s", forbidden)
				}
			}
			if !strings.Contains(body, `href="/sandbox/privacy?lang=en"`) || !strings.Contains(body, `src="/sandbox/static/landing.js"`) {
				t.Fatal("informational page lost its isolated prefix")
			}
			if name == "account/delete" && !strings.Contains(body, `"base_path":"/sandbox"`) {
				t.Fatal("deletion requests would not preserve sandbox isolation")
			}
		})
	}
}
