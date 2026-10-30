package api

import (
	"errors"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/auth"
	"github.com/aireply/ai-reply-back-end/internal/catalog"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/legal"
	"github.com/aireply/ai-reply-back-end/internal/middleware"
	"github.com/aireply/ai-reply-back-end/internal/payments/bcc"
	"github.com/aireply/ai-reply-back-end/internal/traits"
	"github.com/aireply/ai-reply-back-end/internal/transport/httpx"
)

type webAccount struct {
	s       *Server
	bank    *bcc.CheckoutService
	catalog *catalog.Service
	prefix  string
	origin  string
	secure  bool
}

// RegisterWebAccount gives the standalone website its own cookie session. None
// of these routes is used by native clients or changes their purchase flow.
func (s *Server) RegisterWebAccount(mux *http.ServeMux, bank *bcc.CheckoutService, prices *catalog.Service) {
	base, err := url.Parse(s.cfg.App.PublicBaseURL)
	if err != nil || base.Host == "" {
		return
	}
	// Browsers omit default ports from Origin, even if the configured public
	// URL explicitly includes :443 (the bank requires it on callback URLs).
	originHost := base.Host
	if base.Scheme == "https" && base.Port() == "443" || base.Scheme == "http" && base.Port() == "80" {
		originHost = base.Hostname()
	}
	a := &webAccount{s: s, bank: bank, catalog: prices, origin: strings.ToLower(base.Scheme + "://" + originHost), secure: base.Scheme == "https"}
	if strings.TrimRight(base.Path, "/") == "/sandbox" {
		a.prefix = "/sandbox"
	}
	ip := func(r *http.Request) string { return httpx.ClientIP(r, s.cfg.App.TrustProxy) }
	requestLimit := middleware.RateLimit(s.limiter, "otp_request", s.cfg.Limits.OTPRequestPerHour, time.Hour, ip)
	verifyLimit := middleware.RateLimit(s.limiter, "otp_verify", s.cfg.Limits.OTPVerifyPerHour, time.Hour, ip)
	userKey := func(r *http.Request) string { u, _ := UserFrom(r.Context()); return u.ID }
	checkoutLimit := middleware.RateLimit(s.limiter, "web_checkout", 10, time.Minute, userKey)
	statusLimit := middleware.RateLimit(s.limiter, "web_order_status", 20, time.Minute, userKey)
	handoffCreateLimit := middleware.RateLimit(s.limiter, "web_handoff_create", 5, time.Minute, userKey)
	handoffConsumeLimit := middleware.RateLimit(s.limiter, "web_handoff_consume", 20, time.Minute, ip)
	post := func(h http.Handler) http.Handler { return a.headers(a.sameOrigin(h)) }
	mux.Handle("POST /account/api/email/request", post(requestLimit(http.HandlerFunc(s.handleEmailOTPRequest))))
	mux.Handle("POST /account/api/email/verify", post(verifyLimit(http.HandlerFunc(a.verify))))
	mux.Handle("POST /account/api/logout", post(http.HandlerFunc(a.logout)))
	mux.Handle("POST /api/v1/web/payment-handoff", a.headers(s.requireUser(handoffCreateLimit(http.HandlerFunc(a.createHandoff)))))
	mux.Handle("POST /account/api/handoff", post(handoffConsumeLimit(http.HandlerFunc(a.consumeHandoff))))
	mux.Handle("GET /account/api/session", a.headers(a.requireSession(http.HandlerFunc(a.session))))
	mux.Handle("POST /account/api/checkout", post(a.requireSession(checkoutLimit(http.HandlerFunc(a.checkout)))))
	mux.Handle("GET /account/api/orders/{id}", a.headers(a.requireSession(statusLimit(http.HandlerFunc(a.order)))))
}

func (a *webAccount) createHandoff(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ExpectedUserID string `json:"expected_user_id"`
		Locale         string `json:"locale"`
	}
	if err := httpx.Decode(w, r, 2048, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	user, _ := UserFrom(r.Context())
	if body.ExpectedUserID != user.ID {
		httpx.Error(w, http.StatusConflict, "WEB_ACCOUNT_CHANGED", "The account changed. Open the website again from the app.", nil)
		return
	}
	if !a.secure {
		httpx.Error(w, http.StatusServiceUnavailable, "WEB_HANDOFF_UNAVAILABLE", "Secure website sign-in is unavailable.", nil)
		return
	}
	handoff, err := a.s.auth.CreateWebPaymentHandoff(r.Context(), user.ID, body.Locale)
	if err != nil {
		a.handoffError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"url": a.origin + a.prefix + "/payment#handoff=" + handoff.Code, "expires_in": handoff.ExpiresIn})
}

