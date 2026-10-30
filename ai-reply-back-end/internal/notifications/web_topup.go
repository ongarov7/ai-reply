package notifications

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	"github.com/aireply/ai-reply-back-end/internal/catalog"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/payments/bcc"
)

type webTopupOffers struct {
	prices     *catalog.Service
	paymentURL string
}

// WithWebTopupOffers supplies the catalog from this application's own
// database/environment. It never enables the separately gated feature.
func (s *Service) WithWebTopupOffers(prices *catalog.Service, publicBaseURL string) *Service {
	u, err := url.Parse(publicBaseURL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Port() != "" && u.Port() != "443" || u.Path != "" && u.Path != "/" && u.Path != "/sandbox" {
		return s
	}
	u.Host = strings.ToLower(u.Hostname())
	u.Path = strings.TrimRight(u.Path, "/") + "/payment"
	u.RawPath = ""
	s.webTopup = &webTopupOffers{prices: prices, paymentURL: u.String()}
	return s
}

func (s *Service) currentWebTopupOffer(ctx context.Context) (catalog.Product, int64, bool, error) {
	if !s.cfg.WebTopupOfferEnabled || !s.Ready() || s.webTopup == nil || s.webTopup.prices == nil {
		return catalog.Product{}, 0, false, nil
	}
	p, version, err := s.webTopup.prices.ProductForWeb(ctx, "pack30")
	if err == domain.ErrPlanUnavailable {
		return p, version, false, nil
	}
	if err != nil {
		return p, version, false, err
	}
	return p, version, p.Kind == "pack" && p.Units > 0 && p.Currency == "KZT", nil
}

// MaybeQueueWebTopupOffer requires a separate explicit marketing preference.
// With no reachable supported installation, the transaction rolls back without
// consuming the once-per-user key; token registration invokes this again.
func (s *Service) MaybeQueueWebTopupOffer(ctx context.Context, userID string) {
	if userID == "" {
		return
	}
	p, version, available, err := s.currentWebTopupOffer(ctx)
	if err != nil {
		s.log.Warn("website offer lookup failed")
		return
	}
	if !available {
		return
	}
	prefs, err := s.repo.NotificationPreferences(ctx, userID)
	if err != nil || !prefs[domain.CategoryMarketing] {
		return
	}
	price, _ := bcc.FormatKZT(p.PriceMinor)
	price = strings.TrimSuffix(price, ".00")
	_, err = s.NotifyUser(ctx, UserNotification{
		UserID: userID, IdempotencyKey: "web_topup_offer:v1", Type: domain.TypeWebTopupOffer,
		Category: domain.CategoryMarketing, Link: s.webTopup.paymentURL,
		Platforms: []string{domain.PlatformIOS}, RequirePushTarget: true,
		Params:   map[string]string{"product_code": p.Code, "catalog_version": strconv.FormatInt(version, 10), "price_minor": strconv.FormatInt(p.PriceMinor, 10), "price": price, "units": strconv.Itoa(p.Units), "currency": p.Currency},
		TitleKey: "push.web_topup_offer.title", BodyKey: "push.web_topup_offer.body",
	})
	if err != nil {
		s.log.Warn("website offer could not be queued")
	}
}

// Do not deliver a queued promotion after consent, availability or its quoted
// catalog offer changed. Returning from the website still grants no rights.
func (s *Service) webTopupDispatchAllowed(ctx context.Context, n domain.Notification) (bool, error) {
	p, version, available, err := s.currentWebTopupOffer(ctx)
	if err != nil || !available {
		return false, err
	}
	return n.Category == domain.CategoryMarketing && n.Link == s.webTopup.paymentURL &&
		n.Params["product_code"] == p.Code && n.Params["catalog_version"] == strconv.FormatInt(version, 10) &&
		n.Params["price_minor"] == strconv.FormatInt(p.PriceMinor, 10) && n.Params["units"] == strconv.Itoa(p.Units) && n.Params["currency"] == p.Currency, nil
}
