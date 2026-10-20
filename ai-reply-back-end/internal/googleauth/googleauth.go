// Package googleauth — Google қызметтік тіркелгісінің JWT-ін OAuth access token-ге айырбастау.
//
// FCM (push) and Google Play (in-app purchases) both call Google APIs as a
// service account: an RS256 assertion signed with the account's private key
// is exchanged at the token endpoint for a short-lived access token. Only the
// standard library is used; the key never reaches a log line or an error.
package googleauth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// TokenURL — Google OAuth 2.0 token мекенжайы.
const TokenURL = "https://oauth2.googleapis.com/token"

// refreshBefore — кэштегі токен мерзімі бітуге осынша қалғанда жаңарады.
const refreshBefore = 5 * time.Minute

// ErrKey — кілт PEM емес не RSA емес (мазмұны ешқашан айтылмайды).
var ErrKey = errors.New("googleauth: private key is not a valid PEM key of the expected type")

// Config — бір қызметтік тіркелгі және бір scope.
type Config struct {
	ClientEmail string
	// Key — ParseRSAKey оқыған кілт.
	Key   *rsa.PrivateKey
	Scope string
	// TokenURL — тестте жалған серверге (бос — TokenURL).
	TokenURL   string
	HTTPClient *http.Client
	Now        func() time.Time
}

// TokenSource — access token-ді кэштейді және мерзімі бітуге жақын жаңартады.
type TokenSource struct {
	cfg    Config
	client *http.Client
	now    func() time.Time

	mu          sync.Mutex
	accessToken string
	expiresAt   time.Time
}

// New — токен көзі. Пошта мен кілт міндетті.
func New(cfg Config) (*TokenSource, error) {
	if strings.TrimSpace(cfg.ClientEmail) == "" || cfg.Key == nil || strings.TrimSpace(cfg.Scope) == "" {
		return nil, errors.New("googleauth: the client e-mail, the key and the scope are required")
	}
	if cfg.TokenURL == "" {
		cfg.TokenURL = TokenURL
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &TokenSource{cfg: cfg, client: client, now: now}, nil
}

// TokenURL — assertion-ның aud-ы да осы мекенжай.
func (s *TokenSource) TokenURL() string { return s.cfg.TokenURL }

// Token — OAuth access token (кэштеледі, мерзімі бітуге 5 минут қалғанда жаңарады).
//
// The returned error carries the HTTP status and Google's error code only.
func (s *TokenSource) Token(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if s.accessToken != "" && now.Before(s.expiresAt.Add(-refreshBefore)) {
		return s.accessToken, nil
	}

	assertion, err := s.assertion(now)
	if err != nil {
		return "", err
	}
	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
	form.Set("assertion", assertion)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("oauth token request failed: %s", SafeDetail(err.Error()))
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 64*1024))
	if res.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		return "", fmt.Errorf("oauth token refused: HTTP %d %s", res.StatusCode, SafeDetail(e.Error))
	}
	var tok struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(raw, &tok); err != nil || tok.AccessToken == "" {
		return "", errors.New("oauth token response has no access_token")
	}
	if tok.ExpiresIn <= 0 {
		tok.ExpiresIn = 3600
	}
	s.accessToken = tok.AccessToken
	s.expiresAt = now.Add(time.Duration(tok.ExpiresIn) * time.Second)
	return s.accessToken, nil
}

// Forget — кэштегі токен қабылданбады (401): келесі шақыру жаңасын алады.
func (s *TokenSource) Forget() {
	s.mu.Lock()
	s.accessToken = ""
	s.mu.Unlock()
}

// assertion — RS256 JWT, Google оны access token-ге айырбастайды.
func (s *TokenSource) assertion(now time.Time) (string, error) {
	header, err := segment(map[string]string{"alg": "RS256", "typ": "JWT"})
	if err != nil {
		return "", err
	}
	claims, err := segment(map[string]any{
		"iss":   s.cfg.ClientEmail,
		"sub":   s.cfg.ClientEmail,
		"scope": s.cfg.Scope,
		"aud":   s.cfg.TokenURL,
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	})
	if err != nil {
		return "", err
	}
	signing := header + "." + claims
	digest := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, s.cfg.Key, crypto.SHA256, digest[:])
	if err != nil {
		return "", errors.New("oauth assertion could not be signed")
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func segment(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// ParseRSAKey — Google қызметтік тіркелгісінің кілті (PKCS#8, кейде PKCS#1).
func ParseRSAKey(raw string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(raw)))
	if block == nil {
		return nil, ErrKey
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if rsaKey, ok := key.(*rsa.PrivateKey); ok {
			return rsaKey, nil
		}
		return nil, ErrKey
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	return nil, ErrKey
}

// SafeDetail — қысқа, бір жолды мәтін (журнал мен қате үшін).
func SafeDetail(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 160 {
		s = string(r[:160])
	}
	return s
}
