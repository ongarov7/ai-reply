// Package idtoken — Google және Apple беретін identity токендерін (OIDC JWT) тексереді.
//
// Only RS256 is accepted, which is what both providers sign with. The package
// uses the standard library alone: the format is small and fixed, and a JWT
// dependency tree would be a larger attack surface than the ~300 lines here.
package idtoken

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	// ErrInvalidToken — қолтаңба, мерзім, aud/iss не nonce сәйкес емес.
	ErrInvalidToken = errors.New("idtoken: invalid token")
	// ErrUnavailable — провайдердің ашық кілттерін алу мүмкін болмады.
	ErrUnavailable = errors.New("idtoken: signing keys unavailable")
)

// Провайдерлердің жариялаған мәндері.
const (
	GoogleKeysURL = "https://www.googleapis.com/oauth2/v3/certs"
	AppleKeysURL  = "https://appleid.apple.com/auth/keys"

	// maxTokenBytes — бұдан ұзын токен тексерусіз қабылданбайды.
	maxTokenBytes = 16 << 10
	defaultLeeway = time.Minute
)

// GoogleIssuers — Google ID token-нің рұқсат етілген iss мәндері.
var GoogleIssuers = []string{"https://accounts.google.com", "accounts.google.com"}

// AppleIssuers — Apple identity token-нің iss мәні.
var AppleIssuers = []string{"https://appleid.apple.com"}

// Claims — тексерілген токеннің қолданбаға қажет өрістері.
type Claims struct {
	Issuer          string
	Subject         string
	Audience        []string
	AuthorizedParty string
	ExpiresAt       time.Time
	IssuedAt        time.Time
	Nonce           string
	Email           string
	EmailVerified   bool
	IsPrivateEmail  bool
	HostedDomain    string
	Name            string
}

// Config — тексерушінің баптауы.
type Config struct {
	// Issuers — iss өрісінің рұқсат етілген мәндері.
	Issuers []string
	// Audiences — aud өрісінің рұқсат етілген мәндері (біздің client ID-лер).
	Audiences []string
	// Keys — kid бойынша ашық кілт көзі.
	Keys KeySource
	// Leeway — сағат айырмашылығына төзімділік (әдепкі 1 минут).
	Leeway time.Duration
	// Now — тестте уақытты басқару үшін.
	Now func() time.Time
}

// Verifier — бір провайдердің токенін тексереді.
type Verifier struct {
	issuers   []string
	audiences []string
	keys      KeySource
	leeway    time.Duration
	now       func() time.Time
}

