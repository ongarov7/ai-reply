package ai

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/audio"
	"github.com/aireply/ai-reply-back-end/internal/domain"
)

// VOICE. The keyboard records a short voice note — the message itself
// (dictation) or a note for the assistant on how to answer (instruction) —
// and gets the text back to review and edit; nothing is generated from it
// until the person taps a button. The recording is checked by its own bytes
// (M4A or WAV, at most 40 seconds of audio), sent to the speech-to-text model
// and dropped: it is never written to disk, the database or the log, and
// neither is the transcript. Like polish it spends no reply quota; it has a
// per-minute bucket and a per-day cap of its own, and a usage event
// (mode=transcribe) records the seconds, latency and outcome.

// Дауыс жазбасының шектері.
const (
	// MaxVoiceSeconds — клиент жазатын ең ұзын белсенді дауыс.
	MaxVoiceSeconds = 40
	// MaxVoiceBytes — бір жазбаның ең үлкен өлшемі (4 MiB).
	MaxVoiceBytes = 4 << 20
	// MaxVoiceLanguages — провайдерге кететін тіл болжамдарының ең көп саны.
	MaxVoiceLanguages = 3

	// Контейнердегі ұзақтық: 40 с + бір секунд кодек пен таймер айырмасына.
	minVoiceDuration = 200 * time.Millisecond
	maxVoiceDuration = (MaxVoiceSeconds + 1) * time.Second
)

// Дауыс жазбасының мақсаты: промпттың мазмұнын ғана өзгертеді.
const (
	// PurposeDictation — хабарламаның өзі (әдепкі).
	PurposeDictation = "dictation"
	// PurposeInstruction — ассистентке нұсқау: хабарламаға қалай жауап беру керек.
	PurposeInstruction = "instruction"
)

// TranscribeRequest — бір дауыс жазбасы. Аудио тек жадта, тек осы шақыру ішінде.
type TranscribeRequest struct {
	User     domain.User
	DeviceID string
	// RequestID — клиенттің идемпотенттік кілті (тексерілген пішін).
	RequestID string
	Audio     []byte
	// ClientDurationMS — клиент айтқан белсенді ұзақтық (тек журналға).
	ClientDurationMS int
	// Languages — VoiceLanguages арқылы тексерілген болжамдар, ең ықтималы бірінші.
	Languages []string
	Purpose   string
}

// TranscribeResult — клиентке қайтатын нәтиже.
type TranscribeResult struct {
	Text      string
	Languages []string
	// Duration — контейнерден оқылған ұзақтық.
	Duration time.Duration
	// Reused — нәтиже сол request_id-дің бұрынғы не қатар жүрген сұранысынан алынды.
	Reused bool
}

// WithTranscriber — дауысты мәтінге айналдыратын провайдер (nil — мүмкіндік жоқ).
func (s *Service) WithTranscriber(t Transcriber) *Service { s.transcriber = t; return s }

// WithTranscribeDailyLimit — дауыс шегі бір қолданушыға күніне (LIMIT_TRANSCRIBE_PER_DAY).
func (s *Service) WithTranscribeDailyLimit(n int) *Service { s.transcribePerDay = n; return s }

// CanTranscribe — дауыс провайдері қосылған ба.
func (s *Service) CanTranscribe() bool { return s.transcriber != nil }

// VoiceLanguages — клиенттің тіл болжамдары (үтірмен, ең ықтималы бірінші) →
// kk | ru | en | uz: кіші әріппен, қайталанбай, ең көбі MaxVoiceLanguages.
// Белгісіз код үнсіз тасталады; "kk-KZ" сияқты аймақ белгісі алынып тасталады.
func VoiceLanguages(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		code := domain.NormalizePreferredLanguage(part)
		if code == "" || slices.Contains(out, code) {
			continue
		}
		if out = append(out, code); len(out) == MaxVoiceLanguages {
			break
		}
	}
	return out
}

