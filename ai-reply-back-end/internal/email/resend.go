package email

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"strings"
	"time"
)

// DefaultResendURL — Resend API мекенжайы.
const DefaultResendURL = "https://api.resend.com"

const (
	resendTimeout   = 10 * time.Second
	resendAttempts  = 2
	resendRetryWait = 400 * time.Millisecond
	maxErrorBody    = 16 << 10
)

// ResendConfig — Resend жіберушісінің баптауы. Кілт тек ортадан келеді.
type ResendConfig struct {
	APIKey    string
	FromEmail string
	FromName  string
	// BaseURL — тестте жалған серверге бағыттау үшін (бос болса — DefaultResendURL).
	BaseURL string
	// HTTPClient — бос болса, 10 секунд таймауты бар клиент.
	HTTPClient *http.Client
	Translate  Translator
}

// Resend — https://resend.com арқылы OTP хаттарын жібереді.
type Resend struct {
	apiKey    string
	from      string
	brand     string
	endpoint  string
	client    *http.Client
	translate Translator
}

// NewResend — баптауды тексеріп, жіберушіні құрады.
func NewResend(cfg ResendConfig) (*Resend, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("email: RESEND_API_KEY is required")
	}
	from, err := mail.ParseAddress(strings.TrimSpace(cfg.FromEmail))
	if err != nil || from.Name != "" {
		return nil, errors.New("email: RESEND_FROM_EMAIL must be a plain address")
	}
	if cfg.Translate == nil {
		return nil, errors.New("email: translator is required")
	}
	brand := strings.TrimSpace(cfg.FromName)
	if brand == "" {
		brand = "AI Reply"
	}
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		base = DefaultResendURL
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: resendTimeout}
	}
	return &Resend{
		apiKey:    strings.TrimSpace(cfg.APIKey),
		from:      (&mail.Address{Name: brand, Address: from.Address}).String(),
		brand:     brand,
		endpoint:  base + "/emails",
		client:    client,
		translate: cfg.Translate,
	}, nil
}

// DeliveryError — Resend жауабы (журнал үшін: кілт те, код та жоқ).
type DeliveryError struct {
	Status int
	Name   string
}

func (e *DeliveryError) Error() string {
	if e.Name != "" {
		return fmt.Sprintf("email: resend responded %d (%s)", e.Status, e.Name)
	}
	return fmt.Sprintf("email: resend responded %d", e.Status)
}

// Unwrap — errors.Is(err, ErrDelivery) жұмыс істеуі үшін.
func (e *DeliveryError) Unwrap() error { return ErrDelivery }

type resendRequest struct {
	From    string            `json:"from"`
	To      []string          `json:"to"`
	Subject string            `json:"subject"`
	HTML    string            `json:"html"`
	Text    string            `json:"text"`
	Headers map[string]string `json:"headers,omitempty"`
}

// SendOTP — хатты жібереді; уақытша қатеде бір рет қайталайды.
//
// The retry reuses the same Idempotency-Key, so Resend delivers at most one
// email even when the first response was lost on the way back. 4xx answers
// (bad key, unverified domain, quota) are not retried: repeating them cannot
// succeed and only burns the rate limit.
func (r *Resend) SendOTP(ctx context.Context, msg OTPMessage) error {
	content, err := RenderOTP(r.translate, r.brand, msg.Locale, msg.Code, msg.TTL)
	if err != nil {
		return err
	}
	payload := resendRequest{
		From:    r.from,
		To:      []string{msg.To},
		Subject: content.Subject,
		HTML:    content.HTML,
		Text:    content.Text,
	}
	if msg.Reference != "" {
		// A unique header keeps Gmail from folding consecutive codes into one
		// thread, where the newest code would hide under the oldest.
		payload.Headers = map[string]string{"X-Entity-Ref-ID": msg.Reference}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	var last error
	for attempt := 1; attempt <= resendAttempts; attempt++ {
		retry, err := r.post(ctx, body, msg.Reference)
		if err == nil {
			return nil
		}
		last = err
		if !retry || attempt == resendAttempts {
			break
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: %v", ErrDelivery, ctx.Err())
		case <-time.After(resendRetryWait):
		}
	}
	return last
}

// post — бір HTTP сұранысы. retry=true — қайталауға болатын қате.
func (r *Resend) post(ctx context.Context, body []byte, reference string) (retry bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint, bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+r.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "ai-reply-backend")
	if reference != "" {
		req.Header.Set("Idempotency-Key", "otp-"+reference)
	}

	res, err := r.client.Do(req)
	if err != nil {
		// A transport error names the URL, never the headers or the body.
		return ctx.Err() == nil, fmt.Errorf("%w: %v", ErrDelivery, err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, maxErrorBody))
		return false, nil
	}

	var failure struct {
		Name string `json:"name"`
	}
	_ = json.NewDecoder(io.LimitReader(res.Body, maxErrorBody)).Decode(&failure)
	return res.StatusCode >= 500, &DeliveryError{Status: res.StatusCode, Name: failure.Name}
}
