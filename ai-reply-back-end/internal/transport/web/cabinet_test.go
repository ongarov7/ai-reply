package web

import (
	"encoding/json"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/aireply/ai-reply-back-end/config"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/localization"
	"github.com/aireply/ai-reply-back-end/internal/middleware"
)

func cabinetHandler(t *testing.T, prefix string, configure func(*config.Config)) (http.Handler, *localization.Bundle) {
	t.Helper()
	bundle, err := localization.Load()
	if err != nil {
		t.Fatal(err)
	}
	var cfg config.Config
	cfg.App.Env = "test"
	cfg.App.PublicBaseURL = "https://example.test" + prefix
	if configure != nil {
		configure(&cfg)
	}
	s, err := New(Deps{Config: cfg, Bundle: bundle, Limiter: middleware.NewLimiter(), Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.Register(mux)
	if prefix != "" {
		return http.StripPrefix(prefix, mux), bundle
	}
	return mux, bundle
}

func cabinetPage(t *testing.T, handler http.Handler, path string) string {
	t.Helper()
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("%s: HTTP %d", path, w.Code)
	}
	return w.Body.String()
}

func staticSource(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("static", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// scriptIDs — the ids a script reads through its "['a', 'b'].forEach(… getElementById(prefix + id))" list.
func scriptIDs(t *testing.T, source, prefix string) []string {
	t.Helper()
	m := regexp.MustCompile(`\[((?:'[a-z-]+',?\s*)+)\]\.forEach\(function \(id\) \{\s*\w+\[id\] = document\.getElementById\('` + prefix + `' \+ id\)`).FindStringSubmatch(source)
	if m == nil {
		t.Fatalf("no %s* id list found", prefix)
	}
	var ids []string
	for _, item := range regexp.MustCompile(`'([a-z-]+)'`).FindAllStringSubmatch(m[1], -1) {
		ids = append(ids, item[1])
	}
	return ids
}

// Every element account.js and cabinet.js look up is rendered on both pages,
// in every locale and under the sandbox prefix.
func TestCabinetShellRendersEveryElementTheScriptsUse(t *testing.T) {
	accountIDs := scriptIDs(t, staticSource(t, "account.js"), "account-")
	cabinetIDs := scriptIDs(t, staticSource(t, "cabinet.js"), "cabinet-")
	if len(accountIDs) < 40 || len(cabinetIDs) < 50 {
		t.Fatalf("suspiciously few ids: account %d, cabinet %d", len(accountIDs), len(cabinetIDs))
	}
	for _, prefix := range []string{"", "/sandbox"} {
		handler, _ := cabinetHandler(t, prefix, nil)
		for _, page := range []string{"/account", "/payment"} {
			for _, locale := range domain.Locales {
				body := cabinetPage(t, handler, prefix+page+"?lang="+locale)
				var missing []string
				for _, id := range accountIDs {
					if !strings.Contains(body, `id="account-`+id+`"`) {
						missing = append(missing, "account-"+id)
					}
				}
				for _, id := range cabinetIDs {
					if !strings.Contains(body, `id="cabinet-`+id+`"`) {
						missing = append(missing, "cabinet-"+id)
					}
				}
				for _, key := range accountRecipients {
					for _, suffix := range []string{"", "-count", "-error", "-cost", "-voice"} {
						if id := "cabinet-notes-" + key + suffix; !strings.Contains(body, `id="`+id+`"`) {
							missing = append(missing, id)
						}
					}
				}
				for _, match := range regexp.MustCompile(`data-mic-for="([^"]+)"`).FindAllStringSubmatch(body, -1) {
					if !strings.Contains(body, `<textarea id="`+match[1]+`"`) || !strings.Contains(body, `id="`+match[1]+`-voice"`) {
						missing = append(missing, "microphone target "+match[1])
					}
				}
				if len(missing) > 0 {
					t.Fatalf("%s%s?lang=%s is missing %v", prefix, page, locale, missing)
				}
				for _, section := range []string{"overview", "profile", "recipients", "billing", "apps"} {
					if !strings.Contains(body, `href="#`+section+`" data-section-link="`+section+`"`) ||
						!strings.Contains(body, `data-section="`+section+`"`) {
						t.Fatalf("section %s is not addressable by hash", section)
					}
				}
				// The cabinet listens before account.js announces the first session.
				cabinet := strings.Index(body, `src="`+prefix+`/static/cabinet.js?v=`)
				account := strings.Index(body, `src="`+prefix+`/static/account.js?v=`)
				if cabinet < 0 || account < 0 || cabinet > account {
					t.Fatal("cabinet.js must load (deferred) before account.js")
				}
				if !strings.Contains(body, `href="`+prefix+`/account/delete?lang=`+locale+`"`) {
					t.Fatal("the account block has no delete-account link")
				}
				if strings.Contains(body, "innerHTML") {
					t.Fatal("the shell must not build markup from strings")
				}
			}
		}
	}
}

func TestCabinetBootTellsThePageWhichSectionToOpen(t *testing.T) {
	for _, prefix := range []string{"", "/sandbox"} {
		handler, _ := cabinetHandler(t, prefix, nil)
		for page, want := range map[string]bool{"/account": false, "/payment": true} {
			body := cabinetPage(t, handler, prefix+page+"?lang=en")
			m := regexp.MustCompile(`<script id="account-data" type="application/json">([^<]+)</script>`).FindStringSubmatch(body)
			if m == nil {
				t.Fatal("no boot data")
			}
			var boot struct {
				PaymentPage bool              `json:"payment_page"`
				CaptureURL  string            `json:"capture_url"`
				BasePath    string            `json:"base_path"`
				Messages    map[string]string `json:"messages"`
			}
			if err := json.Unmarshal([]byte(m[1]), &boot); err != nil {
				t.Fatal(err)
			}
			if boot.PaymentPage != want || boot.BasePath != prefix || !strings.HasPrefix(boot.CaptureURL, prefix+"/static/cabinet-capture.js?v=") {
				t.Fatalf("%s%s boot = %+v", prefix, page, boot)
			}
			for _, key := range []string{"role.seller", "trait.no_emoji", "recipient.colleague", "voice.start", "save.conflict", "cost.value"} {
				if boot.Messages[key] == "" {
					t.Fatalf("boot messages are missing %s", key)
				}
			}
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, prefix+"/static/cabinet-capture.js", nil))
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "registerProcessor('aireply-capture'") {
			t.Fatalf("capture worklet not served: %d", w.Code)
		}
	}
}

func TestHeaderOffersSignInOnCommercialPagesOnly(t *testing.T) {
	for _, prefix := range []string{"", "/sandbox"} {
		handler, bundle := cabinetHandler(t, prefix, nil)
		for _, locale := range domain.Locales {
			for _, page := range []string{"/account", "/payment", "/mac", "/windows"} {
				body := cabinetPage(t, handler, prefix+page+"?lang="+locale)
				signIn := `<a class="btn btn-ghost btn-sm header-sign-in" href="` + prefix + `/account?lang=` + locale + `" data-header-sign-in>` + html.EscapeString(bundle.T(locale, "nav.sign_in")) + `</a>`
				if !strings.Contains(body, signIn) {
					t.Fatalf("%s%s?lang=%s has no sign-in button", prefix, page, locale)
				}
				if strings.Index(body, signIn) > strings.Index(body, `header-download`) {
					t.Fatal("«Sign in» must sit next to, and before, «Download»")
				}
				if !strings.Contains(body, `<a href="`+prefix+`/account?lang=`+locale+`"`) || strings.Contains(body, `<a href="`+prefix+`/payment?lang=`+locale+`">`+html.EscapeString(bundle.T(locale, "account.open"))) {
					t.Fatal("the «Account» menu item must open /account")
				}
			}
			for _, page := range []string{"/privacy", "/offer", "/support", "/account/delete"} {
				if body := cabinetPage(t, handler, prefix+page+"?lang="+locale); strings.Contains(body, "data-header-sign-in") {
					t.Fatalf("informational page %s offers sign-in", page)
				}
			}
		}
	}
}

func TestCabinetAppsShowOnlyConfiguredDownloads(t *testing.T) {
	const (
		appStore = "https://apps.apple.com/kz/app/ai-reply/id123456789"
		play     = "https://play.google.com/store/apps/details?id=kz.yerek.aireply"
	)
	none, bundle := cabinetHandler(t, "", nil)
	body := cabinetPage(t, none, "/account?lang=ru")
	if strings.Contains(body, `data-app="`) || !strings.Contains(body, html.EscapeString(bundle.T("ru", "account.apps.empty"))) {
		t.Fatal("unconfigured downloads must not be offered")
	}
	all, _ := cabinetHandler(t, "/sandbox", func(cfg *config.Config) {
		cfg.App.AppStoreURL, cfg.App.PlayStoreURL = appStore, play
		cfg.App.MacOSDownloadURL = "https://example.test/downloads/macos/AI-Reply-1.0.1.dmg"
		cfg.App.WindowsDownloadURL = "https://example.test/downloads/windows/AIReply-Setup-1.0.1.exe"
	})
	body = html.UnescapeString(cabinetPage(t, all, "/sandbox/account?lang=en"))
	for _, want := range []string{`href="` + appStore + `"`, `href="` + play + `"`, `href="/sandbox/mac?lang=en"`, `href="/sandbox/windows?lang=en"`,
		`data-app="ios"`, `data-app="android"`, `data-app="macos"`, `data-app="windows"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("configured downloads missing %s", want)
		}
	}
	if strings.Contains(body, ".dmg") || strings.Contains(body, ".exe") {
		t.Fatal("desktop downloads go through /mac and /windows, which explain the installer")
	}
	wrong, _ := cabinetHandler(t, "", func(cfg *config.Config) {
		cfg.App.AppStoreURL, cfg.App.PlayStoreURL = "https://apps.apple.com.evil.test/app", "javascript:alert(1)"
		cfg.App.MacOSDownloadURL = "https://evil.test/downloads/macos/x.dmg"
	})
	body = cabinetPage(t, wrong, "/account?lang=en")
	if strings.Contains(body, "evil.test") || strings.Contains(body, "javascript:") || strings.Contains(body, `data-app="`) {
		t.Fatal("an unvalidated download URL reached the cabinet")
	}
}

var placeholderPattern = regexp.MustCompile(`\{(\w+)\}`)

func placeholders(s string) []string {
	var out []string
	for _, m := range placeholderPattern.FindAllStringSubmatch(s, -1) {
		out = append(out, m[1])
	}
	sort.Strings(out)
	return out
}

func readLocale(t *testing.T, locale string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "localization", "locales", locale+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var messages map[string]string
	if err := json.Unmarshal(raw, &messages); err != nil {
		t.Fatal(err)
	}
	return messages
}

// The cabinet and its header button speak all four languages with the same
// placeholders, and every role, style and recipient of the contract has a label.
func TestCabinetTranslationsAreComplete(t *testing.T) {
	english := readLocale(t, "en")
	var keys []string
	for key := range english {
		if strings.HasPrefix(key, "account.") || key == "nav.sign_in" || strings.HasPrefix(key, "admin.settings.ai_context") {
			keys = append(keys, key)
		}
	}
	required := []string{"nav.sign_in", "admin.settings.ai_context", "admin.settings.ai_context_hint", "admin.settings.ai_context_chars_per_reply",
		"admin.settings.ai_context_free_chars", "admin.settings.ai_context_max_extra"}
	for _, code := range []string{"seller", "business_owner", "entrepreneur", "investor", "partner", "manager", "employee", "freelancer", "designer",
		"artist", "developer", "marketer", "blogger", "influencer", "student", "teacher", "doctor", "lawyer", "realtor"} {
		required = append(required, "account.role."+code)
	}
	for _, code := range []string{"introvert", "extrovert", "brief", "detailed", "formal", "casual", "humor", "emoji", "no_emoji", "diplomatic", "direct", "warm"} {
		required = append(required, "account.trait."+code)
	}
	for _, key := range accountRecipients {
		required = append(required, "account.recipient."+key, "account.recipient."+key+"_placeholder")
	}
	// Every key the cabinet script names, including those picked by a condition.
	script := staticSource(t, "cabinet.js")
	literals := regexp.MustCompile(`'((?:overview|chart|platforms|platform|cost|profile|recipients|field|save|voice|consent|error)\.[A-Za-z_]+)'`).FindAllStringSubmatch(script, -1)
	if len(literals) < 50 {
		t.Fatalf("suspiciously few message keys in cabinet.js: %d", len(literals))
	}
	for _, m := range literals {
		if m[1] != "voice.wav" { // the upload's file name, not a message
			required = append(required, "account."+m[1])
		}
	}
	for _, key := range []string{"unlimited", "email_unavailable"} {
		if !strings.Contains(script, "t('"+key+"')") {
			t.Fatalf("cabinet.js no longer uses %s: update this list", key)
		}
		required = append(required, "account."+key)
	}
	for _, key := range required {
		if english[key] == "" {
			t.Errorf("en is missing %s", key)
		}
	}
	for _, locale := range domain.Locales {
		messages := readLocale(t, locale)
		for _, key := range keys {
			if strings.TrimSpace(messages[key]) == "" {
				t.Errorf("%s is missing %s", locale, key)
			} else if !reflect.DeepEqual(placeholders(messages[key]), placeholders(english[key])) {
				t.Errorf("%s %s has placeholders %v, en has %v", locale, key, placeholders(messages[key]), placeholders(english[key]))
			}
		}
	}
	// The recipients use the names the apps already ship (§2.2).
	for locale, names := range map[string][]string{
		"ru": {"Друг", "Клиент", "Коллега", "Партнёр"}, "en": {"Friend", "Client", "Colleague", "Partner"},
		"kk": {"Дос", "Клиент", "Әріптес", "Серіктес"}, "uz": {"Doʻst", "Mijoz", "Hamkasb", "Hamkor"},
	} {
		messages := readLocale(t, locale)
		for i, key := range accountRecipients {
			if messages["account.recipient."+key] != names[i] {
				t.Errorf("%s recipient %s = %q, the apps say %q", locale, key, messages["account.recipient."+key], names[i])
			}
		}
	}
}