// voicePrompts — провайдерге қысқа контекст: не айтылды және қай тілде.
//
// Only a description, written in the language it describes: nothing asks the
// model to translate, rewrite, tidy or answer, so it writes down what it
// hears. The Kazakh lines list the Kazakh letters, which speech models
// otherwise tend to replace with their Russian look-alikes.
var voicePrompts = map[string]map[string]string{
	PurposeDictation: {
		"kk": "Мессенджерге арналған қысқа дауыстық хабарлама. Сөйлеу қазақша, кейде орысша сөздер араласады. Қазақ әріптері: ә, ғ, қ, ң, ө, ұ, ү, һ, і.",
		"ru": "Короткое голосовое сообщение для мессенджера. Речь на русском, иногда с казахскими словами.",
		"en": "A short voice message for a messenger chat, spoken in English.",
		"uz": "Messenjer uchun qisqa ovozli xabar. Nutq oʻzbek tilida, baʼzan ruscha soʻzlar aralashadi.",
	},
	PurposeInstruction: {
		"kk": "AI көмекшіге арналған қысқа дауыстық нұсқау: хабарламаға қалай жауап беру керек. Сөйлеу қазақша, кейде орысша сөздер араласады. Қазақ әріптері: ә, ғ, қ, ң, ө, ұ, ү, һ, і.",
		"ru": "Короткое голосовое указание для ИИ-ассистента: как ответить на сообщение. Речь на русском, иногда с казахскими словами.",
		"en": "A short spoken note for an AI assistant on how to reply to a message, in English.",
		"uz": "AI yordamchi uchun qisqa ovozli koʻrsatma: xabarga qanday javob berish kerak. Nutq oʻzbek tilida, baʼzan ruscha soʻzlar aralashadi.",
	},
}

// VoicePrompt — бірінші болжамның тіліндегі контекст ("" — болжам жоқ: модель өзі анықтайды).
func VoicePrompt(purpose string, languages []string) string {
	if len(languages) == 0 {
		return ""
	}
	if purpose != PurposeInstruction {
		purpose = PurposeDictation
	}
	return voicePrompts[purpose][languages[0]]
}

// Transcribe — дауыс жазбасы → мәтін.
//
// Order: the container is checked first (format by content, duration from
// the container, not from the client), then the request id: a repeat of a
// finished or running call gets that call's result and is neither counted nor
// sent again. Only a new call checks the day's cap and reaches the provider.
func (s *Service) Transcribe(ctx context.Context, req TranscribeRequest) (TranscribeResult, error) {
	if s.transcriber == nil {
		return TranscribeResult{}, domain.ErrNotFound
	}
	info, err := audio.Inspect(req.Audio)
	reason := ""
	switch {
	case errors.Is(err, audio.ErrUnsupported):
		reason = "format"
	case err != nil:
		reason = "unreadable"
	case info.Duration < minVoiceDuration:
		reason = "too_short"
	case info.Duration > maxVoiceDuration:
		reason = "too_long"
	}
	if reason != "" {
		// Клиенттің жазу жолын түзетуге: тек өлшем мен себеп.
		s.log.Info("ai_voice_rejected", "user_id", req.User.ID, "mode", ModeTranscribe, "reason", reason,
			"bytes", len(req.Audio), "duration_ms", info.Duration.Milliseconds(), "client_duration_ms", req.ClientDurationMS)
		return TranscribeResult{}, InvalidAudio(reason)
	}

	result, reused, err := s.voice.do(ctx, req.User.ID+"\x00"+req.RequestID, func() (TranscribeResult, error) {
		return s.transcribe(ctx, req, info)
	})
	if err != nil {
		return TranscribeResult{}, err
	}
	if reused {
		s.log.Info("ai_voice_reused", "user_id", req.User.ID, "mode", ModeTranscribe,
			"bytes", len(req.Audio), "duration_ms", info.Duration.Milliseconds())
	}
	result.Reused = reused
	return result, nil
}

