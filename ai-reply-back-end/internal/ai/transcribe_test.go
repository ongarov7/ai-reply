package ai

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/config"
	"github.com/aireply/ai-reply-back-end/internal/audio"
	"github.com/aireply/ai-reply-back-end/internal/audio/audiotest"
	"github.com/aireply/ai-reply-back-end/internal/domain"
)

// speechCall — жалған /audio/transcriptions серверіне келген бір сұраныс.
type speechCall struct {
	path, auth  string
	fields      map[string][]string
	fileName    string
	contentType string
	file        []byte
}

// fakeSpeech — httptest сервері: келгенін жазады, жауаптарды кезекпен береді.
type fakeSpeech struct {
	mu      sync.Mutex
	calls   []speechCall
	replies []fakeReply
	delay   time.Duration
}

type fakeReply struct {
	status int
	body   string
}

const speechOK = `{"text":"  Сәлем, ертең хабарласамын.  ","languages":[{"code":"kk"},{"code":"RU"},{"code":"kk"}],"usage":{"type":"tokens","input_tokens":50,"output_tokens":9}}`

func newFakeSpeech(t *testing.T, replies ...fakeReply) (*fakeSpeech, *OpenAI) {
	t.Helper()
	f := &fakeSpeech{replies: replies}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := speechCall{path: r.URL.Path, auth: r.Header.Get("Authorization")}
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			t.Errorf("multipart: %v", err)
		}
		call.fields = r.MultipartForm.Value
		if files := r.MultipartForm.File["file"]; len(files) == 1 {
			call.fileName = files[0].Filename
			call.contentType = files[0].Header.Get("Content-Type")
			file, _ := files[0].Open()
			call.file, _ = io.ReadAll(file)
			_ = file.Close()
		}
		f.mu.Lock()
		f.calls = append(f.calls, call)
		reply := fakeReply{status: http.StatusOK, body: speechOK}
		if len(f.replies) > 0 {
			reply, f.replies = f.replies[0], f.replies[1:]
		}
		delay := f.delay
		f.mu.Unlock()
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(reply.status)
		_, _ = io.WriteString(w, reply.body)
	}))
	t.Cleanup(server.Close)
	client := NewOpenAI(config.OpenAI{APIKey: "sk-test", Model: "test-model", BaseURL: server.URL,
		Timeout: time.Second, TranscribeModel: "gpt-transcribe"})
	return f, client
}

func (f *fakeSpeech) recorded() []speechCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]speechCall(nil), f.calls...)
}

func TestTranscribeSendsTheRecordingWithHints(t *testing.T) {
	speech, client := newFakeSpeech(t)
	recording := audiotest.M4A(3*time.Second, []byte("voice-bytes"))
	prompt := VoicePrompt(PurposeDictation, []string{"kk", "ru"})

	out, err := client.Transcribe(context.Background(), Audio{Data: recording, Format: audio.FormatM4A,
		Languages: []string{"kk", "ru"}, Prompt: prompt})
	if err != nil {
		t.Fatal(err)
	}
	if out.Text != "Сәлем, ертең хабарласамын." || !slices.Equal(out.Languages, []string{"kk", "ru"}) ||
		out.Model != "gpt-transcribe" || out.InputTokens != 50 || out.OutputTokens != 9 || out.Retried {
		t.Fatalf("transcription = %+v", out)
	}

	calls := speech.recorded()
	if len(calls) != 1 {
		t.Fatalf("calls = %d", len(calls))
	}
	c := calls[0]
	if c.path != "/audio/transcriptions" || c.auth != "Bearer sk-test" {
		t.Fatalf("path %q auth %q", c.path, c.auth)
	}
	if c.fields["model"][0] != "gpt-transcribe" || c.fields["response_format"][0] != "json" ||
		!slices.Equal(c.fields["languages[]"], []string{"kk", "ru"}) || c.fields["prompt"][0] != prompt {
		t.Fatalf("fields = %v", c.fields)
	}
	if _, sent := c.fields["language"]; sent {
		t.Fatal("the singular language field must never be sent with languages[]")
	}
	if c.fileName != "voice.m4a" || c.contentType != "audio/mp4" || !bytes.Equal(c.file, recording) {
		t.Fatalf("file %q %q (%d bytes)", c.fileName, c.contentType, len(c.file))
	}

	// WAV — өз аты мен түрі; болжамсыз — languages[] де, prompt та жоқ.
	if _, err := client.Transcribe(context.Background(), Audio{Data: audiotest.WAV(time.Second), Format: audio.FormatWAV}); err != nil {
		t.Fatal(err)
	}
	wav := speech.recorded()[1]
	if wav.fileName != "voice.wav" || wav.contentType != "audio/wav" || wav.fields["languages[]"] != nil || wav.fields["prompt"] != nil {
		t.Fatalf("wav call = %q %q %v", wav.fileName, wav.contentType, wav.fields)
	}
}

