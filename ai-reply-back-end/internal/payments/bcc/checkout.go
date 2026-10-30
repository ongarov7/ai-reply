package bcc

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"math/big"
	"mime"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/catalog"
	"github.com/aireply/ai-reply-back-end/internal/database"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/legal"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// CheckoutConfig binds hosted checkout to one bank environment. Production also
// requires an explicit operator opt-in and separately configured credentials.
type CheckoutConfig struct {
	Enabled            bool
	Environment        Environment
	ProductionApproved bool
	AppEnv             string
	PublicBaseURL      string
	Merchant           string
	MerchantName       string
	Terminal           string
	MACKeyHex          string
	NotifyUsername     string
	NotifyPassword     string
	StateSecret        string
}

func (CheckoutConfig) String() string               { return "bcc checkout configuration (redacted)" }
func (CheckoutConfig) GoString() string             { return "bcc.CheckoutConfig{redacted}" }
func (CheckoutConfig) MarshalJSON() ([]byte, error) { return []byte(`{"redacted":true}`), nil }
func (c CheckoutConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	switch c.Environment {
	case Sandbox:
		if c.AppEnv != "test" && c.AppEnv != "development" && c.AppEnv != "staging" {
			return ErrInvalidConfig
		}
	case Production:
		if c.AppEnv != "production" || !c.ProductionApproved {
			return ErrInvalidConfig
		}
	default:
		return ErrInvalidConfig
	}
	u, err := url.Parse(c.PublicBaseURL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Path != "" && u.Path != "/" && u.Path != "/sandbox" || net.ParseIP(u.Hostname()) != nil || !strings.Contains(u.Hostname(), ".") || u.Port() != "" && u.Port() != "443" {
		return ErrInvalidConfig
	}
	if c.Environment == Production && u.Path == "/sandbox" {
		return ErrInvalidConfig
	}
	if !digits(c.Merchant) || c.Terminal == "" || len(c.Merchant) > 32 || len(c.Terminal) > 32 || !printableASCII(c.Merchant) || !printableASCII(c.Terminal) || strings.ContainsAny(c.Merchant+c.Terminal, " \t\n\r") {
		return ErrInvalidConfig
	}
	if !regexp.MustCompile(`^[A-Z0-9][A-Z0-9 .&'()-]{0,20}$`).MatchString(c.MerchantName) || strings.TrimSpace(c.MerchantName) != c.MerchantName {
		return ErrInvalidConfig
	}
	if _, err = decodeMACKey(c.MACKeyHex); err != nil {
		return err
	}
	if c.Environment == Production && (c.Terminal == "88888881" || c.Terminal == "88888888" || strings.EqualFold(c.MACKeyHex, publicTestKey)) {
		return ErrInvalidConfig
	}
	if c.NotifyUsername == "" || len(c.NotifyUsername) > 128 || strings.ContainsAny(c.NotifyUsername, ":\r\n\x00") || len(c.NotifyPassword) < 32 || len(c.NotifyPassword) > 256 || strings.ContainsAny(c.NotifyPassword, "\r\n\x00") || len(c.StateSecret) < 32 || c.StateSecret == c.NotifyPassword {
		return ErrInvalidConfig
	}
	return nil
}

type CheckoutService struct {
	cfg     CheckoutConfig
	db      *database.DB
	catalog *catalog.Service
	client  *Client
	now     func() time.Time
}

func NewCheckout(cfg CheckoutConfig, db *database.DB, prices *catalog.Service) (*CheckoutService, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	s := &CheckoutService{cfg: cfg, db: db, catalog: prices, now: time.Now}
	if !cfg.Enabled {
		return s, nil
	}
	if db == nil || prices == nil {
		return nil, ErrInvalidConfig
	}
	client, err := New(Config{Environment: cfg.Environment, Terminal: cfg.Terminal, MACKeyHex: cfg.MACKeyHex})
	if err != nil {
		return nil, err
	}
	s.client = client
	return s, nil
}
func (s *CheckoutService) Enabled() bool { return s != nil && s.cfg.Enabled }
func (s *CheckoutService) Environment() Environment {
	if s == nil {
		return ""
	}
	return s.cfg.Environment
}

type BankOrder struct {
	ID                 string `json:"payment_id"`
	ProductCode        string `json:"product_code"`
	Units              int    `json:"units"`
	CatalogVersion     int64  `json:"catalog_version"`
	AmountMinor        int64  `json:"amount_minor"`
	Currency           string `json:"currency"`
	Status             string `json:"status"`
	RefundStatus       string `json:"refund_status,omitempty"`
	Environment        string `json:"environment"`
	RedirectURL        string `json:"redirect_url,omitempty"`
	VerificationReason string `json:"verification_reason,omitempty"`
	userID             string
	bankOrder          string
	tokenHash          string
	request            string
	createdAt          int64
	snapshot           string
}

const orderColumns = `id,coalesce(user_id,''),product_code,catalog_version,amount_minor,currency,status,environment,bank_order,checkout_token_hash,request_json,created_at,coalesce((SELECT status FROM billing_bank_operations WHERE order_id=billing_orders.id AND kind='refund'),''),snapshot_json`

func readOrder(row interface{ Scan(...any) error }) (BankOrder, error) {
	var o BankOrder
	err := row.Scan(&o.ID, &o.userID, &o.ProductCode, &o.CatalogVersion, &o.AmountMinor, &o.Currency, &o.Status, &o.Environment, &o.bankOrder, &o.tokenHash, &o.request, &o.createdAt, &o.RefundStatus, &o.snapshot)
	if errors.Is(err, sql.ErrNoRows) {
		err = domain.ErrNotFound
	}
	if err == nil {
		var product catalog.Product
		if err = json.Unmarshal([]byte(o.snapshot), &product); err == nil {
			o.Units = product.Units
		}
	}
	return o, err
}
func (s *CheckoutService) token(id string) string {
	mac := hmac.New(sha256.New, []byte(s.cfg.StateSecret))
	_, _ = mac.Write([]byte("bcc-" + string(s.cfg.Environment) + "-checkout-v1:" + id))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func hashToken(t string) string { h := sha256.Sum256([]byte(t)); return hex.EncodeToString(h[:]) }
func (s *CheckoutService) baseURL() string {
	u, _ := url.Parse(s.cfg.PublicBaseURL)
	u.Host = net.JoinHostPort(u.Hostname(), "443")
	u.Path = strings.TrimRight(u.Path, "/")
	return strings.TrimRight(u.String(), "/")
}
func (s *CheckoutService) redirect(o BankOrder) BankOrder {
	if o.Status != "created" || s.now().Sub(time.UnixMilli(o.createdAt)) > 15*time.Minute {
		return o
	}
	o.RedirectURL = s.baseURL() + "/api/v1/payments/bcc/orders/" + o.ID + "/checkout?state=" + url.QueryEscape(s.token(o.ID))
	return o
}

// Start fixes both price and entitlements before the browser visits the bank.
// It never accepts an amount, merchant, endpoint or recipient from the caller.
func (s *CheckoutService) Start(ctx context.Context, userID, code, idempotency string) (BankOrder, error) {
	return s.start(ctx, userID, code, idempotency, nil)
}

// ExpectedQuote is the exact offer accepted in the authenticated website flow.
// Replaying the same order never replaces its original offer or consent.
type ExpectedQuote struct {
	CatalogVersion int64
	PriceMinor     int64
	Units          int
	Currency       string
	TermsVersion   string
}

func (s *CheckoutService) StartExpected(ctx context.Context, userID, code, idempotency string, expected ExpectedQuote) (BankOrder, error) {
	return s.start(ctx, userID, code, idempotency, &expected)
}

func (s *CheckoutService) start(ctx context.Context, userID, code, idempotency string, expected *ExpectedQuote) (BankOrder, error) {
	if !s.Enabled() {
		return BankOrder{}, domain.ErrPurchasesDisabled
	}
	if userID == "" {
		return BankOrder{}, domain.ErrUnauthorized
	}
	if len(idempotency) < 8 || len(idempotency) > 128 || !printableASCII(idempotency) || strings.TrimSpace(idempotency) != idempotency {
		return BankOrder{}, domain.InvalidField("Idempotency-Key", "required")
	}
	previous, lookupErr := readOrder(s.db.Reader().QueryRowContext(ctx, `SELECT `+orderColumns+` FROM billing_orders WHERE user_id=? AND idempotency_key=?`, userID, idempotency))
	if lookupErr == nil {
		if previous.Environment != string(s.Environment()) {
			return BankOrder{}, domain.ConflictField("Idempotency-Key", "different_environment")
		}
		if previous.ProductCode != code {
			return BankOrder{}, domain.ConflictField("Idempotency-Key", "different_product")
		}
		return s.redirect(previous), nil
	}
	if !errors.Is(lookupErr, domain.ErrNotFound) {
		return BankOrder{}, lookupErr
	}
	var o BankOrder
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		previous, err := readOrder(tx.QueryRowContext(ctx, `SELECT `+orderColumns+` FROM billing_orders WHERE user_id=? AND idempotency_key=?`, userID, idempotency))
		if err == nil {
			if previous.Environment != string(s.Environment()) {
				return domain.ConflictField("Idempotency-Key", "different_environment")
			}
			if previous.ProductCode != code {
				return domain.ConflictField("Idempotency-Key", "different_product")
			}
			o = previous
			return nil
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		// The catalog read shares the BEGIN IMMEDIATE writer transaction with
		// order creation, so a concurrent publication cannot change an accepted offer.
		p, version, err := s.catalog.ProductForWebInTx(ctx, tx, code)
		if err != nil {
			return err
		}
		if p.Kind != "pack" {
			return domain.ErrPlanUnavailable
		}
		if expected != nil {
			if expected.TermsVersion != legal.WebPurchaseTermsVersion {
				return domain.ConflictField("terms_version", "changed")
			}
			if expected.CatalogVersion != version || expected.PriceMinor != p.PriceMinor || expected.Units != p.Units || expected.Currency != p.Currency {
				return domain.ConflictField("quote", "changed")
			}
		}
		now := s.now().UTC()
		o = BankOrder{ID: traits.NewID(), userID: userID, ProductCode: p.Code, Units: p.Units, CatalogVersion: version, AmountMinor: p.PriceMinor, Currency: p.Currency, Status: "created", Environment: string(s.Environment()), createdAt: now.UnixMilli()}
		suffix, err := rand.Int(rand.Reader, big.NewInt(1000000))
		if err != nil {
			return ErrUnavailable
		}
		o.bankOrder = fmt.Sprintf("%d%06d", now.UnixMilli(), suffix.Int64())
		state := s.token(o.ID)
		o.tokenHash = hashToken(state)
		var nonce [16]byte
		if _, err = rand.Read(nonce[:]); err != nil {
			return ErrUnavailable
		}
		amount, _ := FormatKZT(p.PriceMinor)
		var rnID [8]byte
		if _, err = rand.Read(rnID[:]); err != nil {
			return ErrUnavailable
		}
		fields := url.Values{"AMOUNT": {amount}, "CURRENCY": {CurrencyKZT}, "ORDER": {o.bankOrder}, "MERCH_RN_ID": {strings.ToUpper(hex.EncodeToString(rnID[:]))}, "DESC": {"AI Reply " + p.Code}, "MERCHANT": {s.cfg.Merchant}, "MERCH_NAME": {s.cfg.MerchantName}, "TERMINAL": {s.cfg.Terminal}, "MERCH_GMT": {"0"}, "TIMESTAMP": {now.Format("20060102150405")}, "TRTYPE": {"1"}, "NONCE": {strings.ToUpper(hex.EncodeToString(nonce[:]))}, "LANG": {"ru"}, "BACKREF": {s.baseURL() + "/api/v1/payments/bcc/orders/" + o.ID + "/return?state=" + url.QueryEscape(state)}, "NOTIFY_URL": {s.baseURL() + "/api/v1/payments/bcc/notify"}}
		signature, err := SignRequest(Purchase, fields, s.cfg.MACKeyHex)
		if err != nil {
			return err
		}
		fields.Set("P_SIGN", signature)
		raw, _ := json.Marshal(fields)
		snapshot, _ := json.Marshal(p)
		o.request = string(raw)
		_, err = tx.ExecContext(ctx, `INSERT INTO billing_orders(id,user_id,idempotency_key,catalog_version,product_code,snapshot_json,amount_minor,currency,provider,environment,bank_order,status,checkout_token_hash,request_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?, 'bcc',?,?,'created',?,?,?,?)`, o.ID, userID, idempotency, version, p.Code, string(snapshot), p.PriceMinor, p.Currency, o.Environment, o.bankOrder, o.tokenHash, o.request, o.createdAt, o.createdAt)
		if err == nil && expected != nil {
			_, err = tx.ExecContext(ctx, `INSERT INTO billing_web_purchase_consents(order_id,user_id,product_code,catalog_version,price_minor,units,terms_version,source,accepted_at) VALUES(?,?,?,?,?,?,?,'website',?)`, o.ID, userID, p.Code, version, p.PriceMinor, p.Units, expected.TermsVersion, o.createdAt)
		}
		return err
	})
	if err != nil {
		return BankOrder{}, err
	}
	return s.redirect(o), nil
}
func (s *CheckoutService) owned(ctx context.Context, user, id string) (BankOrder, error) {
	if !s.Enabled() {
		return BankOrder{}, domain.ErrPurchasesDisabled
	}
	o, err := readOrder(s.db.Reader().QueryRowContext(ctx, `SELECT `+orderColumns+` FROM billing_orders WHERE id=? AND environment=?`, id, s.Environment()))
	if err != nil {
		return o, err
	}
	if o.userID != user {
		return BankOrder{}, domain.ErrNotFound
	}
	return o, nil
}
func (s *CheckoutService) Status(ctx context.Context, user, id string) (BankOrder, error) {
	o, err := s.owned(ctx, user, id)
	if err != nil {
		return o, err
	}
	if o.Status == "created" || o.Status == "pending" || o.Status == "verification_pending" {
		o.VerificationReason = "authenticated_bank_confirmation_pending"
	}
	if o.Status == "created" {
		if s.now().Sub(time.UnixMilli(o.createdAt)) > 15*time.Minute {
			o.VerificationReason = "checkout_expired_bank_confirmation_pending"
		} else {
			o = s.redirect(o)
		}
	}
	return o, nil
}

// Reconcile performs a signed read-only query. It cannot grant rights because
// the original-operation final-status response is not yet documented by BCC.
func (s *CheckoutService) Reconcile(ctx context.Context, user, id string) (BankOrder, error) {
	o, err := s.owned(ctx, user, id)
	if err != nil {
		return o, err
	}
	if o.Status != "created" && o.Status != "pending" && o.Status != "verification_pending" && o.Status != "needs_review" {
		return o, nil
	}
	if s.now().Sub(time.UnixMilli(o.createdAt)) >= 24*time.Hour {
		if err = s.recordEvent(ctx, o.ID, "reconcile", "needs_review", map[string]string{"reason": "status_query_window_expired"}); err != nil {
			return BankOrder{}, err
		}
		o.Status = "needs_review"
		o.VerificationReason = "status_query_window_expired"
		return o, nil
	}
	response, err := s.client.Query(ctx, QueryRequest{Order: o.bankOrder, OriginalType: Purchase, StartedAt: time.UnixMilli(o.createdAt)})
	if err != nil {
		return BankOrder{}, domain.ErrStoreUnavailable
	}
	if response.Order != "" && response.Order != o.bankOrder || response.Terminal != "" && response.Terminal != s.cfg.Terminal {
		return BankOrder{}, ErrUnsupportedResponse
	}
	status := "verification_pending"
	safe := map[string]string{"query_action": response.Action, "query_rc": response.RC, "reason": "bank_final_status_contract_pending"}
	if err = s.recordEvent(ctx, o.ID, "reconcile", status, safe); err != nil {
		return BankOrder{}, err
	}
	o.Status = status
	o.VerificationReason = safe["reason"]
	return o, nil
}
func (s *CheckoutService) recordEvent(ctx context.Context, id, source, state string, fields map[string]string) error {
	raw, _ := json.Marshal(fields)
	digest := sha256.Sum256([]byte(id + ":" + source + ":" + string(raw)))
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO billing_order_events(id,order_id,source,state,fields_json,created_at) VALUES(?,?,?,?,?,?)`, hex.EncodeToString(digest[:]), id, source, state, string(raw), s.now().UnixMilli())
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE billing_orders SET status=?,updated_at=? WHERE id=? AND status IN ('created','pending','verification_pending','needs_review')`, state, s.now().UnixMilli(), id)
		return err
	})
}
func (s *CheckoutService) withState(r *http.Request) (BankOrder, error) {
	if !s.Enabled() {
		return BankOrder{}, domain.ErrNotFound
	}
	o, err := readOrder(s.db.Reader().QueryRowContext(r.Context(), `SELECT `+orderColumns+` FROM billing_orders WHERE id=? AND environment=?`, r.PathValue("id"), s.Environment()))
	if err != nil {
		return o, err
	}
	if o.userID == "" {
		return BankOrder{}, domain.ErrNotFound
	}
	state := r.URL.Query().Get("state")
	got := hashToken(state)
	if len(state) != 43 || subtle.ConstantTimeCompare([]byte(got), []byte(o.tokenHash)) != 1 {
		return BankOrder{}, domain.ErrNotFound
	}
	return o, nil
}

