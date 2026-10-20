// Package googleplay — Google Play Developer API (androidpublisher v3): бір реттік өнімдерді
// тексеру, тұтыну және қайтарылғандар тізімі.
//
// The server calls Google as a service account (internal/googleauth) with the
// androidpublisher scope; the account needs "View financial data" and "Manage
// orders" in Play Console. Only the standard library is used. Purchase tokens
// travel in the URL path to Google only: they never reach a log line or an
// error returned here.
package googleplay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/googleauth"
)

// Әдепкі мекенжайлар.
const (
	DefaultBaseURL = "https://androidpublisher.googleapis.com"
	Scope          = "https://www.googleapis.com/auth/androidpublisher"
)

// Қателер.
var (
	// ErrNotFound — Google бұл токенді білмейді (жарамсыз, басқа пакеттікі не тым ескі).
	ErrNotFound = errors.New("googleplay: purchase not found")
	// ErrUnavailable — API уақытша жауап бермеді не кіру рұқсаты жоқ (қайталауға болады).
	ErrUnavailable = errors.New("googleplay: the Play Developer API is unavailable")
)

// APIError — Google-дің жауабы (мәртебе және себеп коды, токенсіз).
type APIError struct {
	Status int
	Reason string
	kind   error
}

func (e *APIError) Error() string {
	return fmt.Sprintf("%s: HTTP %d %s", e.kind.Error(), e.Status, e.Reason)
}

// Unwrap — ErrNotFound не ErrUnavailable.
func (e *APIError) Unwrap() error { return e.kind }

// Permanent — қайталау көмектеспейді (4xx, 401/408/429-дан басқасы).
func (e *APIError) Permanent() bool {
	return e.Status >= 400 && e.Status < 500 && e.Status != http.StatusUnauthorized &&
		e.Status != http.StatusRequestTimeout && e.Status != http.StatusTooManyRequests
}

// Config — бір қосымша (package) және бір қызметтік тіркелгі.
type Config struct {
	PackageName string
	ClientEmail string
	// PrivateKey — қызметтік тіркелгінің PEM кілті.
	PrivateKey string
	// BaseURL, TokenURL — тестте жалған серверлерге.
	BaseURL    string
	TokenURL   string
	HTTPClient *http.Client
	Now        func() time.Time
}

// Client — androidpublisher v3 клиенті.
type Client struct {
	pkg    string
	base   string
	tokens *googleauth.TokenSource
	client *http.Client
}

// New — клиент. Қате мәтінінде кілт ешқашан болмайды.
func New(cfg Config) (*Client, error) {
	pkg := strings.TrimSpace(cfg.PackageName)
	if pkg == "" || strings.TrimSpace(cfg.ClientEmail) == "" {
		return nil, errors.New("googleplay: the package name and the service-account e-mail are required")
	}
	key, err := googleauth.ParseRSAKey(cfg.PrivateKey)
	if err != nil {
		return nil, errors.New("googleplay: the private key is not a valid service-account private key")
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	tokens, err := googleauth.New(googleauth.Config{
		ClientEmail: strings.TrimSpace(cfg.ClientEmail), Key: key, Scope: Scope, TokenURL: cfg.TokenURL,
		HTTPClient: client, Now: cfg.Now,
	})
	if err != nil {
		return nil, fmt.Errorf("googleplay: %w", err)
	}
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		base = DefaultBaseURL
	}
	return &Client{pkg: pkg, base: base, tokens: tokens, client: client}, nil
}

// Сатып алу күйлері (purchaseState) және тұтыну күйлері (consumptionState).
const (
	StatePurchased = 0
	StateCanceled  = 1
	StatePending   = 2

	NotConsumed = 0
	Consumed    = 1
)

// Сатып алу түрлері (purchaseType); өріс жоқ — кәдімгі ақылы сатып алу.
const (
	TypeTest     = 0
	TypePromo    = 1
	TypeRewarded = 2
)

// ProductPurchase — purchases.products ресурсы (серверге керек өрістер).
type ProductPurchase struct {
	PurchaseTimeMillis          string `json:"purchaseTimeMillis"`
	PurchaseState               int    `json:"purchaseState"`
	ConsumptionState            int    `json:"consumptionState"`
	AcknowledgementState        int    `json:"acknowledgementState"`
	OrderID                     string `json:"orderId"`
	PurchaseType                *int   `json:"purchaseType"`
	ProductID                   string `json:"productId"`
	Quantity                    int    `json:"quantity"`
	ObfuscatedExternalAccountID string `json:"obfuscatedExternalAccountId"`
	RegionCode                  string `json:"regionCode"`
}

// PurchasedAt — сатып алу уақыты (белгісіз болса nil).
func (p ProductPurchase) PurchasedAt() *time.Time {
	ms, err := strconv.ParseInt(p.PurchaseTimeMillis, 10, 64)
	if err != nil || ms <= 0 {
		return nil
	}
	t := time.UnixMilli(ms).UTC()
	return &t
}

// Units — сатып алынған дана саны (өріс жоқ — 1).
func (p ProductPurchase) Units() int { return max(p.Quantity, 1) }

func (c *Client) productPath(productID, token string) string {
	return c.base + "/androidpublisher/v3/applications/" + url.PathEscape(c.pkg) + "/purchases/products/" +
		url.PathEscape(productID) + "/tokens/" + url.PathEscape(token)
}