func (a *webAccount) handoffError(w http.ResponseWriter, err error) {
	if errors.Is(err, auth.ErrWebHandoffInvalid) || errors.Is(err, domain.ErrAccountDisabled) {
		httpx.Error(w, http.StatusUnauthorized, "WEB_HANDOFF_INVALID", "This sign-in link is invalid or expired. Sign in by email or open a new link from the app.", nil)
		return
	}
	httpx.Fail(w, err)
}

func (a *webAccount) consumeHandoff(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code                 string `json:"code"`
		ConfirmAccountSwitch bool   `json:"confirm_account_switch"`
	}
	if err := httpx.Decode(w, r, 2048, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	incoming, err := a.s.auth.PeekWebPaymentHandoff(r.Context(), body.Code)
	if err != nil {
		a.handoffError(w, err)
		return
	}
	var oldToken string
	if cookie, err := r.Cookie(a.cookieName()); err == nil {
		// A cookie containing a native refresh token is never a browser session
		// and must never be revoked by a website account switch.
		current, err := a.s.auth.AuthenticateWebSession(r.Context(), cookie.Value)
		if err == nil {
			oldToken = cookie.Value
			if current.ID != incoming.ID && !body.ConfirmAccountSwitch {
				httpx.Error(w, http.StatusConflict, "WEB_ACCOUNT_SWITCH_REQUIRED", "Confirm switching the website account before continuing.", map[string]any{"email": incoming.Email, "current_email": current.Email})
				return
			}
		} else if !errors.Is(err, domain.ErrUnauthorized) && !errors.Is(err, domain.ErrAccountDisabled) {
			httpx.Fail(w, err)
			return
		}
	}
	session, err := a.s.auth.ConsumeWebPaymentHandoff(r.Context(), body.Code, auth.DeviceInfo{UserAgent: r.UserAgent()})
	if err != nil {
		a.handoffError(w, err)
		return
	}
	if oldToken != "" {
		if _, err := a.s.auth.LogoutSession(r.Context(), oldToken); err != nil {
			// Do not change the browser if the old session could not be ended.
			_, _ = a.s.auth.LogoutSession(r.Context(), session.RefreshToken)
			httpx.Fail(w, err)
			return
		}
	}
	http.SetCookie(w, a.cookie(session.RefreshToken, session.RefreshExpiresAt))
	a.session(w, r.WithContext(WithUser(r.Context(), session.User)))
}

func (a *webAccount) headers(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

func (a *webAccount) sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if strings.ToLower(r.Header.Get("Origin")) != a.origin || r.Header.Get("X-Requested-With") != "AIReplyAccount" || err != nil || media != "application/json" {
			httpx.Error(w, http.StatusForbidden, "WEB_ORIGIN_REQUIRED", "Use the account page to continue.", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *webAccount) cookieName() string {
	if a.prefix != "" {
		return "aireply_web_sandbox_session"
	}
	return "aireply_web_session"
}

func (a *webAccount) cookie(token string, expires time.Time) *http.Cookie {
	c := &http.Cookie{Name: a.cookieName(), Value: token, Path: a.prefix + "/account/", HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteLaxMode, Expires: expires}
	if token == "" {
		c.MaxAge = -1
	} else {
		c.MaxAge = int(time.Until(expires).Seconds())
	}
	return c
}

func (a *webAccount) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(a.cookieName())
		if err != nil {
			httpx.Fail(w, domain.ErrUnauthorized)
			return
		}
		user, err := a.s.auth.AuthenticateWebSession(r.Context(), cookie.Value)
		if err != nil {
			http.SetCookie(w, a.cookie("", time.Unix(1, 0)))
			httpx.Fail(w, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), user)))
	})
}

