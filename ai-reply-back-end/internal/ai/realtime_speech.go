package ai

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// RealtimeInput is the deliberately small public protocol. It cannot override
// the provider, model, credentials, prompt, format or server limits.
type RealtimeInput struct {
	Type  string `json:"type"`
	Audio string `json:"audio,omitempty"`
}
type RealtimeEvent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	Code string `json:"code,omitempty"`
}
type RealtimeSource interface {
	Read(context.Context) (RealtimeInput, error)
}
type realtimeTranscriber interface {
	RealtimeModel() string
	RealtimeTranscribe(context.Context, []string, RealtimeSource, func(RealtimeEvent) error) (Transcription, time.Duration, error)
}

func (o *OpenAI) RealtimeModel() string { return o.cfg.RealtimeTranscribeModel }
func (s *Service) CanRealtimeTranscribe() bool {
	p, ok := s.transcriber.(realtimeTranscriber)
	return ok && p.RealtimeModel() != ""
}

// RealtimeTranscribe reuses the same account/device checks, daily voice cap,
// usage events and successful request-id cache as file transcription. Streaming
// has no reply charge: the existing generation endpoint charges the reply.
func (s *Service) RealtimeTranscribe(ctx context.Context, req TranscribeRequest, source RealtimeSource,
	emit func(RealtimeEvent) error) (TranscribeResult, error) {
	provider, ok := s.transcriber.(realtimeTranscriber)
	if !ok || provider.RealtimeModel() == "" {
		return TranscribeResult{}, domain.ErrNotFound
	}
	entitlement, err := s.subs.Entitlement(ctx, req.User.ID)
	if err != nil {
		return TranscribeResult{}, err
	}
	if s.proDevices != nil {
		if err := s.proDevices.Authorize(ctx, req.User, req.DeviceID, entitlement); err != nil {
			if !purchasedDeviceFallback(entitlement, err) || entitlement.Balance.Packs <= 0 {
				return TranscribeResult{}, err
			}
		}
	}
	result, reused, err := s.voice.do(ctx, req.User.ID+"\x00"+req.RequestID, func() (TranscribeResult, error) {
		if _, loaded := s.liveUsers.LoadOrStore(req.User.ID, struct{}{}); loaded {
			return TranscribeResult{}, domain.ErrRateLimited
		}
		defer s.liveUsers.Delete(req.User.ID)
		started := s.clock.Now()
		if err := s.voiceCapReached(ctx, req.User.ID, started); err != nil {
			return TranscribeResult{}, err
		}
		out, duration, err := provider.RealtimeTranscribe(ctx, req.Languages, source, emit)
		recordCtx, cancel := bookkeeping(ctx)
		defer cancel()
		language := ""
		if len(req.Languages) > 0 {
			language = req.Languages[0]
		}
		c := call{Mode: ModeTranscribe, User: req.User, DeviceID: req.DeviceID, Language: language,
			Platform: req.User.Platform, AppVersion: req.User.AppVersion, Chars: int((duration + time.Second - 1) / time.Second)}
		status, code := "success", ""
		if err != nil {
			status, code = "error", errorCode(err)
		}
		s.record(recordCtx, c, entitlement, status, code, PromptVersionTranscribe,
			Completion{Model: provider.RealtimeModel(), InputTokens: out.InputTokens, OutputTokens: out.OutputTokens, ProviderMS: out.ProviderMS},
			int(s.clock.Now().Sub(started).Milliseconds()))
		// Metadata only. In particular no provider error text, transcript or audio.
		s.log.Info("ai_voice_realtime", "user_id", req.User.ID, "status", status, "code", code, "duration_ms", duration.Milliseconds())
		if err != nil {
			return TranscribeResult{}, err
		}
		return TranscribeResult{Text: out.Text, Languages: []string{}, Duration: duration}, nil
	})
	result.Reused = reused
	return result, err
}

const realtimePCMBytesPerSecond = 24000 * 2

func realtimeURL(base string) (string, error) {
	u, err := url.Parse(strings.TrimRight(base, "/") + "/realtime")
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	default:
		return "", domain.ErrProviderDown
	}
	q := u.Query()
	q.Set("intent", "transcription")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// The client performs VAD; gpt-live-transcribe does not accept server_vad.
