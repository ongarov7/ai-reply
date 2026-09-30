package idtoken

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// KeySource — kid бойынша провайдердің ашық кілтін береді.
type KeySource interface {
	PublicKey(ctx context.Context, kid string) (*rsa.PublicKey, error)
}

// StaticKeys — тұрақты кілттер жиыны (тесттер үшін).
type StaticKeys map[string]*rsa.PublicKey

// PublicKey — kid бойынша кілт.
func (s StaticKeys) PublicKey(_ context.Context, kid string) (*rsa.PublicKey, error) {
	if key, ok := s[kid]; ok {
		return key, nil
	}
	return nil, invalid("unknown key id")
}

const (
	defaultKeysTTL = time.Hour
	minKeysTTL     = 5 * time.Minute
	maxKeysTTL     = 24 * time.Hour
	// refetchInterval — белгісіз kid не қате кезінде JWKS-ті жиірек сұрамаймыз.
	refetchInterval = 30 * time.Second
	fetchTimeout    = 5 * time.Second
	maxJWKSBytes    = 256 << 10
)

// RemoteKeys — HTTPS арқылы алынатын JWKS, Cache-Control бойынша кэштеледі.
//
// Refreshes happen on expiry, and early when a token names a key id the cache
// has not seen (providers rotate keys). Both are throttled, so a flood of
// tokens with made-up key ids cannot turn into a flood of requests to the
// provider. A fetch failure keeps serving keys that are already known.
type RemoteKeys struct {
	url    string
	client *http.Client
	now    func() time.Time

	mu          sync.Mutex
	keys        map[string]*rsa.PublicKey
	expiresAt   time.Time
	lastAttempt time.Time
}

// NewRemoteKeys — client nil болса, 5 секундтық таймауты бар клиент қолданылады.
func NewRemoteKeys(url string, client *http.Client) *RemoteKeys {
	if client == nil {
		client = &http.Client{Timeout: fetchTimeout}
	}
	return &RemoteKeys{url: url, client: client, now: time.Now}
}

// PublicKey — кэштен немесе жаңартылған JWKS-тен кілт.
func (r *RemoteKeys) PublicKey(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now()
	key, known := r.keys[kid]
	fresh := now.Before(r.expiresAt)
	if known && fresh {
		return key, nil
	}

	throttled := !r.lastAttempt.IsZero() && now.Sub(r.lastAttempt) < refetchInterval
	if !throttled {
		r.lastAttempt = now
		if err := r.refresh(ctx, now); err == nil {
			key, known = r.keys[kid]
			fresh = true
		} else if !known {
			return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
	}

	switch {
	case known:
		// Fresh, or stale but still the provider's last published key.
		return key, nil
	case fresh || len(r.keys) > 0:
		return nil, invalid("unknown key id")
	default:
		return nil, fmt.Errorf("%w: keys not loaded", ErrUnavailable)
	}
}

func (r *RemoteKeys) refresh(ctx context.Context, now time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	res, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("jwks status %d", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maxJWKSBytes))
	if err != nil {
		return err
	}
	keys, err := ParseJWKS(body)
	if err != nil {
		return err
	}
	r.keys = keys
	r.expiresAt = now.Add(cacheTTL(res.Header.Get("Cache-Control")))
	return nil
}

// ParseJWKS — RS256 қолтаңба кілттерін JWKS құжатынан оқиды.
func ParseJWKS(body []byte) (map[string]*rsa.PublicKey, error) {
	var doc struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			Use string `json:"use"`
			Alg string `json:"alg"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	keys := make(map[string]*rsa.PublicKey, len(doc.Keys))
	for _, k := range doc.Keys {
		if k.Kty != "RSA" || k.Kid == "" || (k.Use != "" && k.Use != "sig") || (k.Alg != "" && k.Alg != "RS256") {
			continue
		}
		key, err := rsaKey(k.N, k.E)
		if err != nil {
			continue
		}
		keys[k.Kid] = key
	}
	if len(keys) == 0 {
		return nil, errors.New("jwks has no usable RS256 keys")
	}
	return keys, nil
}

func rsaKey(n, e string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(n)
	if err != nil {
		return nil, err
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(e)
	if err != nil || len(eBytes) == 0 || len(eBytes) > 4 {
		return nil, errors.New("bad exponent")
	}
	exponent := 0
	for _, b := range eBytes {
		exponent = exponent<<8 | int(b)
	}
	modulus := new(big.Int).SetBytes(nBytes)
	if modulus.BitLen() < 2048 || exponent < 3 {
		return nil, errors.New("weak key")
	}
	return &rsa.PublicKey{N: modulus, E: exponent}, nil
}

func cacheTTL(header string) time.Duration {
	for _, directive := range strings.Split(header, ",") {
		directive = strings.TrimSpace(strings.ToLower(directive))
		if !strings.HasPrefix(directive, "max-age=") {
			continue
		}
		seconds, err := strconv.Atoi(strings.TrimPrefix(directive, "max-age="))
		if err != nil {
			break
		}
		ttl := time.Duration(seconds) * time.Second
		if ttl < minKeysTTL {
			return minKeysTTL
		}
		if ttl > maxKeysTTL {
			return maxKeysTTL
		}
		return ttl
	}
	return defaultKeysTTL
}