func (a *webAccount) verify(w http.ResponseWriter, r *http.Request) {
	var body emailOTPVerifyRequest
	if err := httpx.Decode(w, r, a.s.cfg.Limits.RequestBodyBytes, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	info := body.Device.toInfo(r)
	info.Platform, info.DeviceID = "web", traits.NewID()
	session, err := a.s.auth.VerifyEmailOTP(r.Context(), body.Email, body.Code, info)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	// Changing accounts ends the previous browser session without touching the
	// user's app sessions. A replayed OTP cannot manufacture a session.
	if old, err := r.Cookie(a.cookieName()); err == nil {
		_, _ = a.s.auth.LogoutSession(r.Context(), old.Value)
	}
	http.SetCookie(w, a.cookie(session.RefreshToken, session.RefreshExpiresAt))
	a.session(w, r.WithContext(WithUser(r.Context(), session.User)))
}

func (a *webAccount) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(a.cookieName()); err == nil {
		if _, err := a.s.auth.LogoutSession(r.Context(), cookie.Value); err != nil {
			httpx.Fail(w, err)
			return
		}
	}
	http.SetCookie(w, a.cookie("", time.Unix(1, 0)))
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type webBankView struct {
	Enabled        bool              `json:"enabled"`
	Environment    bcc.Environment   `json:"environment"`
	CatalogVersion int64             `json:"catalog_version"`
	TermsVersion   string            `json:"terms_version"`
	Products       []catalog.Product `json:"products"`
}

type webSessionView struct {
	User struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	} `json:"user"`
	Subscription subscriptionDTO `json:"subscription"`
	Usage        usageDTO        `json:"usage"`
	Bank         webBankView     `json:"bank"`
}

func (a *webAccount) session(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	a.s.ensureWelcome(r.Context(), user)
	entitlement, err := a.s.subs.Entitlement(r.Context(), user.ID)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	bank := webBankView{Enabled: a.bank.Enabled(), Environment: a.bank.Environment(), TermsVersion: legal.WebPurchaseTermsVersion, Products: []catalog.Product{}}
	if a.catalog != nil {
		published, err := a.catalog.Published(r.Context())
		if err != nil {
			httpx.Fail(w, err)
			return
		}
		bank.CatalogVersion = published.ID
		for _, p := range published.Products {
			if p.Active && p.AvailableWeb && p.Kind == "pack" && p.Units > 0 && p.PriceMinor > 0 && p.Currency == "KZT" {
				bank.Products = append(bank.Products, p)
			}
		}
	}
	view := webSessionView{Subscription: a.s.subscriptionDTO(entitlement), Usage: a.s.usage(entitlement), Bank: bank}
	view.User.ID, view.User.Email = user.ID, user.Email
	httpx.JSON(w, http.StatusOK, view)
}

func (a *webAccount) checkout(w http.ResponseWriter, r *http.Request) {
	if !a.bank.Enabled() {
		httpx.Error(w, http.StatusServiceUnavailable, "WEB_PAYMENTS_UNAVAILABLE", "Payments are temporarily unavailable.", nil)
		return
	}
	var body struct {
		ProductCode            string `json:"product_code"`
		ExpectedUserID         string `json:"expected_user_id"`
		ExpectedCatalogVersion int64  `json:"expected_catalog_version"`
		ExpectedPriceMinor     int64  `json:"expected_price_minor"`
		ExpectedUnits          int    `json:"expected_units"`
		TermsVersion           string `json:"terms_version"`
		AcceptTerms            bool   `json:"accept_terms"`
	}
	if err := httpx.Decode(w, r, 2048, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	u, _ := UserFrom(r.Context())
	if body.ExpectedUserID != u.ID {
		httpx.Error(w, http.StatusConflict, "WEB_ACCOUNT_CHANGED", "The account changed. Check the recipient before continuing.", nil)
		return
	}
	if !body.AcceptTerms {
		httpx.Error(w, http.StatusConflict, "WEB_TERMS_CHANGED", "Accept the purchase terms before continuing.", nil)
		return
	}
	o, err := a.bank.StartExpected(r.Context(), u.ID, body.ProductCode, r.Header.Get("Idempotency-Key"), bcc.ExpectedQuote{
		CatalogVersion: body.ExpectedCatalogVersion, PriceMinor: body.ExpectedPriceMinor,
		Units: body.ExpectedUnits, Currency: "KZT", TermsVersion: body.TermsVersion,
	})
	if err != nil {
		var conflict *domain.ConflictError
		if errors.As(err, &conflict) && (conflict.Field == "quote" || conflict.Field == "terms_version") {
			code := "WEB_QUOTE_CHANGED"
			if conflict.Field == "terms_version" {
				code = "WEB_TERMS_CHANGED"
			}
			httpx.Error(w, http.StatusConflict, code, "The offer changed. Review the current terms before continuing.", conflict.Details())
			return
		}
		httpx.Fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, o)
}

func (a *webAccount) order(w http.ResponseWriter, r *http.Request) {
	if !a.bank.Enabled() {
		httpx.Error(w, http.StatusServiceUnavailable, "WEB_PAYMENTS_UNAVAILABLE", "Payments are temporarily unavailable.", nil)
		return
	}
	u, _ := UserFrom(r.Context())
	o, err := a.bank.Status(r.Context(), u.ID, r.PathValue("id"))
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, o)
}