func (o *OpenAI) RealtimeTranscribe(ctx context.Context, languages []string, source RealtimeSource,
	emit func(RealtimeEvent) error) (Transcription, time.Duration, error) {
	ctx, cancel := context.WithTimeout(ctx, 65*time.Second)
	defer cancel()
	endpoint, err := realtimeURL(o.cfg.BaseURL)
	if err != nil {
		return Transcription{}, 0, domain.ErrProviderDown
	}
	headers := http.Header{"Authorization": []string{"Bearer " + o.cfg.APIKey}}
	setupCtx, setupCancel := context.WithTimeout(ctx, 10*time.Second)
	conn, response, err := websocket.Dial(setupCtx, endpoint, &websocket.DialOptions{HTTPHeader: headers, HTTPClient: o.audio})
	if err != nil {
		setupCancel()
		if response != nil && response.StatusCode == http.StatusTooManyRequests {
			return Transcription{}, 0, domain.ErrRateLimited
		}
		return Transcription{}, 0, transportError(ctx, err)
	}
	defer conn.CloseNow()
	conn.SetReadLimit(256 << 10)
	config := map[string]any{"model": o.cfg.RealtimeTranscribeModel, "delay": "low"}
	if len(languages) > 0 {
		config["languages"] = languages
	}
	update := map[string]any{"type": "session.update", "session": map[string]any{"type": "transcription", "audio": map[string]any{"input": map[string]any{
		"format": map[string]any{"type": "audio/pcm", "rate": 24000}, "transcription": config, "turn_detection": nil,
	}}}}
	if err := wsjson.Write(setupCtx, conn, update); err != nil {
		setupCancel()
		return Transcription{}, 0, transportError(ctx, err)
	}
	type providerEvent struct {
		Type       string `json:"type"`
		ItemID     string `json:"item_id"`
		Delta      string `json:"delta"`
		Transcript string `json:"transcript"`
	}
	for {
		var event providerEvent
		if err := wsjson.Read(setupCtx, conn, &event); err != nil {
			setupCancel()
			return Transcription{}, 0, transportError(ctx, err)
		}
		if event.Type == "error" {
			setupCancel()
			return Transcription{}, 0, domain.ErrProviderDown
		}
		if event.Type == "session.updated" || event.Type == "transcription_session.updated" {
			break
		}
	}
	setupCancel()
	if err := emit(RealtimeEvent{Type: "ready"}); err != nil {
		return Transcription{}, 0, err
	}
	type sent struct {
		duration time.Duration
		err      error
	}
	inputDone := make(chan sent, 1)
	go func() {
		bytes := 0
		finish := func(err error) {
			inputDone <- sent{time.Duration(bytes) * time.Second / realtimePCMBytesPerSecond, err}
			if err != nil {
				cancel()
			}
		}
		for {
			input, err := source.Read(ctx)
			if err != nil {
				finish(err)
				return
			}
			switch input.Type {
			case "audio":
				// At most 64 KiB of decoded audio in a frame, and 40 seconds total.
				if len(input.Audio) > 87384 {
					finish(InvalidAudio("too_large"))
					return
				}
				chunk, err := base64.StdEncoding.Strict().DecodeString(input.Audio)
				if len(chunk) > 64<<10 {
					finish(InvalidAudio("too_large"))
					return
				}
				if err != nil || len(chunk) == 0 || len(chunk)%2 != 0 {
					finish(InvalidAudio("format"))
					return
				}
				bytes += len(chunk)
				if bytes > MaxVoiceSeconds*realtimePCMBytesPerSecond {
					finish(InvalidAudio("too_long"))
					return
				}
				if err := wsjson.Write(ctx, conn, map[string]string{"type": "input_audio_buffer.append", "audio": input.Audio}); err != nil {
					finish(transportError(ctx, err))
					return
				}
			case "commit":
				if bytes < realtimePCMBytesPerSecond/5 {
					finish(InvalidAudio("too_short"))
					return
				}
				err := wsjson.Write(ctx, conn, map[string]string{"type": "input_audio_buffer.commit"})
				finish(err)
				return
			case "cancel":
				finish(context.Canceled)
				return
			default:
				finish(domain.ErrInvalidRequest)
				return
			}
		}
	}()
	duration := time.Duration(0)
	committedItem := ""
	for {
		var event providerEvent
		err := wsjson.Read(ctx, conn, &event)
		if inputDone != nil {
			select {
			case in := <-inputDone:
				duration = in.duration
				inputDone = nil
				if in.err != nil {
					return Transcription{}, duration, in.err
				}
			default:
			}
		}
		if err != nil {
			return Transcription{}, duration, transportError(ctx, err)
		}
		switch event.Type {
		case "input_audio_buffer.committed":
			committedItem = event.ItemID
		case "conversation.item.input_audio_transcription.delta":
			if len(event.Delta) > 64<<10 {
				return Transcription{}, duration, domain.ErrProviderDown
			}
			if err := emit(RealtimeEvent{Type: "delta", Text: event.Delta}); err != nil {
				return Transcription{}, duration, err
			}
		case "conversation.item.input_audio_transcription.completed":
			if committedItem == "" || event.ItemID != committedItem {
				continue
			}
			if len(event.Transcript) > 64<<10 {
				return Transcription{}, duration, domain.ErrProviderDown
			}
			if inputDone != nil {
				in := <-inputDone
				duration = in.duration
				if in.err != nil {
					return Transcription{}, duration, in.err
				}
			}
			return Transcription{Text: strings.TrimSpace(event.Transcript), Model: o.cfg.RealtimeTranscribeModel}, duration, nil
		case "conversation.item.input_audio_transcription.failed", "error":
			return Transcription{}, duration, domain.ErrProviderDown
		}
	}
}
