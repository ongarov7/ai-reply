package main

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/legal"
)

// The website cabinet endpoints (docs/web-cabinet-ai-context.md §5.4): the
// browser's cookie session, the same-origin rule on every write, the AI
// context, the usage chart, consent and profile dictation.

func cabinetJSON(t *testing.T, w *httptest.ResponseRecorder, into any) {
	t.Helper()
	if err := json.Unmarshal(w.Body.Bytes(), into); err != nil {
		t.Fatalf("decode %d %s: %v", w.Code, w.Body.String(), err)
	}
}

func TestWebCabinetAIContextUsageAndConsent(t *testing.T) {
	cfg := webTestConfig(t)
	app := webTestApplication(t, cfg)
	origin := "https://billing.example.invalid"
	cookie, signedIn := webTestSignIn(t, app.handler, "", "cabinet@example.invalid")
	me := signedIn.User.ID

	var session struct {
		Features struct {
			AIContext          bool `json:"ai_context"`
			VoiceTranscription bool `json:"voice_transcription"`
		} `json:"features"`
		Legal struct {
			ConsentRequired bool   `json:"consent_required"`
			TermsVersion    string `json:"terms_version"`
			PrivacyVersion  string `json:"privacy_version"`
			TermsURL        string `json:"terms_url"`
		} `json:"legal"`
	}
	w := webTestRequest(app.handler, http.MethodGet, "/account/api/session", nil, cookie, "", "")
	cabinetJSON(t, w, &session)
	if w.Code != http.StatusOK || !session.Features.AIContext || !session.Legal.ConsentRequired ||
		session.Legal.TermsVersion != legal.TermsVersion || session.Legal.PrivacyVersion != legal.PrivacyVersion ||
		!strings.HasSuffix(session.Legal.TermsURL, "/offer") {
		t.Fatalf("session additions: %d %s", w.Code, w.Body.String())
	}

	// The AI context through the cookie session.
	if w := webTestRequest(app.handler, http.MethodGet, "/account/api/ai-context", nil, nil, "", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous read: %d", w.Code)
	}
	var doc struct {
		Revision int64 `json:"revision"`
		Cost     struct {
			General int `json:"general"`
			Client  int `json:"client"`
		} `json:"cost"`
	}
	w = webTestRequest(app.handler, http.MethodGet, "/account/api/ai-context", nil, cookie, "", "")
	cabinetJSON(t, w, &doc)
	if w.Code != http.StatusOK || doc.Revision != 0 || doc.Cost.General != 1 || !strings.Contains(w.Body.String(), `"user_id":"`+me+`"`) {
		t.Fatalf("empty document: %d %s", w.Code, w.Body.String())
	}
	body := map[string]any{"expected_revision": 0, "expected_user_id": me, "context": map[string]any{
		"enabled": true, "roles": []string{"blogger"}, "occupation": "Блогер", "traits": []string{"humor"},
		"about": strings.Repeat("Пишу о путешествиях. ", 20), "audience": "Instagram 40 тыс.",
		"recipients": map[string]any{"client": map[string]any{"notes": "Рекламодателям — официально"}},
	}}
	if w := webTestRequest(app.handler, http.MethodPost, "/account/api/ai-context", body, cookie, "https://evil.example", ""); w.Code != http.StatusForbidden {
		t.Fatalf("cross-site write: %d %s", w.Code, w.Body.String())
	}
	// A tab still showing another account must not write into this one.
	for _, expected := range []any{"another-account", nil} {
		stale := map[string]any{"expected_revision": 0, "context": body["context"]}
		if expected != nil {
			stale["expected_user_id"] = expected
		}
		if w := webTestRequest(app.handler, http.MethodPost, "/account/api/ai-context", stale, cookie, origin, ""); w.Code != http.StatusConflict ||
			!strings.Contains(w.Body.String(), "WEB_ACCOUNT_CHANGED") {
			t.Fatalf("save for %v: %d %s", expected, w.Code, w.Body.String())
		}
	}
	w = webTestRequest(app.handler, http.MethodPost, "/account/api/ai-context", body, cookie, origin, "")
	cabinetJSON(t, w, &doc)
	if w.Code != http.StatusOK || doc.Revision != 1 || doc.Cost.General != 3 || doc.Cost.Client != 3 {
		t.Fatalf("save: %d %s", w.Code, w.Body.String())
	}
	if w := webTestRequest(app.handler, http.MethodPost, "/account/api/ai-context", body, cookie, origin, ""); w.Code != http.StatusConflict ||
		!strings.Contains(w.Body.String(), "AI_CONTEXT_CHANGED") {
		t.Fatalf("stale save: %d %s", w.Code, w.Body.String())
	}

	// The usage chart: exactly N days, oldest first, today last.
	var history struct {
		Days []struct {
			Date    string `json:"date"`
			Replies int    `json:"replies"`
			Units   int    `json:"units"`
		} `json:"days"`
		Platforms []any  `json:"platforms"`
		Timezone  string `json:"timezone"`
	}
	w = webTestRequest(app.handler, http.MethodGet, "/account/api/usage", nil, cookie, "", "")
	cabinetJSON(t, w, &history)
	if !strings.Contains(w.Body.String(), `"user_id":"`+me+`"`) {
		t.Fatalf("usage history does not name its account: %s", w.Body.String())
	}
	today := time.Now().In(cfg.App.Location()).Format("2006-01-02")
	if w.Code != http.StatusOK || len(history.Days) != 30 || history.Days[29].Date != today || history.Platforms == nil || history.Timezone == "" {
		t.Fatalf("usage: %d %s", w.Code, w.Body.String())
	}
	for i := 1; i < len(history.Days); i++ {
		if history.Days[i-1].Date >= history.Days[i].Date {
			t.Fatalf("days out of order: %v", history.Days)
		}
	}
	for path, status := range map[string]int{"/account/api/usage?days=7": 200, "/account/api/usage?days=90": 200,
		"/account/api/usage?days=6": 400, "/account/api/usage?days=91": 400, "/account/api/usage?days=x": 400} {
		if w := webTestRequest(app.handler, http.MethodGet, path, nil, cookie, "", ""); w.Code != status {
			t.Fatalf("%s: %d, want %d", path, w.Code, status)
		}
	}

	// Dictation: multipart with the cabinet's headers only, after consent.
	upload := func(origin, requestedWith string, expected ...string) *httptest.ResponseRecorder {
		var buf bytes.Buffer
		form := multipart.NewWriter(&buf)
		account := me
		if len(expected) > 0 {
			account = expected[0]
		}
		_ = form.WriteField("expected_user_id", account)
		_ = form.WriteField("request_id", "web-voice-0001")
		_ = form.WriteField("duration_ms", "1200")
		_ = form.WriteField("languages", "ru,kk,en")
		_ = form.WriteField("purpose", "dictation")
		part, _ := form.CreateFormFile("audio", "voice.wav")
		_, _ = part.Write([]byte("RIFF"))
		_ = form.Close()
		r := httptest.NewRequest(http.MethodPost, "https://billing.example.invalid/account/api/transcribe", &buf)
		r.Header.Set("Content-Type", form.FormDataContentType())
		r.Header.Set("Origin", origin)
		r.Header.Set("X-Requested-With", requestedWith)
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		app.handler.ServeHTTP(w, r)
		return w
	}
	if w := upload("https://evil.example", "AIReplyAccount"); w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "WEB_ORIGIN_REQUIRED") {
		t.Fatalf("cross-site upload: %d %s", w.Code, w.Body.String())
	}
	if w := upload(origin, ""); w.Code != http.StatusForbidden {
		t.Fatalf("upload without X-Requested-With: %d", w.Code)
	}
	if session.Features.VoiceTranscription {
		if w := upload(origin, "AIReplyAccount"); w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "CONSENT_REQUIRED") {
			t.Fatalf("upload before consent: %d %s", w.Code, w.Body.String())
		}
	} else if w := upload(origin, "AIReplyAccount"); w.Code != http.StatusNotFound {
		t.Fatalf("voice off: %d %s", w.Code, w.Body.String())
	}

	// Consent from the cabinet (platform web).
	if w := webTestRequest(app.handler, http.MethodPost, "/account/api/consent", map[string]string{"terms_version": "old", "privacy_version": legal.PrivacyVersion, "locale": "ru", "expected_user_id": me}, cookie, origin, ""); w.Code != http.StatusBadRequest {
		t.Fatalf("outdated terms accepted: %d", w.Code)
	}
	if w := webTestRequest(app.handler, http.MethodPost, "/account/api/consent", map[string]string{"terms_version": legal.TermsVersion, "privacy_version": legal.PrivacyVersion, "locale": "ru", "expected_user_id": "another-account"}, cookie, origin, ""); w.Code != http.StatusConflict {
		t.Fatalf("consent for another account: %d", w.Code)
	}
	w = webTestRequest(app.handler, http.MethodPost, "/account/api/consent", map[string]string{"terms_version": legal.TermsVersion, "privacy_version": legal.PrivacyVersion, "locale": "ru", "expected_user_id": me}, cookie, origin, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"platform":"web"`) {
		t.Fatalf("consent: %d %s", w.Code, w.Body.String())
	}
	w = webTestRequest(app.handler, http.MethodGet, "/account/api/session", nil, cookie, "", "")
	cabinetJSON(t, w, &session)
	if session.Legal.ConsentRequired {
		t.Fatal("consent still required after accepting")
	}
	if session.Features.VoiceTranscription {
		// Past the consent check: the four bytes are not a recording.
		if w := upload(origin, "AIReplyAccount"); w.Code != http.StatusBadRequest {
			t.Fatalf("invalid audio after consent: %d %s", w.Code, w.Body.String())
		}
		if w := upload(origin, "AIReplyAccount", "another-account"); w.Code != http.StatusConflict {
			t.Fatalf("dictation for another account: %d %s", w.Code, w.Body.String())
		}
	}
}

func TestWebHandoffCanOpenTheCabinet(t *testing.T) {
	app := webTestApplication(t, webTestConfig(t))
	native := handoffNativeSignIn(t, app, "handoff-cabinet@example.invalid")
	request := func(target string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(map[string]string{"expected_user_id": native.User.ID, "locale": "ru", "target": target})
		r := httptest.NewRequest(http.MethodPost, "https://billing.example.invalid/api/v1/web/payment-handoff", bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+native.Access)
		w := httptest.NewRecorder()
		app.handler.ServeHTTP(w, r)
		return w
	}
	w := request("account")
	var result struct {
		URL string `json:"url"`
	}
	cabinetJSON(t, w, &result)
	u, err := url.Parse(result.URL)
	if w.Code != http.StatusOK || err != nil || u.Path != "/account" || !strings.HasPrefix(u.Fragment, "handoff=") {
		t.Fatalf("cabinet handoff: %d %s", w.Code, w.Body.String())
	}
	if w := request("payment"); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "/payment#handoff=") {
		t.Fatalf("payment handoff: %d %s", w.Code, w.Body.String())
	}
	if w := request("admin"); w.Code != http.StatusBadRequest {
		t.Fatalf("unknown target: %d %s", w.Code, w.Body.String())
	}
}
