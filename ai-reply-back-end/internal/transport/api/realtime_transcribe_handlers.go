package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/ai"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/transport/httpx"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func (s *Server) realtimeVoiceEnabled() bool {
	return s.voiceEnabled() && s.cfg.AI.RealtimeTranscribeEnabled && s.ai.CanRealtimeTranscribe()
}

type realtimeVoiceSource struct{ conn *websocket.Conn }

func (s realtimeVoiceSource) Read(ctx context.Context) (ai.RealtimeInput, error) {
	var input ai.RealtimeInput
	if err := wsjson.Read(ctx, s.conn, &input); err != nil {
		return input, err
	}
	return input, nil
}

// The native client authenticates the HTTP upgrade with the same access token
// as /ai/transcribe. Browser origins must be same-origin (the library default).
// Never accept a token in the URL, a caller-selected model, or arbitrary upstream
// events. No audio is written to disk and no transcript goes to access logs.
func (s *Server) handleRealtimeTranscribe(w http.ResponseWriter, r *http.Request) {
	if !s.realtimeVoiceEnabled() {
		httpx.Fail(w, domain.ErrNotFound)
		return
	}
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(128 << 10)
	ctx, cancel := context.WithTimeout(r.Context(), 70*time.Second)
	defer cancel()
	send := func(event ai.RealtimeEvent) error { return wsjson.Write(ctx, conn, event) }
	fail := func(err error) {
		if errors.Is(err, context.Canceled) {
			return
		}
		_, code, _ := httpx.Translate(err)
		_ = send(ai.RealtimeEvent{Type: "error", Code: code})
	}
	var start struct {
		Type      string   `json:"type"`
		RequestID string   `json:"request_id"`
		Languages []string `json:"languages"`
	}
	startCtx, startCancel := context.WithTimeout(ctx, 5*time.Second)
	err = wsjson.Read(startCtx, conn, &start)
	startCancel()
	if err != nil {
		fail(domain.ErrInvalidRequest)
		return
	}
	if start.Type != "start" || !voiceRequestID.MatchString(start.RequestID) || len(start.Languages) > ai.MaxVoiceLanguages {
		fail(domain.ErrInvalidRequest)
		return
	}
	// Filtering uses the existing small supported language allowlist.
	languages := ai.VoiceLanguages(strings.Join(start.Languages, ","))
	user, _ := UserFrom(r.Context())
	result, err := s.ai.RealtimeTranscribe(ctx, ai.TranscribeRequest{User: user, DeviceID: DeviceFrom(r.Context()),
		RequestID: start.RequestID, Languages: languages, Purpose: ai.PurposeInstruction}, realtimeVoiceSource{conn}, send)
	if err != nil {
		fail(err)
		return
	}
	_ = send(ai.RealtimeEvent{Type: "completed", Text: result.Text})
	_ = conn.Close(websocket.StatusNormalClosure, "")
}
