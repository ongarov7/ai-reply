package apptest

import (
	"bytes"
	"context"
	"encoding/base64"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func serveFakeRealtime(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	if r.Header.Get("Authorization") != "Bearer sk-test-key" || r.URL.Query().Get("intent") != "transcription" {
		t.Error("bad upstream realtime authentication")
		return
	}
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	var v map[string]any
	if wsjson.Read(r.Context(), conn, &v) != nil {
		return
	}
	_ = wsjson.Write(r.Context(), conn, map[string]string{"type": "session.updated"})
	for {
		if wsjson.Read(r.Context(), conn, &v) != nil {
			return
		}
		if v["type"] == "input_audio_buffer.commit" {
			break
		}
	}
	_ = wsjson.Write(r.Context(), conn, map[string]string{"type": "input_audio_buffer.committed", "item_id": "voice-turn"})
	_ = wsjson.Write(r.Context(), conn, map[string]string{"type": "conversation.item.input_audio_transcription.delta", "item_id": "voice-turn", "delta": "Сәлем!"})
	_ = wsjson.Write(r.Context(), conn, map[string]string{"type": "conversation.item.input_audio_transcription.completed", "item_id": "voice-turn", "transcript": speechTranscript})
}

func liveClient(t *testing.T, h *harness, token string) *websocket.Conn {
	t.Helper()
	header := http.Header{}
	for key, value := range h.auth(token) {
		header.Set(key, value)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, strings.Replace(h.server.URL, "http:", "ws:", 1)+"/api/v1/ai/transcribe/realtime", &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	return conn
}

func liveTurn(t *testing.T, h *harness, token, id string) map[string]any {
	t.Helper()
	conn := liveClient(t, h, token)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = wsjson.Write(ctx, conn, map[string]any{"type": "start", "request_id": id, "languages": []string{"ru", "kk", "en"}})
	var event map[string]any
	if err := wsjson.Read(ctx, conn, &event); err != nil {
		t.Fatal(err)
	}
	if event["type"] == "ready" {
		_ = wsjson.Write(ctx, conn, map[string]string{"type": "audio", "audio": base64.StdEncoding.EncodeToString(make([]byte, 24000))})
		_ = wsjson.Write(ctx, conn, map[string]string{"type": "commit"})
		for event["type"] != "completed" && event["type"] != "error" {
			if err := wsjson.Read(ctx, conn, &event); err != nil {
				t.Fatal(err)
			}
		}
	}
	return event
}

func TestRealtimeTranscriptionUsesVoiceCapIdempotencyAndNoReplyQuota(t *testing.T) {
	h := newHarness(t, withEnv("LIMIT_TRANSCRIBE_PER_DAY", "1"))
	s := h.signIn("+7 701 900 39 01")
	first := liveTurn(t, h, s.access, "live-test-0001")
	if first["type"] != "completed" || first["text"] != speechTranscript {
		t.Fatalf("final: %+v", first)
	}
	again := liveTurn(t, h, s.access, "live-test-0001")
	if again["type"] != "completed" || again["text"] != speechTranscript {
		t.Fatalf("replay: %+v", again)
	}
	capped := liveTurn(t, h, s.access, "live-test-0002")
	if capped["type"] != "error" || capped["code"] != "VOICE_LIMIT_REACHED" {
		t.Fatalf("cap: %+v", capped)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM ai_usage_events WHERE user_id = ? AND mode = 'transcribe'`, s.userID); n != 1 {
		t.Fatalf("voice events: %d", n)
	}
	if n := h.scalar(`SELECT COALESCE(SUM(used), 0) FROM usage_daily WHERE user_id = ?`, s.userID); n != 0 {
		t.Fatalf("reply usage: %d", n)
	}
	if strings.Contains(h.logs.String(), speechTranscript) {
		t.Fatal("private transcript in logs")
	}
}

func TestRealtimeTranscriptionRequiresAuthConsentAndEnabledFeature(t *testing.T) {
	h := newHarness(t)
	mustStatus(t, h.do(http.MethodGet, "/api/v1/ai/transcribe/realtime", nil, nil), http.StatusUnauthorized, "UNAUTHORIZED")
	s := h.signInWithoutConsent("live-no-consent@example.com")
	mustStatus(t, h.do(http.MethodGet, "/api/v1/ai/transcribe/realtime", nil, h.auth(s.access)), http.StatusForbidden, "CONSENT_REQUIRED")
	off := newHarness(t, withEnv("REALTIME_TRANSCRIBE_ENABLED", "false"))
	signed := off.signIn("+7 701 900 39 02")
	mustStatus(t, off.do(http.MethodGet, "/api/v1/ai/transcribe/realtime", nil, off.auth(signed.access)), http.StatusNotFound, "NOT_FOUND")
}

// A hijacked WebSocket handler may finish logging after the client reads its
// final event. Protect the fixture's log snapshots while that handler exits.
type synchronizedLogBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *synchronizedLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(p)
}
func (b *synchronizedLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}