// 400 → дәл бір қайталау, languages[] мен prompt-сыз.
func TestTranscribeRetriesOnceWithoutHintsAfter400(t *testing.T) {
	speech, client := newFakeSpeech(t, fakeReply{http.StatusBadRequest, `{"error":{"message":"unsupported language"}}`})
	in := Audio{Data: audiotest.M4A(time.Second, nil), Format: audio.FormatM4A, Languages: []string{"uz", "ru"},
		Prompt: VoicePrompt(PurposeDictation, []string{"uz"})}
	out, err := client.Transcribe(context.Background(), in)
	if err != nil || !out.Retried || out.Text == "" {
		t.Fatalf("retry: %+v %v", out, err)
	}
	calls := speech.recorded()
	if len(calls) != 2 || len(calls[0].fields["languages[]"]) != 2 || calls[1].fields["languages[]"] != nil ||
		calls[1].fields["prompt"] != nil || !bytes.Equal(calls[1].file, in.Data) {
		t.Fatalf("calls = %+v", calls)
	}

	// Екінші 400 — қате, үшінші әрекет жоқ.
	speech, client = newFakeSpeech(t, fakeReply{http.StatusBadRequest, `{}`}, fakeReply{http.StatusBadRequest, `{}`})
	if _, err := client.Transcribe(context.Background(), in); !errors.Is(err, domain.ErrProviderDown) {
		t.Fatalf("two 400s: %v", err)
	}
	if n := len(speech.recorded()); n != 2 {
		t.Fatalf("calls after two 400s = %d, want exactly 2", n)
	}

	// Тастайтын болжам жоқ — қайталау мағынасыз.
	speech, client = newFakeSpeech(t, fakeReply{http.StatusBadRequest, `{}`})
	if _, err := client.Transcribe(context.Background(), Audio{Data: in.Data, Format: audio.FormatM4A}); !errors.Is(err, domain.ErrProviderDown) {
		t.Fatalf("400 without hints: %v", err)
	}
	if n := len(speech.recorded()); n != 1 {
		t.Fatalf("calls = %d", n)
	}
}

// Провайдер қателері жауап жолындағы түрлерге түседі.
func TestTranscribeMapsUpstreamFailures(t *testing.T) {
	in := Audio{Data: audiotest.M4A(time.Second, nil), Format: audio.FormatM4A, Languages: []string{"ru"}}
	for _, c := range []struct {
		reply fakeReply
		want  error
	}{
		{fakeReply{http.StatusInternalServerError, `{}`}, domain.ErrProviderDown},
		{fakeReply{http.StatusUnauthorized, `{}`}, domain.ErrProviderDown},
		{fakeReply{http.StatusTooManyRequests, `{}`}, domain.ErrRateLimited},
		{fakeReply{http.StatusOK, `not json`}, domain.ErrProviderDown},
	} {
		speech, client := newFakeSpeech(t, c.reply)
		if _, err := client.Transcribe(context.Background(), in); !errors.Is(err, c.want) {
			t.Errorf("%d %q: %v, want %v", c.reply.status, c.reply.body, err, c.want)
		}
		if n := len(speech.recorded()); n != 1 {
			t.Errorf("%d: %d calls, only a 400 is retried", c.reply.status, n)
		}
	}

	slow, client := newFakeSpeech(t)
	slow.delay = 2 * time.Second
	client.speechTimeout = 50 * time.Millisecond
	if _, err := client.Transcribe(context.Background(), in); !errors.Is(err, domain.ErrProviderTimeout) {
		t.Fatalf("slow provider: %v", err)
	}

	// Клиент кетті — сұраныс тоқтайды, уақыт біткені емес.
	slow, client = newFakeSpeech(t)
	slow.delay = 2 * time.Second
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(30*time.Millisecond, cancel)
	if _, err := client.Transcribe(ctx, in); !errors.Is(err, domain.ErrProviderDown) {
		t.Fatalf("cancelled: %v", err)
	}

	// Бос мәтін — қате емес; languages өрісінің басқа пішіні елеусіз.
	_, client = newFakeSpeech(t, fakeReply{http.StatusOK, `{"text":"  ","languages":"kazakh"}`})
	if out, err := client.Transcribe(context.Background(), in); err != nil || out.Text != "" || out.Languages == nil || len(out.Languages) != 0 {
		t.Fatalf("empty transcript: %+v %v", out, err)
	}
	_, client = newFakeSpeech(t, fakeReply{http.StatusOK, `{"text":"ok","languages":["kk-KZ","x1",""]}`})
	if out, _ := client.Transcribe(context.Background(), in); !slices.Equal(out.Languages, []string{"kk"}) {
		t.Fatalf("string languages: %v", out.Languages)
	}
}