// transcribe — жаңа шақыру: тәуліктік шек → провайдер → токендер, оқиға, журнал.
func (s *Service) transcribe(ctx context.Context, req TranscribeRequest, info audio.Info) (TranscribeResult, error) {
	started := s.clock.Now()
	if err := s.voiceCapReached(ctx, req.User.ID, started); err != nil {
		return TranscribeResult{}, err
	}
	entitlement, err := s.subs.Entitlement(ctx, req.User.ID)
	if err != nil {
		return TranscribeResult{}, err
	}

	language := ""
	if len(req.Languages) > 0 {
		language = req.Languages[0]
	}
	c := call{
		Mode:       ModeTranscribe,
		User:       req.User,
		DeviceID:   req.DeviceID,
		Language:   language,
		Platform:   req.User.Platform,
		AppVersion: req.User.AppVersion,
		// Дауыс үшін — секунд, жоғары қарай дөңгелектелген (мәтін емес, ұзындық).
		Chars: int((info.Duration + time.Second - 1) / time.Second),
	}
	out, err := s.transcriber.Transcribe(ctx, Audio{
		Data:      req.Audio,
		Format:    info.Format,
		Languages: req.Languages,
		Prompt:    VoicePrompt(req.Purpose, req.Languages),
	})
	latency := int(s.clock.Now().Sub(started).Milliseconds())
	// The keyboard may be closed while the provider is still listening: the
	// event is written even then.
	ctx, cancel := bookkeeping(ctx)
	defer cancel()

	logged := []any{"user_id", c.User.ID, "mode", c.Mode, "prompt_version", PromptVersionTranscribe,
		"purpose", req.Purpose, "format", info.Format, "bytes", len(req.Audio),
		"duration_ms", info.Duration.Milliseconds(), "client_duration_ms", req.ClientDurationMS,
		"languages", req.Languages, "platform", c.Platform, "app_version", c.AppVersion, "latency_ms", latency}
	if err != nil {
		code := errorCode(err)
		s.record(ctx, c, entitlement, "error", code, PromptVersionTranscribe,
			Completion{Model: s.transcriber.TranscribeModel()}, latency)
		s.log.Warn("ai_voice_failed", append(logged, "status", "error", "code", code)...)
		return TranscribeResult{}, err
	}

	if c.Language == "" && len(out.Languages) > 0 {
		c.Language = out.Languages[0]
	}
	completion := Completion{Model: out.Model, InputTokens: out.InputTokens, OutputTokens: out.OutputTokens,
		ProviderMS: out.ProviderMS}
	if completion.InputTokens > 0 || completion.OutputTokens > 0 {
		// Токен мен құн ғана: usage_daily.used (жауап квотасы) қозғалмайды.
		date, month := s.subs.Keys(started)
		if err := s.repo.AddTokens(ctx, c.User.ID, date, month, completion.InputTokens,
			completion.OutputTokens, s.estimateCostMicros(ctx, completion)); err != nil {
			s.log.Error("token accounting failed", "user_id", c.User.ID, "error", err.Error())
		}
	}
	s.record(ctx, c, entitlement, "success", "", PromptVersionTranscribe, completion, latency)
	s.log.Info("ai_voice_transcribed", append(logged, "status", "success", "detected", out.Languages,
		"retried", out.Retried, "provider_ms", out.ProviderMS, "empty", out.Text == "")...)

	return TranscribeResult{Text: out.Text, Languages: out.Languages, Duration: info.Duration}, nil
}

// voiceCapReached — бүгінгі (сервер күні) дауыс шегі толды ма.
//
// Counted the way polish is: the day's transcribe events, one indexed query.
// Unlike polish there is nothing quiet to return instead, so past the cap the
// answer is 429 VOICE_LIMIT_REACHED with the limit and the next reset. If the
// count fails, the request goes through.
func (s *Service) voiceCapReached(ctx context.Context, userID string, now time.Time) error {
	if s.transcribePerDay <= 0 {
		return nil
	}
	local := now.In(s.subs.Location())
	dayStart := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location())
	count, err := s.repo.UsageEventsSince(ctx, userID, ModeTranscribe, dayStart)
	if err != nil {
		s.log.Error("voice cap check failed", "user_id", userID, "error", err.Error())
		return nil
	}
	if count >= s.transcribePerDay {
		s.log.Info("voice daily cap reached", "user_id", userID, "limit", s.transcribePerDay)
		return &domain.VoiceLimitError{Limit: s.transcribePerDay, ResetsAt: dayStart.AddDate(0, 0, 1)}
	}
	return nil
}

// InvalidAudio — жазба қабылданбады; шектер клиентке бірге кетеді. HTTP қабаты да
// қолданады: бөлік жоқ (required) не тым үлкен (too_large).
func InvalidAudio(reason string) error {
	return &domain.AudioError{Reason: reason, MaxSeconds: MaxVoiceSeconds, MaxBytes: MaxVoiceBytes}
}
