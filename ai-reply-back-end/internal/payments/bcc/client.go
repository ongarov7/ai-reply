package bcc

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultTimeout = 10 * time.Second
	maxTimeout     = 30 * time.Second
	queryWindow    = 24 * time.Hour
	publicTestKey  = "6BB0AC02E47BDF73D98FEB777F3B5294"
)

// Config accepts only the documented endpoint for its explicit environment.
// The key is optional for Health; Query requires a valid configured MAC key.
type Config struct {
	Environment Environment
	Endpoint    string
	Terminal    string
	MACKeyHex   string `json:"-"`
	Timeout     time.Duration
}

func (Config) String() string   { return "bcc client configuration (redacted)" }
func (Config) GoString() string { return "bcc.Config{redacted}" }

// Client has read-only HTTP operations only. Construction performs no I/O.
// The HTTP transport and time source are private; production callers cannot
// configure arbitrary URLs, redirects or an insecure TLS transport here.
type Client struct {
	endpoint string
	terminal string
	key      []byte
	http     *http.Client
	now      func() time.Time
}

func (Client) String() string   { return "bcc read-only client (redacted)" }
func (Client) GoString() string { return "bcc.Client{redacted}" }

func New(cfg Config) (*Client, error) {
	endpoint, err := cfg.Environment.Endpoint()
	if err != nil {
		return nil, err
	}
	if cfg.Endpoint != "" {
		if err := ValidateEndpoint(cfg.Environment, cfg.Endpoint); err != nil {
			return nil, err
		}
		endpoint = cfg.Endpoint
	}
	if cfg.Terminal == "" || len(cfg.Terminal) > 32 || !printableASCII(cfg.Terminal) || strings.ContainsAny(cfg.Terminal, " \t\r\n") {
		return nil, ErrInvalidConfig
	}
	if cfg.Environment == Production && (cfg.Terminal == "88888881" || cfg.Terminal == "88888888" || strings.EqualFold(cfg.MACKeyHex, publicTestKey)) {
		return nil, ErrInvalidConfig
	}
	var key []byte
	if cfg.MACKeyHex != "" {
		key, err = decodeMACKey(cfg.MACKeyHex)
		if err != nil {
			return nil, err
		}
	}
	if cfg.Timeout < 0 || cfg.Timeout > maxTimeout {
		return nil, ErrInvalidConfig
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}).DialContext,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		IdleConnTimeout:       30 * time.Second,
		MaxIdleConnsPerHost:   2,
	}
	return &Client{endpoint: endpoint, terminal: cfg.Terminal, key: key, now: time.Now,
		http: &http.Client{Transport: transport, Timeout: timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (c *Client) Health(ctx context.Context) (Response, error) {
	return c.post(ctx, ConnectionCheck, url.Values{"TERMINAL": {c.terminal}, "TRTYPE": {string(ConnectionCheck)}, "LANG": {"ru"}})
}

// QueryRequest binds a status query to the stored attempt, not callback data.
// StartedAt is the original operation's persisted UTC creation time.
type QueryRequest struct {
	Order        string
	OriginalType TransactionType
	StartedAt    time.Time
}

func (c *Client) Query(ctx context.Context, request QueryRequest) (Response, error) {
	out := Response{RequestType: StatusQuery}
	if _, err := signedFields(request.OriginalType); err != nil || request.OriginalType == StatusQuery {
		return out, ErrUnsupportedTransaction
	}
	if len(c.key) == 0 {
		return out, ErrInvalidKey
	}
	now := c.now().UTC()
	if len(request.Order) < 6 || len(request.Order) > 32 || !digits(request.Order) || request.StartedAt.IsZero() || request.StartedAt.After(now) {
		return out, ErrInvalidRequest
	}
	if now.Sub(request.StartedAt) >= queryWindow {
		return out, ErrQueryWindowExpired
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return out, ErrUnavailable
	}
	fields := url.Values{"TERMINAL": {c.terminal}, "TRTYPE": {string(StatusQuery)}, "TRAN_TRTYPE": {string(request.OriginalType)},
		"ORDER": {request.Order}, "TIMESTAMP": {now.Format("20060102150405")}, "MERCH_GMT": {"0"},
		"NONCE": {strings.ToUpper(hex.EncodeToString(nonce[:]))}}
	signature, err := signRequest(StatusQuery, fields, c.key)
	if err != nil {
		return out, err
	}
	fields.Set("P_SIGN", signature)
	return c.post(ctx, StatusQuery, fields)
}

// post makes one request. It does not retry, follow redirects, execute HTML or
// include the request/body or transport error text in returned errors.
func (c *Client) post(ctx context.Context, kind TransactionType, fields url.Values) (Response, error) {
	out := Response{RequestType: kind}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, strings.NewReader(fields.Encode()))
	if err != nil {
		return out, ErrInvalidRequest
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json, text/html, application/xhtml+xml, application/x-www-form-urlencoded")
	response, err := c.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return out, errors.Join(ErrUnavailable, ctx.Err())
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return out, errors.Join(ErrUnavailable, context.DeadlineExceeded)
		}
		return out, ErrUnavailable
	}
	defer response.Body.Close()
	out.HTTPStatus = response.StatusCode
	if response.StatusCode != http.StatusOK {
		return out, ErrUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return out, ErrUnavailable
	}
	parsed, err := ParseResponse(kind, response.Header.Get("Content-Type"), body)
	parsed.HTTPStatus = response.StatusCode
	return parsed, err
}
