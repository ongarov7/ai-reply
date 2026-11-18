package ai

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/config"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type realtimeTestSource struct{ values []RealtimeInput }

func (s *realtimeTestSource) Read(ctx context.Context) (RealtimeInput, error) {
	if len(s.values) == 0 {
		<-ctx.Done()
		return RealtimeInput{}, ctx.Err()
	}
	v := s.values[0]
	s.values = s.values[1:]
	return v, nil
}

func TestRealtimeSpeechConfiguresTranscriptionAndCommitsOneTurn(t *testing.T) {
	var received []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/realtime" || r.URL.Query().Get("intent") != "transcription" || r.URL.Query().Has("model") || r.Header.Get("Authorization") != "Bearer server-secret" {
			t.Error("incorrect realtime handshake")
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		conn.SetReadLimit(128 << 10)
		ctx := r.Context()
		var update map[string]any
		if err := wsjson.Read(ctx, conn, &update); err != nil {
			t.Error(err)
			return
		}
		received = append(received, update)
		_ = wsjson.Write(ctx, conn, map[string]string{"type": "session.updated"})
		for {
			var event map[string]any
			if err := wsjson.Read(ctx, conn, &event); err != nil {
				return
			}
			received = append(received, event)
			if event["type"] == "input_audio_buffer.commit" {
				break
			}
		}
		_ = wsjson.Write(ctx, conn, map[string]string{"type": "input_audio_buffer.committed", "item_id": "one"})
		// A final for another turn must not finish this request.
		_ = wsjson.Write(ctx, conn, map[string]string{"type": "conversation.item.input_audio_transcription.completed", "item_id": "wrong", "transcript": "wrong text"})
		_ = wsjson.Write(ctx, conn, map[string]string{"type": "conversation.item.input_audio_transcription.delta", "item_id": "one", "delta": "Сәлем"})
		_ = wsjson.Write(ctx, conn, map[string]string{"type": "conversation.item.input_audio_transcription.completed", "item_id": "one", "transcript": "  Сәлем!  "})
	}))
	defer server.Close()
	p := NewOpenAI(config.OpenAI{BaseURL: server.URL, APIKey: "server-secret", RealtimeTranscribeModel: "gpt-live-transcribe"})
	source := &realtimeTestSource{values: []RealtimeInput{{Type: "audio", Audio: base64.StdEncoding.EncodeToString(make([]byte, 24000))}, {Type: "commit"}}}
	var events []RealtimeEvent
	out, duration, err := p.RealtimeTranscribe(context.Background(), []string{"ru", "kk", "en"}, source, func(e RealtimeEvent) error { events = append(events, e); return nil })
	if err != nil || out.Text != "Сәлем!" || duration != 500*time.Millisecond {
		t.Fatalf("result: %+v %s %v", out, duration, err)
	}
	if !reflect.DeepEqual(events, []RealtimeEvent{{Type: "ready"}, {Type: "delta", Text: "Сәлем"}}) {
		t.Fatalf("events: %+v", events)
	}
	if len(received) != 3 {
		t.Fatalf("provider events: %+v", received)
	}
	session := received[0]["session"].(map[string]any)
	input := session["audio"].(map[string]any)["input"].(map[string]any)
	transcription := input["transcription"].(map[string]any)
	if session["type"] != "transcription" || input["turn_detection"] != nil || transcription["model"] != "gpt-live-transcribe" || transcription["language"] != nil || !reflect.DeepEqual(transcription["languages"], []any{"ru", "kk", "en"}) {
		t.Fatalf("bad config: %+v", received[0])
	}
}

func TestRealtimeSpeechRejectsInvalidOrExcessAudio(t *testing.T) {
	for _, tt := range []struct {
		name  string
		input []RealtimeInput
	}{
		{"bad base64", []RealtimeInput{{Type: "audio", Audio: "%%%"}}},
		{"odd PCM", []RealtimeInput{{Type: "audio", Audio: "AA=="}}},
		{"empty commit", []RealtimeInput{{Type: "commit"}}},
		{"arbitrary event", []RealtimeInput{{Type: "session.update"}}},
		{"oversize frame", []RealtimeInput{{Type: "audio", Audio: base64.StdEncoding.EncodeToString(make([]byte, 65538))}}},
		{"duration", func() []RealtimeInput {
			var values []RealtimeInput
			for i := 0; i < 31; i++ {
				values = append(values, RealtimeInput{Type: "audio", Audio: base64.StdEncoding.EncodeToString(make([]byte, 64000))})
			}
			return values
		}()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := websocket.Accept(w, r, nil)
				if err != nil {
					return
				}
				defer conn.CloseNow()
				conn.SetReadLimit(128 << 10)
				var v map[string]any
				if wsjson.Read(r.Context(), conn, &v) != nil {
					return
				}
				_ = wsjson.Write(r.Context(), conn, map[string]string{"type": "session.updated"})
				for wsjson.Read(r.Context(), conn, &v) == nil {
				}
			}))
			defer server.Close()
			p := NewOpenAI(config.OpenAI{BaseURL: server.URL, RealtimeTranscribeModel: "gpt-live-transcribe"})
			_, _, err := p.RealtimeTranscribe(context.Background(), nil, &realtimeTestSource{values: tt.input}, func(RealtimeEvent) error { return nil })
			if !errors.Is(err, domain.ErrInvalidRequest) {
				t.Fatalf("expected invalid audio, got %v", err)
			}
		})
	}
}