func TestVoiceLanguagesFiltering(t *testing.T) {
	for raw, want := range map[string][]string{
		"":                       nil,
		"kk":                     {"kk"},
		" RU , kk,xx,ru, en, uz": {"ru", "kk", "en"},
		"kk-KZ,ru_RU,tr,de":      {"kk", "ru"},
		",,,":                    nil,
		"uz,UZ,uz":               {"uz"},
	} {
		if got := VoiceLanguages(raw); !slices.Equal(got, want) {
			t.Errorf("%q → %v, want %v", raw, got, want)
		}
	}
}

// Промпт — тек сипаттама, бірінші болжамның тілінде; аудару не қайта жазу туралы сөз жоқ.
func TestVoicePromptDescribesAndNeverAsksToRewrite(t *testing.T) {
	if VoicePrompt(PurposeDictation, nil) != "" {
		t.Fatal("no hints, no prompt")
	}
	kk := VoicePrompt(PurposeDictation, []string{"kk", "ru"})
	if kk != "Мессенджерге арналған қысқа дауыстық хабарлама. Сөйлеу қазақша, кейде орысша сөздер араласады. Қазақ әріптері: ә, ғ, қ, ң, ө, ұ, ү, һ, і." {
		t.Fatalf("kk dictation = %q", kk)
	}
	if ru := VoicePrompt("", []string{"ru", "kk"}); !strings.HasPrefix(ru, "Короткое голосовое сообщение") {
		t.Fatalf("ru default purpose = %q", ru)
	}
	for _, purpose := range []string{PurposeDictation, PurposeInstruction} {
		for _, lang := range []string{"kk", "ru", "en", "uz"} {
			p := VoicePrompt(purpose, []string{lang})
			if p == "" || len([]rune(p)) > 200 {
				t.Errorf("%s/%s: %q", purpose, lang, p)
			}
			lower := strings.ToLower(p)
			for _, banned := range []string{"translat", "rewrite", "перевед", "перепиш", "аудар", "tarjima"} {
				if strings.Contains(lower, banned) {
					t.Errorf("%s/%s asks to %q: %q", purpose, lang, banned, p)
				}
			}
			if lang == "kk" {
				for _, letter := range []string{"ә", "ғ", "қ", "ң", "ө", "ұ", "ү", "һ", "і"} {
					if !strings.Contains(p, letter) {
						t.Errorf("%s/kk is missing %q", purpose, letter)
					}
				}
			}
		}
	}
	if VoicePrompt(PurposeInstruction, []string{"ru"}) == VoicePrompt(PurposeDictation, []string{"ru"}) {
		t.Fatal("the instruction prompt must differ from dictation")
	}
}

