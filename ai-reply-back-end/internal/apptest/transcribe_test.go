package apptest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/ai"
	"github.com/aireply/ai-reply-back-end/internal/audio/audiotest"
)

var voiceSeq atomic.Int64

// voiceFields — жарамды өрістер жиыны; әр шақыруға жаңа request_id.
func voiceFields(overrides ...string) map[string]string {
	fields := map[string]string{
		"request_id":  fmt.Sprintf("voice-req-%06d", voiceSeq.Add(1)),
		"duration_ms": "3000",
		"languages":   "kk,ru",
		"purpose":     "dictation",
	}
	for i := 0; i+1 < len(overrides); i += 2 {
		if overrides[i+1] == "<absent>" {
			delete(fields, overrides[i])
			continue
		}
		fields[overrides[i]] = overrides[i+1]
	}
	return fields
}

// transcribe — multipart сұраныс: өрістер және (nil болмаса) audio бөлігі.
func (h *harness) transcribe(token string, recording []byte, fields map[string]string) response {
	h.t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for key, value := range fields {
		if err := form.WriteField(key, value); err != nil {
			h.t.Fatal(err)
		}
	}
	if recording != nil {
		part, err := form.CreateFormFile("audio", "recording.m4a")
		if err != nil {
			h.t.Fatal(err)
		}
		_, _ = part.Write(recording)
	}
	_ = form.Close()
	return h.send(http.MethodPost, "/api/v1/ai/transcribe", form.FormDataContentType(), body.Bytes(), h.auth(token))
}

