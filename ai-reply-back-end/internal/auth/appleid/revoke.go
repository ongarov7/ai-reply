// Package appleid — Sign in with Apple токендерін кері қайтару (тіркелгі жойылғанда).
//
// App Store Review requires an app that offers Sign in with Apple to revoke
// the user's Apple tokens when the account is deleted. The app sends a fresh
// authorization code with the deletion request; the server exchanges it for a
// refresh token at Apple and revokes that token. Both calls authenticate with
// a short-lived ES256 client secret signed by the .p8 key from the Apple
// Developer account. Only the standard library is used, as in idtoken.
package appleid

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
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
	"time"
)

// DefaultBaseURL — Apple ID серверінің мекенжайы.
const DefaultBaseURL = "https://appleid.apple.com"

const (
	// secretTTL — client_secret қанша уақыт жарамды (Apple шегі — 6 ай; бізге бір сұраныс жетеді).
	secretTTL     = 5 * time.Minute
	clientTimeout = 10 * time.Second
	maxBody       = 16 << 10
)

// ErrRevoke — Apple токенді алмастырмады не кері қайтармады.
var ErrRevoke = errors.New("appleid: token revocation failed")

// Config — Apple Developer ▸ Keys бөліміндегі «Sign in with Apple» кілті.
type Config struct {
	TeamID string
	KeyID  string
	// PrivateKey — .p8 файлының PEM мәтіні (PKCS#8, P-256).
	PrivateKey string
	// ClientIDs — APPLE_CLIENT_ID: қосымшаның bundle id-і (код соған берілген).
	ClientIDs []string
	// BaseURL — тестте жалған серверге бағыттау үшін (бос — DefaultBaseURL).
	BaseURL    string
	HTTPClient *http.Client
	// Now — тестте уақытты бекіту үшін (nil — time.Now).
	Now func() time.Time
}

// Revoker — authorization code → refresh token → revoke.
type Revoker struct {
	teamID    string
	keyID     string
	key       *ecdsa.PrivateKey
	clientIDs []string
	base      string
	client    *http.Client
	now       func() time.Time
}

// New — кілтті тексеріп, кері қайтарушыны құрады. Қате мәтінінде кілт ешқашан болмайды.
func New(cfg Config) (*Revoker, error) {
	if strings.TrimSpace(cfg.TeamID) == "" || strings.TrimSpace(cfg.KeyID) == "" {
		return nil, errors.New("appleid: team id and key id are required")
	}
	if len(cfg.ClientIDs) == 0 {
		return nil, errors.New("appleid: APPLE_CLIENT_ID is required")
	}
	key, err := parseKey(cfg.PrivateKey)
	if err != nil {
		return nil, err
	}
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		base = DefaultBaseURL
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: clientTimeout}
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Revoker{
		teamID: strings.TrimSpace(cfg.TeamID), keyID: strings.TrimSpace(cfg.KeyID), key: key,
		clientIDs: cfg.ClientIDs, base: base, client: client, now: now,
	}, nil
}

func parseKey(raw string) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(raw)))
	if block == nil {
		return nil, errors.New("appleid: APPLE_PRIVATE_KEY is not a PEM key")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.New("appleid: APPLE_PRIVATE_KEY is not a PKCS#8 key")
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok || key.Curve != elliptic.P256() {
		return nil, errors.New("appleid: APPLE_PRIVATE_KEY must be the P-256 .p8 key from Apple")
	}
	return key, nil
}

// Revoke — кодты refresh token-ге алмастырып, оны кері қайтарады.
//
// The code was issued for one of the configured client ids (the app's
// bundle id); each is tried in order until Apple accepts the exchange. The
// returned error carries Apple's status and error code only — never the
// code, the token or the secret.
func (r *Revoker) Revoke(ctx context.Context, code string) error {
	code = strings.TrimSpace(code)
	if code == "" {
		return fmt.Errorf("%w: no authorization code", ErrRevoke)
	}
	var last error
	for _, clientID := range r.clientIDs {
		secret, err := r.clientSecret(clientID)
		if err != nil {
			return err
		}
		token, hint, err := r.exchange(ctx, clientID, secret, code)
		if err != nil {
			last = err
			continue
		}
		return r.revoke(ctx, clientID, secret, token, hint)
	}
	return last
}

// exchange — POST /auth/token (grant_type=authorization_code).
func (r *Revoker) exchange(ctx context.Context, clientID, secret, code string) (token, hint string, err error) {
	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := r.post(ctx, "/auth/token", url.Values{
		"client_id": {clientID}, "client_secret": {secret},
		"code": {code}, "grant_type": {"authorization_code"},
	}, &out); err != nil {
		return "", "", err
	}
	switch {
	case out.RefreshToken != "":
		return out.RefreshToken, "refresh_token", nil
	case out.AccessToken != "":
		return out.AccessToken, "access_token", nil
	default:
		return "", "", fmt.Errorf("%w: token response without a token", ErrRevoke)
	}
}

// revoke — POST /auth/revoke.
func (r *Revoker) revoke(ctx context.Context, clientID, secret, token, hint string) error {
	return r.post(ctx, "/auth/revoke", url.Values{
		"client_id": {clientID}, "client_secret": {secret},
		"token": {token}, "token_type_hint": {hint},
	}, nil)
}

func (r *Revoker) post(ctx context.Context, path string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.base+path, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	res, err := r.client.Do(req)
	if err != nil {
		// A transport error names the URL, never the form.
		return fmt.Errorf("%w: %s: %v", ErrRevoke, path, unwrapURLError(err))
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, maxBody))
	if res.StatusCode != http.StatusOK {
		var failure struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(body, &failure)
		return fmt.Errorf("%w: %s responded %d %s", ErrRevoke, path, res.StatusCode, safeCode(failure.Error))
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("%w: %s: unreadable response", ErrRevoke, path)
		}
	}
	return nil
}

// clientSecret — ES256 JWT: iss = Team ID, sub = client id, aud = Apple.
func (r *Revoker) clientSecret(clientID string) (string, error) {
	now := r.now().UTC()
	header, _ := json.Marshal(map[string]string{"alg": "ES256", "kid": r.keyID})
	claims, _ := json.Marshal(map[string]any{
		"iss": r.teamID, "iat": now.Unix(), "exp": now.Add(secretTTL).Unix(),
		"aud": DefaultBaseURL, "sub": clientID,
	})
	unsigned := b64(header) + "." + b64(claims)
	digest := sha256.Sum256([]byte(unsigned))
	sigR, sigS, err := ecdsa.Sign(rand.Reader, r.key, digest[:])
	if err != nil {
		return "", fmt.Errorf("appleid: sign client secret: %w", err)
	}
	// JWS wants the raw 64-byte r||s form, not ASN.1.
	signature := make([]byte, 64)
	sigR.FillBytes(signature[:32])
	sigS.FillBytes(signature[32:])
	return unsigned + "." + b64(signature), nil
}

func b64(v []byte) string { return base64.RawURLEncoding.EncodeToString(v) }

// safeCode — Apple-дің error өрісі (invalid_grant сияқты), тек қауіпсіз таңбалар.
func safeCode(v string) string {
	if len(v) > 40 {
		v = v[:40]
	}
	var b bytes.Buffer
	for _, r := range v {
		if r == '_' || r == '-' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func unwrapURLError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	return err
}
