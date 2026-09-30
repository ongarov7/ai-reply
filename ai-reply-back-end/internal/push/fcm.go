package push

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// FCM әдепкі мекенжайлары.
const (
	GoogleTokenURL = "https://oauth2.googleapis.com/token"
	FCMEndpoint    = "https://fcm.googleapis.com"
	fcmScope       = "https://www.googleapis.com/auth/firebase.messaging"
)

// Android арналары: қосымша дәл осы идентификаторлармен жасайды.
const (
	AndroidChannelGeneral   = "general"
	AndroidChannelImportant = "important"
)

// FCMConfig — Firebase қызметтік тіркелгісі.
type FCMConfig struct {
	ProjectID   string
	ClientEmail string
	PrivateKey  string // PEM
	// TokenURL and Endpoint are overridden only in tests.
	TokenURL   string
	Endpoint   string
	HTTPClient *http.Client
	Now        func() time.Time
}

// FCM — Firebase Cloud Messaging HTTP v1 провайдері.
type FCM struct {
	cfg    FCMConfig
	key    *rsa.PrivateKey
	client *http.Client
	now    func() time.Time

	mu          sync.Mutex
	accessToken string
	expiresAt   time.Time
}

// NewFCM — кілтті тексеріп, провайдер жасайды. Қате мәтінінде кілт мазмұны болмайды.
func NewFCM(cfg FCMConfig) (*FCM, error) {
	if strings.TrimSpace(cfg.ProjectID) == "" || strings.TrimSpace(cfg.ClientEmail) == "" {
		return nil, errors.New("fcm: FIREBASE_PROJECT_ID and FIREBASE_CLIENT_EMAIL are required")
	}
	if !strings.Contains(cfg.ClientEmail, "@") {
		return nil, errors.New("fcm: FIREBASE_CLIENT_EMAIL must be the service account e-mail")
	}
	key, err := parseRSAKey(cfg.PrivateKey)
	if err != nil {
		return nil, errors.New("fcm: FIREBASE_PRIVATE_KEY is not a valid service-account private key")
	}
	if cfg.TokenURL == "" {
		cfg.TokenURL = GoogleTokenURL
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = FCMEndpoint
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				Proxy:               http.ProxyFromEnvironment,
				ForceAttemptHTTP2:   true,
				MaxIdleConnsPerHost: 16,
				IdleConnTimeout:     90 * time.Second,
				TLSHandshakeTimeout: 10 * time.Second,
			},
		}
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &FCM{cfg: cfg, key: key, client: client, now: now}, nil
}

// Name — провайдер аты.
func (f *FCM) Name() string { return "fcm" }

type fcmRequest struct {
	Message fcmMessage `json:"message"`
}

type fcmMessage struct {
	Token        string            `json:"token"`
	Notification *fcmNotification  `json:"notification,omitempty"`
	Data         map[string]string `json:"data,omitempty"`
	Android      *fcmAndroid       `json:"android,omitempty"`
}

type fcmNotification struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

type fcmAndroid struct {
	Priority     string                  `json:"priority,omitempty"`
	TTL          string                  `json:"ttl,omitempty"`
	Notification *fcmAndroidNotification `json:"notification,omitempty"`
}

type fcmAndroidNotification struct {
	ChannelID    string `json:"channel_id,omitempty"`
	Tag          string `json:"tag,omitempty"`
	DefaultSound bool   `json:"default_sound,omitempty"`
}

// buildFCMMessage — HTTP v1 денесі. Тестте де, өлшем тексерісінде де қолданылады.
//
// A notification + data message: while the app is in the background the FCM
// SDK shows it on the channel named here and puts the data keys into the
// launch intent; in the foreground the app's FirebaseMessagingService shows
// it itself with the same channel and tag. The tag is the notification id, so
// a repeated delivery replaces the first one instead of stacking.
func buildFCMMessage(token string, msg Message) fcmRequest {
	priority, channel := "NORMAL", AndroidChannelGeneral
	if msg.Important {
		priority, channel = "HIGH", AndroidChannelImportant
	}
	android := &fcmAndroid{
		Priority: priority,
		Notification: &fcmAndroidNotification{
			ChannelID: channel, Tag: msg.CollapseID, DefaultSound: true,
		},
	}
	if msg.TTL > 0 {
		android.TTL = fmt.Sprintf("%ds", int64(msg.TTL/time.Second))
	}
	return fcmRequest{Message: fcmMessage{
		Token:        token,
		Notification: &fcmNotification{Title: msg.Title, Body: msg.Body},
		Data:         msg.Data,
		Android:      android,
	}}
}

