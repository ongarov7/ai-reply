package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"slices"
	"strings"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/audio"
	"github.com/aireply/ai-reply-back-end/internal/domain"
)

// SPEECH. One keyboard recording goes to POST {OPENAI_BASE_URL}/audio/transcriptions
// as multipart/form-data, with the model the server picks (OPENAI_TRANSCRIBE_MODEL),
// never one the client asks for. The bytes are in memory for this call only;
// neither the audio nor the transcript is logged, and the provider's error body
// is read and dropped, never forwarded.

// speechTimeout — бір жазбаға (қайталауымен бірге) берілетін уақыт. Сұраныс
// контексінен туады: клиент кетсе, провайдерге сұраныс та тоқтайды.
const speechTimeout = 30 * time.Second

// maxDetectedLanguages — жауаптағы анықталған тілдердің ең көп саны.
const maxDetectedLanguages = 5

// Audio — провайдерге кететін бір жазба. Тек жадта, тек осы шақыру ішінде.
type Audio struct {
	Data   []byte
	Format string // audio.FormatM4A | audio.FormatWAV
	// Languages — ISO 639-1 болжамдары, ең ықтималы бірінші (languages[]).
	Languages []string
	// Prompt — қысқа контекст, бірінші болжамның тілінде; бос болса жіберілмейді.
	Prompt string
}

// Transcription — провайдердің жауабы.
type Transcription struct {
	Text string
	// Languages — провайдер анықтаған тілдер (бос болуы мүмкін).
	Languages    []string
	Model        string
	InputTokens  int
	OutputTokens int
	ProviderMS   int
	// Retried — бірінші сұранысқа 400 келді, тілдер мен prompt-сыз қайталанды.
	Retried bool
}

// Transcriber — сөйлеуді мәтінге айналдыратын провайдер. Тек сервер шақырады.
type Transcriber interface {
	TranscribeModel() string
	Transcribe(ctx context.Context, in Audio) (Transcription, error)
}

// TranscribeModel — сервер бекіткен дауыс моделі.
func (o *OpenAI) TranscribeModel() string { return o.cfg.TranscribeModel }

type transcriptionReply struct {
	Text      string          `json:"text"`
	Languages json.RawMessage `json:"languages"`
	Usage     struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// Transcribe — бір жазба → мәтін.
//
// languages[] carries every hint; the singular "language" field is never
// sent, the model takes one or the other. A 400 almost always means a hint
// or prompt the model refused, so the call is repeated exactly once without
// both, inside the same 30-second budget, rather than failing a recording
// that transcribes fine on its own. Errors map onto the reply path's kinds:
// a timeout is ErrProviderTimeout, 429 is ErrRateLimited, the rest
// ErrProviderDown.
func (o *OpenAI) Transcribe(ctx context.Context, in Audio) (Transcription, error) {
	ctx, cancel := context.WithTimeout(ctx, o.speechTimeout)
	defer cancel()

	started := time.Now()
	out, status, err := o.transcribeOnce(ctx, in)
	if err == nil && status == http.StatusBadRequest && (len(in.Languages) > 0 || in.Prompt != "") {
		in.Languages, in.Prompt = nil, ""
		out, status, err = o.transcribeOnce(ctx, in)
		out.Retried = true
	}
	if err != nil {
		return Transcription{}, err
	}
	switch status {
	case http.StatusOK:
	case http.StatusTooManyRequests:
		return Transcription{}, domain.ErrRateLimited
	default:
		return Transcription{}, fmt.Errorf("%w: status %d", domain.ErrProviderDown, status)
	}
	out.Model = o.cfg.TranscribeModel
	out.ProviderMS = int(time.Since(started).Milliseconds())
	return out, nil
}

// transcribeOnce — бір HTTP сұранысы. 200 емес жауап қате емес, мәртебе ретінде
// қайтады: оны Transcribe шешеді (қайталау не қате түрі).
func (o *OpenAI) transcribeOnce(ctx context.Context, in Audio) (Transcription, int, error) {
	body, contentType, err := speechForm(o.cfg.TranscribeModel, in)
	if err != nil {
		return Transcription{}, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.cfg.BaseURL+"/audio/transcriptions", body)
	if err != nil {
		return Transcription{}, 0, err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+o.cfg.APIKey)

	res, err := o.audio.Do(req)
	if err != nil {
		return Transcription{}, 0, transportError(ctx, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return Transcription{}, 0, transportError(ctx, err)
	}
	if res.StatusCode != http.StatusOK {
		// Қате денесінде жазбаның бөлігі не prompt болуы мүмкін — ол ешқайда кетпейді.
		return Transcription{}, res.StatusCode, nil
	}

	var parsed transcriptionReply
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Transcription{}, 0, domain.ErrProviderDown
	}
	return Transcription{
		Text:         strings.TrimSpace(parsed.Text),
		Languages:    detectedLanguages(parsed.Languages),
		InputTokens:  parsed.Usage.InputTokens,
		OutputTokens: parsed.Usage.OutputTokens,
	}, http.StatusOK, nil
}

// transportError — желі қатесі: уақыт біткені ErrProviderTimeout, қалғаны ErrProviderDown.
func transportError(ctx context.Context, err error) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return domain.ErrProviderTimeout
	}
	return domain.ErrProviderDown
}

// speechForm — multipart денесі: model, response_format, languages[], prompt, file.
func speechForm(model string, in Audio) (*bytes.Buffer, string, error) {
	var buf bytes.Buffer
	buf.Grow(len(in.Data) + 2048)
	form := multipart.NewWriter(&buf)
	fields := [][2]string{{"model", model}, {"response_format", "json"}}
	for _, code := range in.Languages {
		fields = append(fields, [2]string{"languages[]", code})
	}
	if in.Prompt != "" {
		fields = append(fields, [2]string{"prompt", in.Prompt})
	}
	for _, field := range fields {
		if err := form.WriteField(field[0], field[1]); err != nil {
			return nil, "", err
		}
	}
	// CreateFormFile әрқашан application/octet-stream қояды; провайдерге нақты түрі керек.
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", `form-data; name="file"; filename="`+audio.FileName(in.Format)+`"`)
	header.Set("Content-Type", audio.ContentType(in.Format))
	part, err := form.CreatePart(header)
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(in.Data); err != nil {
		return nil, "", err
	}
	if err := form.Close(); err != nil {
		return nil, "", err
	}
	return &buf, form.FormDataContentType(), nil
}

// detectedLanguages — жауаптағы languages: [{"code":"kk"}] (не ["kk"]).
// Тек 2–3 әріпті кодтар, кіші әріппен, қайталанбай; басқа пішін елеусіз қалады.
func detectedLanguages(raw json.RawMessage) []string {
	out := []string{}
	var objects []struct {
		Code string `json:"code"`
	}
	var codes []string
	if json.Unmarshal(raw, &objects) == nil {
		for _, o := range objects {
			codes = append(codes, o.Code)
		}
	} else if json.Unmarshal(raw, &codes) != nil {
		return out
	}
	for _, code := range codes {
		code = strings.ToLower(strings.TrimSpace(code))
		if i := strings.IndexAny(code, "-_"); i >= 0 {
			code = code[:i]
		}
		if !isLanguageCode(code) || slices.Contains(out, code) {
			continue
		}
		if out = append(out, code); len(out) == maxDetectedLanguages {
			break
		}
	}
	return out
}

func isLanguageCode(code string) bool {
	if len(code) < 2 || len(code) > 3 {
		return false
	}
	for _, r := range code {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	return true
}