// send — JSON емес денемен сұраныс.
func (h *harness) send(method, path, contentType string, body []byte, headers map[string]string) response {
	h.t.Helper()
	req, err := http.NewRequest(method, h.server.URL+path, bytes.NewReader(body))
	if err != nil {
		h.t.Fatalf("request: %v", err)
	}
	req.Header.Set("Content-Type", contentType)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	res, err := h.server.Client().Do(req)
	if err != nil {
		h.t.Fatalf("do: %v", err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	parsed := map[string]any{}
	_ = json.Unmarshal(raw, &parsed)
	return response{status: res.StatusCode, body: parsed, raw: raw, header: res.Header}
}

func (r response) strings(key string) []string {
	var out []string
	values, _ := r.body[key].([]any)
	for _, v := range values {
		s, _ := v.(string)
		out = append(out, s)
	}
	return out
}

func TestTranscribeReturnsTheTextWithoutSpendingQuota(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("+7 701 900 10 01")
	recording := audiotest.M4A(3*time.Second, nil)

	res := h.transcribe(s.access, recording, voiceFields())
	if res.status != http.StatusOK || res.str("text") != speechTranscript || !slices.Equal(res.strings("languages"), []string{"kk"}) ||
		res.num("duration_ms") != 3000 {
		t.Fatalf("transcribe: %d %s", res.status, res.raw)
	}
	calls := h.speech.requests()
	if len(calls) != 1 {
		t.Fatalf("provider calls = %d", len(calls))
	}
	c := calls[0]
	if c.fields["model"][0] != "gpt-transcribe" || c.fields["response_format"][0] != "json" ||
		!slices.Equal(c.fields["languages[]"], []string{"kk", "ru"}) ||
		c.fields["prompt"][0] != ai.VoicePrompt(ai.PurposeDictation, []string{"kk", "ru"}) || c.fields["language"] != nil {
		t.Fatalf("provider fields = %v", c.fields)
	}
	if c.fileName != "voice.m4a" || c.contentType != "audio/mp4" || !bytes.Equal(c.file, recording) {
		t.Fatalf("provider file = %q %q %d bytes", c.fileName, c.contentType, len(c.file))
	}

	// Жауап квотасы қозғалмайды; токендер ғана есептеледі.
	if usage := h.do(http.MethodGet, "/api/v1/me/usage", nil, h.auth(s.access)); usage.num("used_today") != 0 {
		t.Fatalf("transcription spent the reply quota: %s", usage.raw)
	}
	if used := h.scalar(`SELECT COALESCE(SUM(used), 0) FROM usage_daily WHERE user_id = ?`, s.userID); used != 0 {
		t.Fatalf("usage_daily.used = %d", used)
	}
	events, err := h.store.UserEvents(context.Background(), s.userID, 10)
	if err != nil || len(events) != 1 {
		t.Fatalf("events: %v %d", err, len(events))
	}
	if e := events[0]; e.Mode != "transcribe" || e.PromptVersion != "transcribe_v1" || e.Status != "success" ||
		e.SourceChars != 3 || e.Language != "kk" || e.Model != "gpt-transcribe" || e.InputTokens != 30 {
		t.Fatalf("event: %+v", e)
	}
	if !strings.Contains(h.logs.String(), `"msg":"ai_voice_transcribed"`) {
		t.Fatal("no metadata log for the transcription")
	}

	// WAV және нұсқау: өз файл аты, өз промпты.
	wav := h.transcribe(s.access, audiotest.WAV(1500*time.Millisecond),
		voiceFields("languages", "ru", "purpose", "instruction", "duration_ms", "1500"))
	if wav.status != http.StatusOK || wav.num("duration_ms") != 1500 {
		t.Fatalf("wav: %d %s", wav.status, wav.raw)
	}
	last := h.speech.requests()[1]
	if last.fileName != "voice.wav" || last.contentType != "audio/wav" ||
		last.fields["prompt"][0] != ai.VoicePrompt(ai.PurposeInstruction, []string{"ru"}) {
		t.Fatalf("wav call = %q %q %v", last.fileName, last.contentType, last.fields["prompt"])
	}
}

func TestTranscribeRequiresASessionAndConsent(t *testing.T) {
	h := newHarness(t)
	recording := audiotest.M4A(2*time.Second, nil)
	if res := h.transcribe("", recording, voiceFields()); res.status != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d %s", res.status, res.raw)
	}
	s := h.signInWithoutConsent("voice-noconsent@example.com")
	mustStatus(t, h.transcribe(s.access, recording, voiceFields()), http.StatusForbidden, "CONSENT_REQUIRED")
	if n := len(h.speech.requests()); n != 0 {
		t.Fatalf("the recording reached the provider without consent: %d", n)
	}
	h.consent(s.access)
	if res := h.transcribe(s.access, recording, voiceFields()); res.status != http.StatusOK {
		t.Fatalf("after consent: %d %s", res.status, res.raw)
	}
}

func TestTranscribeValidatesTheForm(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("+7 701 900 10 02")
	good := audiotest.M4A(2*time.Second, nil)

	notMultipart := h.do(http.MethodPost, "/api/v1/ai/transcribe", map[string]any{"audio": "x"}, h.auth(s.access))
	mustStatus(t, notMultipart, http.StatusBadRequest, "INVALID_REQUEST")

	for _, c := range []struct {
		name      string
		recording []byte
		fields    map[string]string
		field     string
		reason    string
	}{
		{"no request id", good, voiceFields("request_id", "<absent>"), "request_id", "required"},
		{"short request id", good, voiceFields("request_id", "abc"), "request_id", "format"},
		{"request id with spaces", good, voiceFields("request_id", "voice req 0001"), "request_id", "format"},
		{"no duration", good, voiceFields("duration_ms", "<absent>"), "duration_ms", "required"},
		{"duration not a number", good, voiceFields("duration_ms", "3s"), "duration_ms", "range"},
		{"duration too short", good, voiceFields("duration_ms", "299"), "duration_ms", "range"},
		{"duration too long", good, voiceFields("duration_ms", "40501"), "duration_ms", "range"},
		{"unknown purpose", good, voiceFields("purpose", "summary"), "purpose", "dictation or instruction"},
		{"oversized field", good, voiceFields("languages", strings.Repeat("kk,", 100)), "languages", "too_long"},
		{"no audio", nil, voiceFields(), "audio", "required"},
		{"ogg", append([]byte("OggS"), make([]byte, 200)...), voiceFields(), "audio", "format"},
		{"mp4 without moov", audiotest.Box("ftyp", []byte("M4A "), audiotest.U32(0)), voiceFields(), "audio", "unreadable"},
		{"too long", audiotest.M4A(42*time.Second, nil), voiceFields(), "audio", "too_long"},
		{"too long wav", audiotest.WAV(42 * time.Second), voiceFields(), "audio", "too_long"},
		{"too short", audiotest.M4A(150*time.Millisecond, nil), voiceFields(), "audio", "too_short"},
		{"too large", audiotest.M4A(10*time.Second, make([]byte, ai.MaxVoiceBytes)), voiceFields(), "audio", "too_large"},
	} {
		res := h.transcribe(s.access, c.recording, c.fields)
		if res.status != http.StatusBadRequest || res.errorCode() != "INVALID_REQUEST" ||
			res.str("error", "details", "field") != c.field || res.str("error", "details", "reason") != c.reason {
			t.Errorf("%s: %d %s", c.name, res.status, res.raw)
			continue
		}
		if c.field == "audio" && (res.num("error", "details", "max_seconds") != 40 ||
			res.num("error", "details", "max_bytes") != float64(ai.MaxVoiceBytes)) {
			t.Errorf("%s: limits missing from %s", c.name, res.raw)
		}
	}

	// Екі audio бөлігі.
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for key, value := range voiceFields() {
		_ = form.WriteField(key, value)
	}
	for i := 0; i < 2; i++ {
		part, _ := form.CreateFormFile("audio", "a.m4a")
		_, _ = part.Write(good)
	}
	_ = form.Close()
	twice := h.send(http.MethodPost, "/api/v1/ai/transcribe", form.FormDataContentType(), body.Bytes(), h.auth(s.access))
	if twice.status != http.StatusBadRequest || twice.str("error", "details", "field") != "audio" {
		t.Fatalf("two audio parts: %d %s", twice.status, twice.raw)
	}

	if n := len(h.speech.requests()); n != 0 {
		t.Fatalf("invalid requests reached the provider: %d", n)
	}
}

// Болжамдар: кіші әріп, қайталанбай, белгісізі тасталады, ең көбі үшеу; промпт — біріншісінің тілінде.
func TestTranscribeFiltersLanguageHints(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("+7 701 900 10 03")
	recording := audiotest.M4A(2*time.Second, nil)

	if res := h.transcribe(s.access, recording, voiceFields("languages", " RU, kk ,xx,ru,en,uz")); res.status != http.StatusOK {
		t.Fatalf("hints: %d %s", res.status, res.raw)
	}
	first := h.speech.requests()[0]
	if !slices.Equal(first.fields["languages[]"], []string{"ru", "kk", "en"}) ||
		first.fields["prompt"][0] != ai.VoicePrompt(ai.PurposeDictation, []string{"ru"}) {
		t.Fatalf("filtered hints = %v", first.fields)
	}

	// Белгілі тіл жоқ — болжам да, промпт та жіберілмейді.
	if res := h.transcribe(s.access, recording, voiceFields("languages", "xx,de")); res.status != http.StatusOK {
		t.Fatalf("unknown hints: %d %s", res.status, res.raw)
	}
	if second := h.speech.requests()[1]; second.fields["languages[]"] != nil || second.fields["prompt"] != nil {
		t.Fatalf("unknown hints were sent: %v", second.fields)
	}
}

// Бір request_id — бір провайдер шақыруы, қатар келсе де, кейін қайталанса да.
func TestTranscribeRequestIDPreventsDoubleCalls(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("+7 701 900 10 04")
	recording := audiotest.M4A(2*time.Second, nil)
	fields := voiceFields()

	first := h.transcribe(s.access, recording, fields)
	again := h.transcribe(s.access, recording, fields)
	if first.status != http.StatusOK || again.status != http.StatusOK || again.str("text") != first.str("text") {
		t.Fatalf("repeat: %d %s / %d %s", first.status, first.raw, again.status, again.raw)
	}
	if n := len(h.speech.requests()); n != 1 {
		t.Fatalf("a repeated request_id called the provider %d times", n)
	}

	// Қатар: екінші сұраныс біріншісі провайдерде тұрғанда келеді.
	concurrent := voiceFields()
	entered, release := h.speech.holdNext()
	var wg sync.WaitGroup
	results := make([]response, 2)
	wg.Add(1)
	go func() { defer wg.Done(); results[0] = h.transcribe(s.access, recording, concurrent) }()
	<-entered
	wg.Add(1)
	go func() { defer wg.Done(); results[1] = h.transcribe(s.access, recording, concurrent) }()
	time.Sleep(50 * time.Millisecond) // екінші сұраныс жүріп жатқан шақыруды күтсін
	close(release)
	wg.Wait()
	for i, res := range results {
		if res.status != http.StatusOK || res.str("text") != speechTranscript {
			t.Fatalf("concurrent %d: %d %s", i, res.status, res.raw)
		}
	}
	if n := len(h.speech.requests()); n != 2 {
		t.Fatalf("provider calls after the concurrent pair = %d, want 2", n)
	}

	// Жаңа id не басқа қолданушы — жаңа шақыру.
	other := h.signIn("+7 701 900 10 05")
	h.transcribe(other.access, recording, fields)
	h.transcribe(s.access, recording, voiceFields())
	if n := len(h.speech.requests()); n != 4 {
		t.Fatalf("provider calls = %d, want 4", n)
	}
	if events := h.scalar(`SELECT COUNT(*) FROM ai_usage_events WHERE user_id = ? AND mode = 'transcribe'`, s.userID); events != 3 {
		t.Fatalf("transcribe events = %d, want 3 (repeats are not recorded)", events)
	}
}

// OpenAI 400 → бір қайталау болжамсыз; қателер жауап жолындағы кодтарға түседі және есте қалмайды.
func TestTranscribeProviderFailures(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("+7 701 900 10 06")
	recording := audiotest.M4A(2*time.Second, nil)

	h.speech.respond(speechReply{http.StatusBadRequest, `{"error":{"message":"bad language"}}`})
	if res := h.transcribe(s.access, recording, voiceFields()); res.status != http.StatusOK || res.str("text") != speechTranscript {
		t.Fatalf("after one 400: %d %s", res.status, res.raw)
	}
	calls := h.speech.requests()
	if len(calls) != 2 || calls[0].fields["languages[]"] == nil || calls[1].fields["languages[]"] != nil ||
		calls[1].fields["prompt"] != nil {
		t.Fatalf("retry without hints: %d calls", len(calls))
	}

	failed := voiceFields()
	h.speech.respond(speechReply{http.StatusInternalServerError, `{}`})
	mustStatus(t, h.transcribe(s.access, recording, failed), http.StatusBadGateway, "AI_PROVIDER_UNAVAILABLE")
	// Қате сақталмайды: сол id қайта жіберілсе, провайдер қайта шақырылады.
	if res := h.transcribe(s.access, recording, failed); res.status != http.StatusOK {
		t.Fatalf("retry of a failed id: %d %s", res.status, res.raw)
	}

	h.speech.respond(speechReply{http.StatusTooManyRequests, `{}`})
	mustStatus(t, h.transcribe(s.access, recording, voiceFields()), http.StatusTooManyRequests, "RATE_LIMITED")
	h.speech.respond(speechReply{http.StatusBadRequest, `{}`}, speechReply{http.StatusBadRequest, `{}`})
	mustStatus(t, h.transcribe(s.access, recording, voiceFields()), http.StatusBadGateway, "AI_PROVIDER_UNAVAILABLE")

	if n := h.scalar(`SELECT COUNT(*) FROM ai_usage_events WHERE user_id = ? AND mode = 'transcribe' AND status = 'error'
		AND error_code IN ('AI_PROVIDER_UNAVAILABLE', 'RATE_LIMITED')`, s.userID); n != 3 {
		t.Fatalf("error events = %d, want 3", n)
	}
	if !strings.Contains(h.logs.String(), `"msg":"ai_voice_failed"`) {
		t.Fatal("no failure log")
	}
}

// Тәуліктік шек: 429 VOICE_LIMIT_REACHED; жауап квотасына әсер жоқ; ертеңіне қайта ашылады.
func TestTranscribeDailyCap(t *testing.T) {
	h := newHarness(t, withEnv("LIMIT_TRANSCRIBE_PER_DAY", "2"))
	s := h.signIn("+7 701 900 10 07")
	recording := audiotest.M4A(2*time.Second, nil)

	first := voiceFields()
	for i := 0; i < 2; i++ {
		fields := first
		if i > 0 {
			fields = voiceFields()
		}
		if res := h.transcribe(s.access, recording, fields); res.status != http.StatusOK {
			t.Fatalf("transcription %d: %d %s", i+1, res.status, res.raw)
		}
	}
	capped := h.transcribe(s.access, recording, voiceFields())
	mustStatus(t, capped, http.StatusTooManyRequests, "VOICE_LIMIT_REACHED")
	// Сервер күні (DEFAULT_TIMEZONE): келесі түн ортасы.
	resetsAt := time.Date(2026, 3, 11, 0, 0, 0, 0, h.cfg.App.Location()).Format(time.RFC3339)
	if capped.num("error", "details", "daily_limit") != 2 || capped.str("error", "details", "resets_at") != resetsAt {
		t.Fatalf("cap details: %s", capped.raw)
	}
	if n := len(h.speech.requests()); n != 2 {
		t.Fatalf("provider calls = %d", n)
	}
	// Аяқталған жазбаның қайталауы шекке тірелмейді.
	if res := h.transcribe(s.access, recording, first); res.status != http.StatusOK {
		t.Fatalf("repeat of a finished id past the cap: %d %s", res.status, res.raw)
	}
	// Жауап генерациясы бұрынғыдай жұмыс істейді.
	if res := h.generate(s.access, "Сәлеметсіз бе, бағасы қанша?"); res.status != http.StatusOK || res.num("usage", "used_today") != 1 {
		t.Fatalf("reply after the voice cap: %d %s", res.status, res.raw)
	}

	h.clock.Advance(24 * time.Hour)
	s = h.refresh(s)
	if res := h.transcribe(s.access, recording, voiceFields()); res.status != http.StatusOK {
		t.Fatalf("next day: %d %s", res.status, res.raw)
	}
}

// Минуттық шелек өзінікі: жауап генерациясының шелегін жемейді.
func TestTranscribeHasItsOwnRateLimit(t *testing.T) {
	h := newHarness(t, withEnv("RATE_TRANSCRIBE_PER_MINUTE", "1"), withEnv("RATE_AI_PER_MINUTE", "1"))
	s := h.signIn("+7 701 900 10 08")
	recording := audiotest.M4A(2*time.Second, nil)

	if res := h.transcribe(s.access, recording, voiceFields()); res.status != http.StatusOK {
		t.Fatalf("first: %d %s", res.status, res.raw)
	}
	mustStatus(t, h.transcribe(s.access, recording, voiceFields()), http.StatusTooManyRequests, "RATE_LIMITED")
	if res := h.generate(s.access, "Сәлеметсіз бе, бағасы қанша?"); res.status != http.StatusOK {
		t.Fatalf("transcription used up the reply bucket: %d %s", res.status, res.raw)
	}
}

func TestConfigAnnouncesVoiceTranscription(t *testing.T) {
	h := newHarness(t)
	features, _ := h.do(http.MethodGet, "/api/v1/config", nil, nil).body["features"].(map[string]any)
	if features["voice_transcription"] != true {
		t.Fatalf("features = %v", features)
	}

	off := newHarness(t, withEnv("TRANSCRIBE_ENABLED", "false"))
	features, _ = off.do(http.MethodGet, "/api/v1/config", nil, nil).body["features"].(map[string]any)
	if features["voice_transcription"] != false {
		t.Fatalf("disabled features = %v", features)
	}
	s := off.signIn("+7 701 900 10 09")
	mustStatus(t, off.transcribe(s.access, audiotest.M4A(2*time.Second, nil), voiceFields()), http.StatusNotFound, "NOT_FOUND")
	if n := len(off.speech.requests()); n != 0 {
		t.Fatalf("disabled endpoint reached the provider: %d", n)
	}
}

// Жазба да, мәтін де дерекқорға, журналға және әкімші JSON-ына түспейді; әкімші графигі оны генерация деп санамайды.
func TestVoiceIsNeverPersisted(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("+7 701 900 10 10")
	transcript := "ҚҰПИЯ ДАУЫС тапсырыс ORDER-55120 дайын"
	audioMarker := "AUDIO-MARKER-77341-SECRET"
	h.speech.respond(speechReply{http.StatusOK, `{"text":"` + transcript + `","languages":[{"code":"kk"}]}`})

	res := h.transcribe(s.access, audiotest.M4A(2*time.Second, []byte(audioMarker)), voiceFields())
	if res.status != http.StatusOK || res.str("text") != transcript {
		t.Fatalf("transcribe: %d %s", res.status, res.raw)
	}
	if !bytes.Contains(h.speech.requests()[0].file, []byte(audioMarker)) {
		t.Fatal("the recording did not reach the provider — the test would prove nothing")
	}
	h.generate(s.access, "Сәлеметсіз бе, бағасы қанша?")

	admin := h.signInAdmin()
	detail := h.do(http.MethodGet, "/api/v1/admin/users/"+s.userID, nil, admin.headers(h.cfg.Admin.CookieName))
	if detail.status != http.StatusOK || !strings.Contains(string(detail.raw), `"transcribe":true`) {
		t.Fatalf("admin detail: %d %s", detail.status, detail.raw)
	}
	dashboard := h.do(http.MethodGet, "/api/v1/admin/dashboard?range=30d", nil, admin.headers(h.cfg.Admin.CookieName))
	if dashboard.num("stats", "requests_range") != 1 || dashboard.num("stats", "succeeded") != 1 {
		t.Fatalf("transcription counted as a generation: %s", dashboard.raw)
	}
	for _, payload := range []string{string(detail.raw), string(dashboard.raw)} {
		if strings.Contains(payload, transcript) || strings.Contains(payload, "ORDER-55120") {
			t.Fatal("the admin API exposes the transcript")
		}
	}

	logs := h.logs.String()
	for _, needle := range []string{transcript, "ORDER-55120", audioMarker} {
		if strings.Contains(logs, needle) {
			t.Fatalf("logs contain %q", needle)
		}
	}
	for _, needle := range []string{transcript, "ORDER-55120", audioMarker} {
		if h.dbContains(needle) {
			t.Fatalf("database contains %q", needle)
		}
	}
}