// Send — бір құрылғыға бір хабарлама.
func (f *FCM) Send(ctx context.Context, target Target, msg Message) Result {
	access, err := f.token(ctx)
	if err != nil {
		return Result{Outcome: Retry, Code: "OAUTH_TOKEN_UNAVAILABLE", Detail: safeDetail(err.Error())}
	}
	body, err := json.Marshal(buildFCMMessage(target.Token, msg))
	if err != nil {
		return Result{Outcome: Rejected, Code: "PAYLOAD_ENCODING", Detail: "payload could not be encoded"}
	}
	endpoint := strings.TrimRight(f.cfg.Endpoint, "/") + "/v1/projects/" + url.PathEscape(f.cfg.ProjectID) + "/messages:send"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Result{Outcome: Rejected, Code: "REQUEST", Detail: "request could not be built"}
	}
	req.Header.Set("Authorization", "Bearer "+access)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	res, err := f.client.Do(req)
	if err != nil {
		return transportFailure(ctx, err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 64*1024))

	if res.StatusCode == http.StatusOK {
		var ok struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(raw, &ok)
		return Result{Outcome: Accepted, MessageID: ok.Name, StatusCode: res.StatusCode}
	}
	result := ClassifyFCM(res.StatusCode, raw, res.Header.Get("Retry-After"), f.now())
	if res.StatusCode == http.StatusUnauthorized {
		f.forgetToken() // the cached OAuth token was refused: fetch a new one next time
	}
	return result
}

type fcmError struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
		Details []struct {
			Type            string `json:"@type"`
			ErrorCode       string `json:"errorCode"`
			FieldViolations []struct {
				Field string `json:"field"`
			} `json:"fieldViolations"`
		} `json:"details"`
	} `json:"error"`
}

// ClassifyFCM — HTTP v1 қатесін жіктейді.
//
// https://firebase.google.com/docs/reference/fcm/rest/v1/ErrorCode
//
//	UNREGISTERED (404), SENDER_ID_MISMATCH (403), INVALID_ARGUMENT on
//	message.token (400)            → InvalidToken
//	QUOTA_EXCEEDED (429), UNAVAILABLE (503), INTERNAL (500),
//	UNAUTHENTICATED (401), network → Retry
//	INVALID_ARGUMENT on the payload, other 4xx → Rejected
func ClassifyFCM(status int, body []byte, retryAfterHeader string, now time.Time) Result {
	var parsed fcmError
	_ = json.Unmarshal(body, &parsed)
	code := parsed.Error.Status
	tokenField := false
	for _, d := range parsed.Error.Details {
		if strings.HasSuffix(d.Type, "google.firebase.fcm.v1.FcmError") && d.ErrorCode != "" {
			code = d.ErrorCode
		}
		for _, v := range d.FieldViolations {
			if v.Field == "message.token" {
				tokenField = true
			}
		}
	}
	result := Result{
		Code:       code,
		StatusCode: status,
		Detail:     statusDetail(status, code),
		RetryAfter: retryAfter(retryAfterHeader, now),
	}
	switch {
	case code == "UNREGISTERED", code == "SENDER_ID_MISMATCH":
		result.Outcome = InvalidToken
	case code == "INVALID_ARGUMENT" && tokenField:
		result.Outcome = InvalidToken
	case code == "QUOTA_EXCEEDED", code == "UNAVAILABLE", code == "INTERNAL",
		code == "UNAUTHENTICATED", code == "THIRD_PARTY_AUTH_ERROR":
		result.Outcome = Retry
	case status == http.StatusTooManyRequests, status == http.StatusUnauthorized, status >= 500:
		result.Outcome = Retry
	default:
		result.Outcome = Rejected
	}
	if result.Code == "" {
		result.Code = fmt.Sprintf("HTTP_%d", status)
	}
	return result
}

// token — OAuth access token (кэштеледі, мерзімі бітуге 5 минут қалғанда жаңарады).
func (f *FCM) token(ctx context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := f.now()
	if f.accessToken != "" && now.Before(f.expiresAt.Add(-5*time.Minute)) {
		return f.accessToken, nil
	}

	assertion, err := f.assertion(now)
	if err != nil {
		return "", err
	}
	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
	form.Set("assertion", assertion)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := f.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("oauth token request failed: %s", safeDetail(err.Error()))
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 64*1024))
	if res.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		return "", fmt.Errorf("oauth token refused: HTTP %d %s", res.StatusCode, e.Error)
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
	f.accessToken = tok.AccessToken
	f.expiresAt = now.Add(time.Duration(tok.ExpiresIn) * time.Second)
	return f.accessToken, nil
}

func (f *FCM) forgetToken() {
	f.mu.Lock()
	f.accessToken = ""
	f.mu.Unlock()
}

// assertion — RS256 JWT, Google оны access token-ге айырбастайды.
func (f *FCM) assertion(now time.Time) (string, error) {
	header, err := jwtSegment(map[string]string{"alg": "RS256", "typ": "JWT"})
	if err != nil {
		return "", err
	}
	claims, err := jwtSegment(map[string]any{
		"iss":   f.cfg.ClientEmail,
		"sub":   f.cfg.ClientEmail,
		"scope": fcmScope,
		"aud":   f.cfg.TokenURL,
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	})
	if err != nil {
		return "", err
	}
	signing := header + "." + claims
	digest := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, f.key, crypto.SHA256, digest[:])
	if err != nil {
		return "", errors.New("oauth assertion could not be signed")
	}
	return signing + "." + b64url(sig), nil
}