// New — тексерушіні құрады. Бос баптау қате: ол кез келген токенді өткізіп жіберер еді.
func New(cfg Config) (*Verifier, error) {
	issuers := nonEmpty(cfg.Issuers)
	audiences := nonEmpty(cfg.Audiences)
	if len(issuers) == 0 || len(audiences) == 0 || cfg.Keys == nil {
		return nil, errors.New("idtoken: issuers, audiences and keys are required")
	}
	leeway := cfg.Leeway
	if leeway <= 0 {
		leeway = defaultLeeway
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Verifier{issuers: issuers, audiences: audiences, keys: cfg.Keys, leeway: leeway, now: now}, nil
}

// NewGoogle — Google ID token тексерушісі.
func NewGoogle(audiences []string, keys KeySource) (*Verifier, error) {
	return New(Config{Issuers: GoogleIssuers, Audiences: audiences, Keys: keys})
}

// NewApple — Sign in with Apple identity token тексерушісі.
func NewApple(audiences []string, keys KeySource) (*Verifier, error) {
	return New(Config{Issuers: AppleIssuers, Audiences: audiences, Keys: keys})
}

// AppleNonce — Apple токеніндегі nonce: клиент жіберген бастапқы мәннің SHA-256 hex-і.
//
// The app puts SHA256(raw) into the Apple request and sends raw to us, so a
// token lifted from one sign-in cannot be replayed with a different nonce.
func AppleNonce(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// Verify — қолтаңбаны, iss, aud, мерзімді және (берілсе) nonce-ты тексереді.
//
// expectedNonce is compared in constant time; an empty value skips the check.
// Errors wrap ErrInvalidToken or ErrUnavailable and name the failed check, so
// the reason can be logged without ever logging the token itself.
func (v *Verifier) Verify(ctx context.Context, token, expectedNonce string) (Claims, error) {
	if token == "" || len(token) > maxTokenBytes {
		return Claims{}, invalid("malformed token")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, invalid("malformed token")
	}

	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := decodeSegment(parts[0], &header); err != nil {
		return Claims{}, invalid("malformed header")
	}
	if header.Alg != "RS256" {
		return Claims{}, invalid("unexpected algorithm")
	}
	if header.Kid == "" {
		return Claims{}, invalid("missing key id")
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(signature) == 0 {
		return Claims{}, invalid("malformed signature")
	}

	key, err := v.keys.PublicKey(ctx, header.Kid)
	if err != nil {
		if errors.Is(err, ErrUnavailable) {
			return Claims{}, err
		}
		return Claims{}, invalid("unknown signing key")
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature); err != nil {
		return Claims{}, invalid("bad signature")
	}

	var raw rawClaims
	if err := decodeSegment(parts[1], &raw); err != nil {
		return Claims{}, invalid("malformed claims")
	}
	return v.validate(raw, expectedNonce)
}

func (v *Verifier) validate(raw rawClaims, expectedNonce string) (Claims, error) {
	now := v.now()
	if !contains(v.issuers, raw.Issuer) {
		return Claims{}, invalid("issuer mismatch")
	}
	if !intersects(v.audiences, raw.Audience) {
		return Claims{}, invalid("audience mismatch")
	}
	if raw.Subject == "" || len(raw.Subject) > 255 {
		return Claims{}, invalid("missing subject")
	}
	if raw.ExpiresAt == 0 {
		return Claims{}, invalid("missing expiry")
	}
	expires := time.Unix(int64(raw.ExpiresAt), 0)
	if now.After(expires.Add(v.leeway)) {
		return Claims{}, invalid("token expired")
	}
	if raw.IssuedAt != 0 && time.Unix(int64(raw.IssuedAt), 0).After(now.Add(v.leeway)) {
		return Claims{}, invalid("issued in the future")
	}
	if raw.NotBefore != 0 && time.Unix(int64(raw.NotBefore), 0).After(now.Add(v.leeway)) {
		return Claims{}, invalid("not yet valid")
	}
	if expectedNonce != "" &&
		subtle.ConstantTimeCompare([]byte(raw.Nonce), []byte(expectedNonce)) != 1 {
		return Claims{}, invalid("nonce mismatch")
	}

	claims := Claims{
		Issuer:          raw.Issuer,
		Subject:         raw.Subject,
		Audience:        []string(raw.Audience),
		AuthorizedParty: raw.AuthorizedParty,
		ExpiresAt:       expires.UTC(),
		Nonce:           raw.Nonce,
		Email:           strings.TrimSpace(raw.Email),
		EmailVerified:   bool(raw.EmailVerified),
		IsPrivateEmail:  bool(raw.IsPrivateEmail),
		HostedDomain:    raw.HostedDomain,
		Name:            strings.TrimSpace(raw.Name),
	}
	if raw.IssuedAt != 0 {
		claims.IssuedAt = time.Unix(int64(raw.IssuedAt), 0).UTC()
	}
	return claims, nil
}

// ---------------------------------------------------------------- JSON

type rawClaims struct {
	Issuer          string      `json:"iss"`
	Subject         string      `json:"sub"`
	Audience        audience    `json:"aud"`
	AuthorizedParty string      `json:"azp"`
	ExpiresAt       numericDate `json:"exp"`
	IssuedAt        numericDate `json:"iat"`
	NotBefore       numericDate `json:"nbf"`
	Nonce           string      `json:"nonce"`
	Email           string      `json:"email"`
	EmailVerified   flexBool    `json:"email_verified"`
	IsPrivateEmail  flexBool    `json:"is_private_email"`
	HostedDomain    string      `json:"hd"`
	Name            string      `json:"name"`
}

// audience — aud бір жол да, жолдар массиві де бола алады (RFC 7519 §4.1.3).
type audience []string

func (a *audience) UnmarshalJSON(b []byte) error {
	var single string
	if err := json.Unmarshal(b, &single); err == nil {
		*a = audience{single}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return err
	}
	*a = audience(many)
	return nil
}

// numericDate — секундтағы уақыт; бүтін де, бөлшек те сан келуі мүмкін.
type numericDate int64

func (n *numericDate) UnmarshalJSON(b []byte) error {
	var f json.Number
	if err := json.Unmarshal(b, &f); err != nil {
		return err
	}
	if i, err := f.Int64(); err == nil {
		*n = numericDate(i)
		return nil
	}
	v, err := f.Float64()
	if err != nil {
		return err
	}
	*n = numericDate(int64(v))
	return nil
}

// flexBool — Apple кей өрістерді "true" жолы ретінде жібереді, Google — bool.
type flexBool bool

func (f *flexBool) UnmarshalJSON(b []byte) error {
	switch string(bytes.TrimSpace(b)) {
	case `true`, `"true"`:
		*f = true
	case `false`, `"false"`, `null`, `""`:
		*f = false
	default:
		return fmt.Errorf("idtoken: not a boolean: %s", b)
	}
	return nil
}

// ---------------------------------------------------------------- helpers

func decodeSegment(segment string, target any) error {
	raw, err := base64.RawURLEncoding.DecodeString(segment)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	return decoder.Decode(target)
}

func invalid(reason string) error { return fmt.Errorf("%w: %s", ErrInvalidToken, reason) }

func nonEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func contains(all []string, v string) bool {
	for _, a := range all {
		if a == v {
			return true
		}
	}
	return false
}

func intersects(allowed, got []string) bool {
	for _, g := range got {
		if contains(allowed, g) {
			return true
		}
	}
	return false
}
