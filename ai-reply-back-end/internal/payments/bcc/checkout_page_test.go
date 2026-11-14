package bcc

import (
	"context"
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/aireply/ai-reply-back-end/internal/domain"
)

func checkoutHiddenFields(body string) url.Values {
	fields := make(url.Values)
	for _, input := range regexp.MustCompile(`<input\b[^>]*>`).FindAllString(body, -1) {
		attributes := make(map[string]string)
		for _, attr := range regexp.MustCompile(`([\w-]+)="([^"]*)"`).FindAllStringSubmatch(input, -1) {
			attributes[attr[1]] = html.UnescapeString(attr[2])
		}
		if attributes["type"] == "hidden" && attributes["name"] != "" {
			fields.Add(attributes["name"], attributes["value"])
		}
	}
	return fields
}

func TestCheckoutPresentationKeepsSignedOrderAndPurchaseUnchanged(t *testing.T) {
	for _, locale := range domain.Locales {
		t.Run(locale, func(t *testing.T) {
			s, _ := testCheckout(t)
			s.cfg.PublicBaseURL += "/sandbox"
			order, err := s.Start(context.Background(), "bank-user", "pack30", "localized-form")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.db.Writer().Exec(`UPDATE billing_order_context SET locale=? WHERE order_id=?`, locale, order.ID); err != nil {
				t.Fatal(err)
			}
			var original url.Values
			if err = json.Unmarshal([]byte(order.request), &original); err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest(http.MethodGet, order.RedirectURL, nil)
			r.SetPathValue("id", order.ID)
			w := httptest.NewRecorder()
			s.CheckoutPageWithClientIP(w, r, "203.0.113.9")
			body := w.Body.String()
			if w.Code != http.StatusOK || !strings.Contains(body, `lang="`+locale+`"`) ||
				!strings.Contains(body, s.translations.T(locale, "checkout.title")) || strings.Contains(body, "checkout.") {
				t.Fatal("checkout did not render in the selected purchase language")
			}
			if !strings.Contains(body, "/sandbox/payment?order="+order.ID+"&amp;lang="+locale) ||
				!strings.Contains(body, "/sandbox/support?lang="+locale) {
				t.Fatal("checkout navigation lost its environment or language")
			}
			fields := checkoutHiddenFields(body)
			if fields.Get("CLIENT_IP") != "203.0.113.9" || !fields.Has("M_INFO") || fields.Get("M_INFO") != "" {
				t.Fatal("browser-specific fields were lost or fabricated")
			}
			fields.Del("CLIENT_IP")
			fields.Del("M_INFO")
			if !reflect.DeepEqual(fields, original) {
				t.Fatal("styled checkout changed the immutable signed bank fields")
			}
			mac, err := SignRequest(Purchase, fields, s.cfg.MACKeyHex)
			if err != nil || fields.Get("P_SIGN") != mac {
				t.Fatal("rendered purchase signature no longer matches the bank request")
			}
			csp := w.Header().Get("Content-Security-Policy")
			nonces := regexp.MustCompile(`<(?:style|script) nonce="([^"]+)"`).FindAllStringSubmatch(body, -1)
			if len(nonces) != 2 || nonces[0][1] != nonces[1][1] ||
				!strings.Contains(csp, "style-src 'nonce-"+html.UnescapeString(nonces[0][1])+"'") ||
				!strings.Contains(csp, "script-src 'nonce-"+html.UnescapeString(nonces[1][1])+"'") || strings.Contains(csp, "unsafe-inline") {
				t.Fatal("checkout styling or script is not protected by its response nonce")
			}
			if w.Header().Get("Referrer-Policy") != "no-referrer" || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
				t.Fatal("checkout lost its privacy headers")
			}
			after, err := s.owned(context.Background(), "bank-user", order.ID)
			if err != nil || after.Status != "created" || after.request != order.request || bankBalance(t, s) != 0 {
				t.Fatal("rendering checkout changed the order or granted a purchase")
			}
		})
	}
}

func TestCheckoutUsesSiteLanguagePreferences(t *testing.T) {
	for _, tc := range []struct{ name, query, cookie, browser, want string }{
		{"explicit choice", "kk", "ru", "en", "kk"},
		{"site cookie", "", "ru", "en", "ru"},
		{"browser", "", "", "ru-KZ, en;q=0.9", "ru"},
		{"purchase language", "", "", "", "uz"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := testCheckout(t)
			order, err := s.Start(context.Background(), "bank-user", "pack30", "language-preference")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.db.Writer().Exec(`UPDATE billing_order_context SET locale='uz' WHERE order_id=?`, order.ID); err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest(http.MethodGet, order.RedirectURL, nil)
			r.SetPathValue("id", order.ID)
			if tc.query != "" {
				q := r.URL.Query()
				q.Set("lang", tc.query)
				r.URL.RawQuery = q.Encode()
			}
			if tc.cookie != "" {
				r.AddCookie(&http.Cookie{Name: "lang", Value: tc.cookie})
			}
			r.Header.Set("Accept-Language", tc.browser)
			w := httptest.NewRecorder()
			s.CheckoutPageWithClientIP(w, r, "203.0.113.9")
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `lang="`+tc.want+`"`) {
				t.Fatal("checkout did not follow site language preferences")
			}
		})
	}
}