//go:embed checkout.html
var checkoutPageHTML string
var bankForm = template.Must(template.New("bank_checkout").Parse(checkoutPageHTML))

// CheckoutPage uses the direct connection address. The HTTP API adapter passes
// the existing trusted-proxy policy through CheckoutPageWithClientIP instead.
func (s *CheckoutService) CheckoutPage(w http.ResponseWriter, r *http.Request) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	s.CheckoutPageWithClientIP(w, r, host)
}
func (s *CheckoutService) CheckoutPageWithClientIP(w http.ResponseWriter, r *http.Request, clientIP string) {
	secureHeaders(w)
	o, err := s.withState(r)
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if s.now().Sub(time.UnixMilli(o.createdAt)) > 15*time.Minute || o.Status != "created" {
		http.Error(w, "Checkout expired. Check the existing order status before starting another payment.", http.StatusGone)
		return
	}
	ip := net.ParseIP(clientIP)
	if ip == nil || ip.IsUnspecified() || ip.IsMulticast() {
		http.Error(w, "Client IP unavailable", http.StatusBadRequest)
		return
	}
	var fields url.Values
	if json.Unmarshal([]byte(o.request), &fields) != nil {
		http.Error(w, "Unavailable", http.StatusServiceUnavailable)
		return
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		http.Error(w, "Unavailable", http.StatusServiceUnavailable)
		return
	}
	cspNonce := base64.StdEncoding.EncodeToString(nonce[:])
	endpoint, _ := s.Environment().Endpoint()
	bankURL, _ := url.Parse(endpoint)
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'nonce-"+cspNonce+"'; form-action "+bankURL.Scheme+"://"+bankURL.Host+"; frame-ancestors 'none'; base-uri 'none'")
	amount, _ := FormatKZT(o.AmountMinor)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = bankForm.Execute(w, map[string]any{"Amount": amount, "Units": o.Units, "Sandbox": s.Environment() == Sandbox, "Endpoint": template.URL(endpoint), "Fields": fields, "ClientIP": ip.String(), "CSPNonce": cspNonce})
}

func secureHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; form-action 'none'; frame-ancestors 'none'; base-uri 'none'")
}
func (s *CheckoutService) Return(w http.ResponseWriter, r *http.Request) {
	secureHeaders(w)
	o, err := s.withState(r)
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if _, err = parseNotification(w, r); err != nil {
		http.Error(w, "Invalid return", http.StatusBadRequest)
		return
	}
	if err = s.recordEvent(r.Context(), o.ID, "browser_return", "verification_pending", map[string]string{"reason": "browser_return_is_not_payment_proof"}); err != nil {
		http.Error(w, "Unavailable", http.StatusServiceUnavailable)
		return
	}
	// A bank browser return is only a navigation signal. The account page reads
	// the user-owned server order; no bank-supplied status reaches its URL.
	http.Redirect(w, r, s.baseURL()+"/payment?"+url.Values{"order": {o.ID}}.Encode(), http.StatusSeeOther)
}
func parseNotification(w http.ResponseWriter, r *http.Request) (url.Values, error) {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/x-www-form-urlencoded" {
		return nil, ErrInvalidRequest
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	if err != nil {
		return nil, err
	}
	fields, err := url.ParseQuery(string(body))
	if err != nil || len(fields) > 128 {
		return nil, ErrInvalidRequest
	}
	for name, values := range fields {
		if len(name) > 64 || len(values) != 1 || len(values[0]) > 8192 {
			return nil, ErrInvalidRequest
		}
		switch strings.ToUpper(name) {
		case "CARD", "PAN", "CARD_NUMBER", "CVV", "CVV2", "CVC", "CVC2":
			return nil, ErrInvalidRequest
		}
	}
	return fields, nil
}
func (s *CheckoutService) Notify(w http.ResponseWriter, r *http.Request) {
	secureHeaders(w)
	if !s.Enabled() {
		http.NotFound(w, r)
		return
	}
	username, password, ok := r.BasicAuth()
	a, b := sha256.Sum256([]byte(username)), sha256.Sum256([]byte(s.cfg.NotifyUsername))
	c, d := sha256.Sum256([]byte(password)), sha256.Sum256([]byte(s.cfg.NotifyPassword))
	if len(r.Header.Values("Authorization")) != 1 || !ok || subtle.ConstantTimeCompare(a[:], b[:]) != 1 || subtle.ConstantTimeCompare(c[:], d[:]) != 1 {
		w.Header().Set("WWW-Authenticate", `Basic realm="bcc-notify"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if r.URL.RawQuery != "" {
		http.Error(w, "Unexpected query", http.StatusBadRequest)
		return
	}
	fields, err := parseNotification(w, r)
	if err != nil {
		http.Error(w, "Invalid notification", http.StatusBadRequest)
		return
	}
	o, err := readOrder(s.db.Reader().QueryRowContext(r.Context(), `SELECT `+orderColumns+` FROM billing_orders WHERE bank_order=? AND environment=?`, fields.Get("ORDER"), s.Environment()))
	if err != nil {
		http.Error(w, "Unknown order", http.StatusNotFound)
		return
	}
	kind := fields.Get("TRTYPE")
	if kind == "" {
		kind = fields.Get("TRAN_TRTYPE")
	}
	amount, _ := FormatKZT(o.AmountMinor)
	if fields.Get("TERMINAL") != s.cfg.Terminal || fields.Get("CURRENCY") != CurrencyKZT || fields.Get("AMOUNT") != amount || (kind == "1" && fields.Get("MERCHANT") != s.cfg.Merchant || kind == "14" && fields.Get("MERCHANT") != "" && fields.Get("MERCHANT") != s.cfg.Merchant) || fields.Get("TRTYPE") == "" && fields.Get("TRAN_TRTYPE") == "" || fields.Get("TRTYPE") != "" && fields.Get("TRTYPE") != "1" && fields.Get("TRTYPE") != "14" || fields.Get("TRAN_TRTYPE") != "" && fields.Get("TRAN_TRTYPE") != "1" && fields.Get("TRAN_TRTYPE") != "14" {
		http.Error(w, "Notification mismatch", http.StatusBadRequest)
		return
	}
	if fields.Get("TRTYPE") != "" && fields.Get("TRAN_TRTYPE") != "" && fields.Get("TRTYPE") != fields.Get("TRAN_TRTYPE") {
		http.Error(w, "Notification type mismatch", http.StatusBadRequest)
		return
	}
	var original url.Values
	if kind == "14" {
		original, err = s.refundRequest(r.Context(), o.ID)
		if err != nil {
			http.Error(w, "Unregistered refund", http.StatusBadRequest)
			return
		}
	} else if json.Unmarshal([]byte(o.request), &original) != nil {
		http.Error(w, "Unavailable", http.StatusServiceUnavailable)
		return
	}
	if fields.Get("MERCH_RN_ID") != original.Get("MERCH_RN_ID") || fields.Get("MERCH_RN_ID") == "" {
		http.Error(w, "Notification mismatch", http.StatusBadRequest)
		return
	}
	expectedNonce, receivedNonce := sha256.Sum256([]byte(original.Get("NONCE"))), sha256.Sum256([]byte(fields.Get("NONCE")))
	if fields.Get("NONCE") == "" || subtle.ConstantTimeCompare(expectedNonce[:], receivedNonce[:]) != 1 {
		http.Error(w, "Notification mismatch", http.StatusBadRequest)
		return
	}
	safe := map[string]string{}
	for _, name := range []string{"ORDER", "TERMINAL", "CURRENCY", "AMOUNT", "ACTION", "RC", "RRN", "INT_REF", "TRTYPE", "TRAN_TRTYPE"} {
		if value := fields.Get(name); value != "" {
			safe[name] = value
		}
	}
	// A bank-only Basic Auth secret authenticates this original-operation
	// notification independently from an echoed request P_SIGN. Final success
	// must include both bank references; an intermediate event grants nothing.
	if fields.Get("ACTION") == "0" && fields.Get("RC") == "00" {
		if !bankReference(fields.Get("RRN")) || !bankReference(fields.Get("INT_REF")) {
			http.Error(w, "Missing bank references", http.StatusBadRequest)
			return
		}
		if kind == "14" {
			err = s.refundAuthenticatedPack(r.Context(), o, safe)
		} else {
			err = s.fulfillAuthenticatedPack(r.Context(), o, safe)
		}
	} else if kind == "1" && fields.Get("ACTION") == "2" && fields.Get("RC") != "" && fields.Get("RC") != "00" {
		err = s.recordEvent(r.Context(), o.ID, "authenticated_notify", "failed", safe)
	} else {
		err = s.recordEvent(r.Context(), o.ID, "authenticated_notify", "verification_pending", safe)
	}
	if err != nil {
		http.Error(w, "Unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, "OK\n")
}

func bankReference(value string) bool {
	return value != "" && len(value) <= 64 && printableASCII(value) && !strings.ContainsAny(value, " \t\r\n")
}