// GetProductPurchase — purchases.products.get.
func (c *Client) GetProductPurchase(ctx context.Context, productID, token string) (ProductPurchase, error) {
	var out ProductPurchase
	err := c.call(ctx, http.MethodGet, c.productPath(productID, token), &out)
	return out, err
}

// Consume — purchases.products.consume (сервер бергеннен кейін ғана).
func (c *Client) Consume(ctx context.Context, productID, token string) error {
	return c.call(ctx, http.MethodPost, c.productPath(productID, token)+":consume", nil)
}

// VoidedPurchase — қайтарылған не болдырылмаған сатып алу.
type VoidedPurchase struct {
	PurchaseToken    string `json:"purchaseToken"`
	PurchaseTimeMs   string `json:"purchaseTimeMillis"`
	VoidedTimeMillis string `json:"voidedTimeMillis"`
	OrderID          string `json:"orderId"`
	// VoidedSource — 0 user, 1 developer, 2 Google; VoidedReason — 0 other … 8 chargeback.
	VoidedSource int `json:"voidedSource"`
	VoidedReason int `json:"voidedReason"`
}

// VoidedAt — қашан қайтарылды.
func (v VoidedPurchase) VoidedAt() time.Time {
	ms, _ := strconv.ParseInt(v.VoidedTimeMillis, 10, 64)
	if ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}

// VoidedPage — бір бет және келесісінің белгісі.
type VoidedPage struct {
	Purchases []VoidedPurchase
	Next      string
}

// VoidedPurchases — purchases.voidedpurchases.list (бір реттік өнімдер, startTime-нан бері).
func (c *Client) VoidedPurchases(ctx context.Context, since time.Time, pageToken string) (VoidedPage, error) {
	q := url.Values{}
	q.Set("startTime", strconv.FormatInt(since.UnixMilli(), 10))
	q.Set("maxResults", "1000")
	q.Set("type", "0")
	if pageToken != "" {
		q.Set("token", pageToken)
	}
	var out struct {
		TokenPagination struct {
			NextPageToken string `json:"nextPageToken"`
		} `json:"tokenPagination"`
		VoidedPurchases []VoidedPurchase `json:"voidedPurchases"`
	}
	endpoint := c.base + "/androidpublisher/v3/applications/" + url.PathEscape(c.pkg) + "/purchases/voidedpurchases?" + q.Encode()
	if err := c.call(ctx, http.MethodGet, endpoint, &out); err != nil {
		return VoidedPage{}, err
	}
	return VoidedPage{Purchases: out.VoidedPurchases, Next: out.TokenPagination.NextPageToken}, nil
}

// call — бір сұраныс; 401 болса токенді ұмытып, бір рет қайталайды.
func (c *Client) call(ctx context.Context, method, endpoint string, out any) error {
	for attempt := 0; ; attempt++ {
		status, reason, body, err := c.do(ctx, method, endpoint)
		if err != nil {
			return err
		}
		switch {
		case status >= 200 && status < 300:
			if out == nil || len(strings.TrimSpace(string(body))) == 0 {
				return nil
			}
			if err := json.Unmarshal(body, out); err != nil {
				return &APIError{Status: status, Reason: "unreadable response", kind: ErrUnavailable}
			}
			return nil
		case status == http.StatusUnauthorized && attempt == 0:
			c.tokens.Forget()
			continue
		case status == http.StatusBadRequest || status == http.StatusNotFound || status == http.StatusGone:
			return &APIError{Status: status, Reason: reason, kind: ErrNotFound}
		default:
			return &APIError{Status: status, Reason: reason, kind: ErrUnavailable}
		}
	}
}

func (c *Client) do(ctx context.Context, method, endpoint string) (int, string, []byte, error) {
	access, err := c.tokens.Token(ctx)
	if err != nil {
		return 0, "", nil, fmt.Errorf("%w: %s", ErrUnavailable, googleauth.SafeDetail(err.Error()))
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		return 0, "", nil, fmt.Errorf("%w: request could not be built", ErrUnavailable)
	}
	req.Header.Set("Authorization", "Bearer "+access)
	req.Header.Set("Accept", "application/json")
	if method == http.MethodPost {
		req.Header.Set("Content-Length", "0")
	}
	res, err := c.client.Do(req)
	if err != nil {
		// The URL holds the purchase token: only the transport error itself is kept.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return 0, "", nil, fmt.Errorf("%w: %s", ErrUnavailable, googleauth.SafeDetail(err.Error()))
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	reason := ""
	if res.StatusCode >= 300 {
		var failure struct {
			Error struct {
				Status string `json:"status"`
				Errors []struct {
					Reason string `json:"reason"`
				} `json:"errors"`
			} `json:"error"`
		}
		_ = json.Unmarshal(body, &failure)
		reason = failure.Error.Status
		if len(failure.Error.Errors) > 0 && failure.Error.Errors[0].Reason != "" {
			reason = failure.Error.Errors[0].Reason
		}
		reason = safeCode(reason)
	}
	return res.StatusCode, reason, body, nil
}

// safeCode — Google-дің себеп коды (purchaseTokenNotFound сияқты), тек қауіпсіз таңбалар.
func safeCode(v string) string {
	if len(v) > 60 {
		v = v[:60]
	}
	var b strings.Builder
	for _, r := range v {
		if r == '_' || r == '-' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}
