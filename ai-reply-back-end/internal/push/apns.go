package push

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// APNs мекенжайлары.
const (
	APNsProductionURL = "https://api.push.apple.com"
	APNsSandboxURL    = "https://api.sandbox.push.apple.com"
)

// APNsConfig — токенге негізделген (.p8) APNs баптауы.
type APNsConfig struct {
	KeyID      string
	TeamID     string
	BundleID   string // apns-topic
	PrivateKey string // .p8 PEM
	// DefaultEnvironment — "production" or "sandbox", for tokens whose app
	// build did not report its environment.
	DefaultEnvironment string
	// ProductionURL and SandboxURL are overridden only in tests.
	ProductionURL string
	SandboxURL    string
	HTTPClient    *http.Client
	Now           func() time.Time
}

// APNs — Apple Push Notification service провайдері (HTTP/2).
type APNs struct {
	cfg    APNsConfig
	key    *ecdsa.PrivateKey
	client *http.Client
	now    func() time.Time

	mu       sync.Mutex
	jwt      string
	issuedAt time.Time
}

var (
	apnsKeyID  = regexp.MustCompile(`^[A-Z0-9]{10}$`)
	apnsTeamID = regexp.MustCompile(`^[A-Z0-9]{10}$`)
)

// NewAPNs — кілтті тексеріп, провайдер жасайды.
func NewAPNs(cfg APNsConfig) (*APNs, error) {
	if !apnsKeyID.MatchString(cfg.KeyID) {
		return nil, errors.New("apns: APNS_KEY_ID must be the 10-character key id")
	}
	if !apnsTeamID.MatchString(cfg.TeamID) {
		return nil, errors.New("apns: APNS_TEAM_ID must be the 10-character team id")
	}
	if strings.TrimSpace(cfg.BundleID) == "" {
		return nil, errors.New("apns: APNS_BUNDLE_ID is required")
	}
	key, err := parseECKey(cfg.PrivateKey)
	if err != nil {
		return nil, errors.New("apns: APNS_PRIVATE_KEY is not a valid .p8 key")
	}
	if cfg.DefaultEnvironment != "sandbox" {
		cfg.DefaultEnvironment = "production"
	}
	if cfg.ProductionURL == "" {
		cfg.ProductionURL = APNsProductionURL
	}
	if cfg.SandboxURL == "" {
		cfg.SandboxURL = APNsSandboxURL
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				Proxy:               http.ProxyFromEnvironment,
				ForceAttemptHTTP2:   true, // APNs speaks HTTP/2 only
				MaxIdleConnsPerHost: 4,
				IdleConnTimeout:     5 * time.Minute,
				TLSHandshakeTimeout: 10 * time.Second,
			},
		}
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &APNs{cfg: cfg, key: key, client: client, now: now}, nil
}

// Name — провайдер аты.
func (a *APNs) Name() string { return "apns" }

// BuildAPNsPayload — APNs денесі.
//
// The alert is shown by iOS itself; the app reads the same flat keys (nid,
// did, type, category, link and custom ones) from userInfo when the person
// taps it or while it is in the foreground. Custom keys can never shadow
// "aps": the notification service rejects reserved names first.
func BuildAPNsPayload(msg Message) map[string]any {
	aps := map[string]any{
		"alert": map[string]string{"title": msg.Title, "body": msg.Body},
		"sound": "default",
	}
	if msg.Category != "" {
		aps["thread-id"] = msg.Category
	}
	payload := map[string]any{"aps": aps}
	for k, v := range msg.Data {
		if k == "aps" {
			continue
		}
		payload[k] = v
	}
	return payload
}

// Send — бір құрылғыға бір хабарлама.
func (a *APNs) Send(ctx context.Context, target Target, msg Message) Result {
	env := target.Environment
	guessed := env == ""
	if guessed {
		env = a.cfg.DefaultEnvironment
	}
	result := a.send(ctx, env, target.Token, msg)
	// A token from a development build is refused by the production host with
	// BadDeviceToken (and the other way round). When the app did not say which
	// environment it runs in, try the other host once before calling the token
	// dead.
	if guessed && result.Outcome == InvalidToken && result.Code == "BadDeviceToken" {
		other := "sandbox"
		if env == "sandbox" {
			other = "production"
		}
		if second := a.send(ctx, other, target.Token, msg); second.Outcome == Accepted {
			return second
		}
	}
	return result
}