// Қатар келген бір кілт — бір шақыру; қате есте қалмайды; мерзімі өтсе — жаңа шақыру.
func TestVoiceCacheRunsOneCallPerKey(t *testing.T) {
	now := time.Date(2026, 3, 10, 9, 0, 0, 0, time.UTC)
	cache := newVoiceCache(10*time.Minute, 100, func() time.Time { return now })
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})

	var wg sync.WaitGroup
	results := make([]TranscribeResult, 4)
	reused := make([]bool, 4)
	wg.Add(1)
	go func() {
		defer wg.Done()
		results[0], reused[0], _ = cache.do(context.Background(), "u1\x00req-1", func() (TranscribeResult, error) {
			calls.Add(1)
			close(started)
			<-release
			return TranscribeResult{Text: "бір рет"}, nil
		})
	}()
	<-started
	for i := 1; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], reused[i], _ = cache.do(context.Background(), "u1\x00req-1", func() (TranscribeResult, error) {
				calls.Add(1)
				return TranscribeResult{Text: "екінші шақыру"}, nil
			})
		}(i)
	}
	time.Sleep(20 * time.Millisecond) // күтушілер жүріп жатқан шақыруға тіркелсін
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("calls = %d", calls.Load())
	}
	for i, r := range results {
		if r.Text != "бір рет" || reused[i] != (i > 0) {
			t.Fatalf("result %d = %+v reused %v", i, r, reused[i])
		}
	}

	// Басқа қолданушының сол id-і — бөлек кілт.
	if r, again, _ := cache.do(context.Background(), "u2\x00req-1", func() (TranscribeResult, error) {
		return TranscribeResult{Text: "басқа"}, nil
	}); r.Text != "басқа" || again {
		t.Fatalf("other user: %+v", r)
	}

	// Қате сақталмайды: келесі әрекет қайта шақырады.
	fail := func() (TranscribeResult, error) { return TranscribeResult{}, domain.ErrProviderDown }
	if _, _, err := cache.do(context.Background(), "u1\x00req-2", fail); !errors.Is(err, domain.ErrProviderDown) {
		t.Fatalf("failure: %v", err)
	}
	if r, again, err := cache.do(context.Background(), "u1\x00req-2", func() (TranscribeResult, error) {
		return TranscribeResult{Text: "қайта"}, nil
	}); err != nil || again || r.Text != "қайта" {
		t.Fatalf("after a failure: %+v %v %v", r, again, err)
	}

	// 10 минуттан кейін нәтиже ұмытылады.
	now = now.Add(10*time.Minute + time.Second)
	if _, again, _ := cache.do(context.Background(), "u1\x00req-1", func() (TranscribeResult, error) {
		return TranscribeResult{Text: "жаңа"}, nil
	}); again {
		t.Fatal("an expired result was reused")
	}
}

// Шақырушының клиенті кетсе, күтуші шақыруды өзі қайталайды.
func TestVoiceCacheWaiterTakesOverAnAbandonedCall(t *testing.T) {
	cache := newVoiceCache(time.Minute, 100, time.Now)
	leaderCtx, leave := context.WithCancel(context.Background())
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, _, err := cache.do(leaderCtx, "k", func() (TranscribeResult, error) {
			close(started)
			<-leaderCtx.Done()
			return TranscribeResult{}, domain.ErrProviderDown
		})
		done <- err
	}()
	<-started
	waiter := make(chan TranscribeResult, 1)
	go func() {
		r, _, _ := cache.do(context.Background(), "k", func() (TranscribeResult, error) {
			return TranscribeResult{Text: "күтуші"}, nil
		})
		waiter <- r
	}()
	time.Sleep(20 * time.Millisecond)
	leave()
	if err := <-done; !errors.Is(err, domain.ErrProviderDown) {
		t.Fatalf("leader: %v", err)
	}
	if r := <-waiter; r.Text != "күтуші" {
		t.Fatalf("waiter = %+v", r)
	}
}

// Шекке жеткенде ең ескі аяқталған нәтиже шығады; жүріп жатқаны ешқашан.
func TestVoiceCacheIsBounded(t *testing.T) {
	now := time.Date(2026, 3, 10, 9, 0, 0, 0, time.UTC)
	cache := newVoiceCache(time.Hour, 3, func() time.Time { return now })
	ok := func(text string) func() (TranscribeResult, error) {
		return func() (TranscribeResult, error) { return TranscribeResult{Text: text}, nil }
	}
	for i, key := range []string{"a", "b", "c", "d", "e"} {
		now = now.Add(time.Duration(i) * time.Second)
		_, _, _ = cache.do(context.Background(), key, ok(key))
	}
	if n := cache.size(); n != 3 {
		t.Fatalf("size = %d, want 3", n)
	}
	if _, again, _ := cache.do(context.Background(), "a", ok("a2")); again {
		t.Fatal("the oldest result should have been evicted")
	}
	if _, again, _ := cache.do(context.Background(), "e", ok("e2")); !again {
		t.Fatal("the newest result must stay")
	}

	// Мерзімі өткендер тазаланады.
	now = now.Add(2 * time.Hour)
	_, _, _ = cache.do(context.Background(), "f", ok("f"))
	if n := cache.size(); n != 1 {
		t.Fatalf("size after expiry = %d, want 1", n)
	}
}
