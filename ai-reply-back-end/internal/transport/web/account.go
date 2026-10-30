package web

import (
	"encoding/json"
	"github.com/aireply/ai-reply-back-end/internal/traits"
	"html/template"
	"net/http"
	"strings"
)

type accountView struct {
	baseView
	Boot        template.JS
	AccountBoot template.JS
	PaymentPage bool
	Offers      []accountOffer
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
		PaymentPage: paymentPage,
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