func (a *APNs) send(ctx context.Context, env, token string, msg Message) Result {
	provider, err := a.providerToken(false)
	if err != nil {
		return Result{Outcome: Rejected, Code: "PROVIDER_TOKEN", Detail: "provider token could not be signed"}
	}
	body, err := json.Marshal(BuildAPNsPayload(msg))
	if err != nil {
		return Result{Outcome: Rejected, Code: "PAYLOAD_ENCODING", Detail: "payload could not be encoded"}
	}
	base := a.cfg.ProductionURL
	if env == "sandbox" {
		base = a.cfg.SandboxURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(base, "/")+"/3/device/"+token, bytes.NewReader(body))
	if err != nil {
		return Result{Outcome: Rejected, Code: "REQUEST", Detail: "request could not be built"}
	}
	priority := "5"
	if msg.Important {
		priority = "10"
	}
	req.Header.Set("authorization", "bearer "+provider)
	req.Header.Set("apns-topic", a.cfg.BundleID)
	req.Header.Set("apns-push-type", "alert")
	req.Header.Set("apns-priority", priority)
	if msg.TTL > 0 {
		req.Header.Set("apns-expiration", strconv.FormatInt(a.now().Add(msg.TTL).Unix(), 10))
	}
	if msg.CollapseID != "" && len(msg.CollapseID) <= 64 {
		req.Header.Set("apns-collapse-id", msg.CollapseID)
	}
	req.Header.Set("content-type", "application/json")

	res, err := a.client.Do(req)
	if err != nil {
		return transportFailure(ctx, err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 16*1024))
	if res.StatusCode == http.StatusOK {
		return Result{Outcome: Accepted, MessageID: res.Header.Get("apns-id"), StatusCode: res.StatusCode}
	}
	result := ClassifyAPNs(res.StatusCode, raw, res.Header.Get("Retry-After"), a.now())
	if result.Code == "ExpiredProviderToken" {
		_, _ = a.providerToken(true)
	}
	return result
}

// ClassifyAPNs — APNs жауабын жіктейді.
//
// https://developer.apple.com/documentation/usernotifications/handling-notification-responses-from-apns
//
//	BadDeviceToken, DeviceTokenNotForTopic (400), Unregistered,
//	ExpiredToken (410)                                  → InvalidToken
//	TooManyRequests, TooManyProviderTokenUpdates (429),
//	InternalServerError (500), ServiceUnavailable,
//	Shutdown (503), ExpiredProviderToken, IdleTimeout   → Retry
//	other 4xx (payload, topic, provider credentials)     → Rejected
func ClassifyAPNs(status int, body []byte, retryAfterHeader string, now time.Time) Result {
	var parsed struct {
		Reason string `json:"reason"`
	}
	_ = json.Unmarshal(body, &parsed)
	result := Result{
		Code:       parsed.Reason,
		StatusCode: status,
		Detail:     statusDetail(status, parsed.Reason),
		RetryAfter: retryAfter(retryAfterHeader, now),
	}
	switch parsed.Reason {
	case "BadDeviceToken", "DeviceTokenNotForTopic", "Unregistered", "ExpiredToken":
		result.Outcome = InvalidToken
	case "TooManyRequests", "TooManyProviderTokenUpdates", "InternalServerError",
		"ServiceUnavailable", "Shutdown", "ExpiredProviderToken", "IdleTimeout":
		result.Outcome = Retry
	default:
		switch {
		case status == http.StatusGone:
			result.Outcome = InvalidToken
		case status == http.StatusTooManyRequests || status >= 500:
			result.Outcome = Retry
		default:
			result.Outcome = Rejected
		}
	}
	if result.Code == "" {
		result.Code = fmt.Sprintf("HTTP_%d", status)
	}
	return result
}

// providerToken — ES256 JWT; 40 минут сайын жаңарады (Apple: 20–60 минут аралығы).
func (a *APNs) providerToken(force bool) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	age := now.Sub(a.issuedAt)
	if a.jwt != "" && age < 40*time.Minute && !(force && age > time.Minute) {
		return a.jwt, nil
	}
	header, err := jwtSegment(map[string]string{"alg": "ES256", "kid": a.cfg.KeyID})
	if err != nil {
		return "", err
	}
	claims, err := jwtSegment(map[string]any{"iss": a.cfg.TeamID, "iat": now.Unix()})
	if err != nil {
		return "", err
	}
	signing := header + "." + claims
	digest := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, a.key, digest[:])
	if err != nil {
		return "", err
	}
	// JWS wants the raw 64-byte R||S, not ASN.1.
	sig := make([]byte, 64)
	fill(sig[:32], r)
	fill(sig[32:], s)
	a.jwt = signing + "." + b64url(sig)
	a.issuedAt = now
	return a.jwt, nil
}

func fill(dst []byte, n *big.Int) {
	b := n.Bytes()
	copy(dst[len(dst)-len(b):], b)
}
