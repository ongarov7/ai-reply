package api

import (
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/aireply/ai-reply-back-end/internal/ai"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/transport/httpx"
)

// Дауыс сұранысының шектері.
const (
	// voiceBodyBytes — бүкіл көп бөлікті дене: 4 MiB жазба + өрістер мен шекаралар.
	voiceBodyBytes = 5 << 20
	// voiceFieldBytes — бір мәтіндік өрістің ең үлкен өлшемі.
	voiceFieldBytes = 256
	// voiceDurationMinMS, voiceDurationMaxMS — клиент айтатын белсенді ұзақтық.
	voiceDurationMinMS = 300
	voiceDurationMaxMS = 40500
)

// voiceRequestID — клиенттің идемпотенттік кілті (UUID та, қысқа id де өтеді).
var voiceRequestID = regexp.MustCompile(`^[A-Za-z0-9-]{8,64}$`)

type transcribeResponse struct {
	Text      string   `json:"text"`
	Languages []string `json:"languages"`
	// DurationMS — контейнерден оқылған ұзақтық (клиент айтқаны емес).
	DurationMS int64 `json:"duration_ms"`
}

// voiceForm — тексерілген көп бөлікті дене.
type voiceForm struct {
	audio      []byte
	durationMS int
	languages  []string
	requestID  string
	purpose    string
}

// voiceEnabled — features.voice_transcription: баптау қосулы және провайдер бар.
func (s *Server) voiceEnabled() bool {
	return s.cfg.VoiceTranscription() && s.ai.CanTranscribe()
}

// handleTranscribe — пернетақтадағы дауыс жазбасы → мәтін (features.voice_transcription).
//
// multipart/form-data: audio (M4A or WAV, ≤ 4 MiB, decided by its bytes),
// duration_ms, request_id, languages, purpose. The audio is read into memory
// under a hard cap and dropped with the request: it never touches the disk,
// the database or the log, and neither does the transcript. No reply quota
// is spent; the per-minute bucket and the day's cap are the endpoint's own.
func (s *Server) handleTranscribe(w http.ResponseWriter, r *http.Request) {
	if !s.voiceEnabled() {
		httpx.Fail(w, domain.ErrNotFound)
		return
	}
	user, _ := UserFrom(r.Context())
	form, err := readVoiceForm(w, r)
	if err != nil {
		httpx.Fail(w, err)
		return
	}

	result, err := s.ai.Transcribe(r.Context(), ai.TranscribeRequest{
		User:             user,
		DeviceID:         DeviceFrom(r.Context()),
		RequestID:        form.requestID,
		Audio:            form.audio,
		ClientDurationMS: form.durationMS,
		Languages:        form.languages,
		Purpose:          form.purpose,
	})
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	languages := result.Languages
	if languages == nil {
		languages = []string{}
	}
	httpx.JSON(w, http.StatusOK, transcribeResponse{
		Text:       result.Text,
		Languages:  languages,
		DurationMS: result.Duration.Milliseconds(),
	})
}

// readVoiceForm — денені бөлік-бөлік оқиды: жазба ≤ ai.MaxVoiceBytes, әр өріс ≤ 256 байт.
//
// r.MultipartReader, not ParseMultipartForm: the latter spills large parts to
// temporary files, and a recording must never reach the disk. Unknown parts
// are skipped without being kept. Field errors are 400 INVALID_REQUEST with
// details.field, as on the JSON endpoints.
func readVoiceForm(w http.ResponseWriter, r *http.Request) (voiceForm, error) {
	r.Body = http.MaxBytesReader(w, r.Body, voiceBodyBytes)
	reader, err := r.MultipartReader()
	if err != nil {
		return voiceForm{}, domain.ErrInvalidRequest
	}

	var form voiceForm
	fields := map[string]string{}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return voiceForm{}, bodyError(err)
		}
		name := part.FormName()
		switch name {
		case "audio":
			if form.audio != nil {
				return voiceForm{}, domain.InvalidField("audio", "duplicate")
			}
			data, err := io.ReadAll(io.LimitReader(part, ai.MaxVoiceBytes+1))
			if err != nil {
				return voiceForm{}, bodyError(err)
			}
			if len(data) > ai.MaxVoiceBytes {
				return voiceForm{}, ai.InvalidAudio("too_large")
			}
			form.audio = data
		case "duration_ms", "languages", "request_id", "purpose":
			value, err := io.ReadAll(io.LimitReader(part, voiceFieldBytes+1))
			if err != nil {
				return voiceForm{}, bodyError(err)
			}
			if len(value) > voiceFieldBytes {
				return voiceForm{}, domain.InvalidField(name, "too_long")
			}
			fields[name] = strings.TrimSpace(string(value))
		}
		_ = part.Close()
	}

	form.requestID = fields["request_id"]
	switch {
	case form.requestID == "":
		return voiceForm{}, domain.InvalidField("request_id", "required")
	case !voiceRequestID.MatchString(form.requestID):
		return voiceForm{}, domain.InvalidField("request_id", "format")
	}
	raw, ok := fields["duration_ms"]
	if !ok || raw == "" {
		return voiceForm{}, domain.InvalidField("duration_ms", "required")
	}
	form.durationMS, err = strconv.Atoi(raw)
	if err != nil || form.durationMS < voiceDurationMinMS || form.durationMS > voiceDurationMaxMS {
		return voiceForm{}, domain.InvalidField("duration_ms", "range")
	}
	switch form.purpose = fields["purpose"]; form.purpose {
	case "":
		form.purpose = ai.PurposeDictation
	case ai.PurposeDictation, ai.PurposeInstruction:
	default:
		return voiceForm{}, domain.InvalidField("purpose", "dictation or instruction")
	}
	form.languages = ai.VoiceLanguages(fields["languages"])
	if len(form.audio) == 0 {
		return voiceForm{}, ai.InvalidAudio("required")
	}
	return form, nil
}

// bodyError — дене оқылмады: шектен асса — жазба тым үлкен, әйтпесе жарамсыз сұраныс.
func bodyError(err error) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return ai.InvalidAudio("too_large")
	}
	return domain.ErrInvalidRequest
}
