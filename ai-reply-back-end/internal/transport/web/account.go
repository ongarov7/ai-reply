package web

import (
	"encoding/json"
	"html/template"
	"net/http"
	"net/url"
	"strings"

	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// accountRecipients — the four recipient notes of the account AI context, in
// the order the apps show them (docs/web-cabinet-ai-context.md §2).
var accountRecipients = []string{"friend", "client", "colleague", "partner"}

type accountView struct {
	baseView
	Boot        template.JS
	AccountBoot template.JS
	PaymentPage bool
	Offers      []accountOffer
	Recipients  []string
	// Downloads in the cabinet's «Apps» section. A store link is shown only
	// for the store's own page; desktop links lead to /mac and /windows, which
	// explain the installer, and only once a release is published.
	AppStoreURL      string
	PlayStoreURL     string
	MacOSAvailable   bool
	WindowsAvailable bool
}

type accountOffer struct {
	Code      string
	Name      string
	PriceText string
	Units     int
}

// The public shell contains no account data. The cookie-authenticated API
// supplies the current account, balance and available bank products.
func (s *Server) handleAccount(w http.ResponseWriter, r *http.Request) {
	s.renderAccount(w, r, false)
}

func (s *Server) handlePayment(w http.ResponseWriter, r *http.Request) {
	s.renderAccount(w, r, true)
}

func (s *Server) renderAccount(w http.ResponseWriter, r *http.Request, paymentPage bool) {
	locale := s.locale(w, r)
	messages := map[string]string{}
	for _, key := range s.bundle.Keys() {
		if strings.HasPrefix(key, "account.") {
			messages[strings.TrimPrefix(key, "account.")] = s.bundle.T(locale, key)
		}
	}
	raw, err := json.Marshal(map[string]any{
		"locale": locale, "base_path": s.basePath(), "messages": messages,
		// The cabinet opens «Balance» on /payment and records dictation with
		// this AudioWorklet module (same origin, versioned with cabinet.js).
		"payment_page": paymentPage,
		"capture_url":  s.basePath() + "/static/cabinet-capture.js?v=" + s.accountVersion,
	})
	if err != nil {
		http.Error(w, "Unavailable", http.StatusServiceUnavailable)
		return
	}
	base := s.base(locale)
	base.AccountPage = true
	base.CommercialPage = true
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "same-origin")
	view := accountView{
		baseView: base, Boot: template.JS("{}"), AccountBoot: template.JS(raw),
		PaymentPage: paymentPage, Recipients: accountRecipients,
		AppStoreURL:      storePageURL(s.cfg.App.AppStoreURL, "https://apps.apple.com/"),
		PlayStoreURL:     storePageURL(s.cfg.App.PlayStoreURL, "https://play.google.com/"),
		MacOSAvailable:   s.cfg.App.MacOSInstallerURL() != "",
		WindowsAvailable: s.cfg.App.WindowsInstallerURL() != "",
	}
	if paymentPage && s.catalog != nil {
		published, err := s.catalog.Published(r.Context())
		if err != nil {
			http.Error(w, "Unavailable", http.StatusServiceUnavailable)
			return
		}
		for _, product := range published.Products {
			if product.Kind != "pack" || !product.Active || !product.AvailableWeb || product.Units <= 0 || product.PriceMinor <= 0 || product.Currency != "KZT" {
				continue
			}
			name := product.Name[locale]
			if name == "" {
				name = product.Name["en"]
			}
			view.Offers = append(view.Offers, accountOffer{Code: product.Code, Name: name, PriceText: traits.FormatMoney(product.PriceMinor, product.Currency), Units: product.Units})
		}
	}
	s.render(w, "account", "public_layout", view)
}

// storePageURL — the configured store page, or "" unless it is an HTTPS page
// of that store (the same rule config validation applies in production).
func storePageURL(raw, prefix string) string {
	if !strings.HasPrefix(raw, prefix) || strings.ContainsAny(raw, " \t\r\n\"'<>\\") {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Scheme != "https" || u.Host == "" {
		return ""
	}
	return raw
}
