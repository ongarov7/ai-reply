package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aireply/ai-reply-back-end/config"
	"github.com/aireply/ai-reply-back-end/internal/domain"
)

// Completion — провайдердің жауабы.
type Completion struct {
	Text         string
	Model        string
	InputTokens  int
	OutputTokens int
	// CachedTokens and ReasoningTokens — the parts of InputTokens and
	// OutputTokens the provider reported as cached input and reasoning.
	CachedTokens    int
	ReasoningTokens int
	ProviderMS      int
	// Truncated — жауап шекке жетіп үзілді (status "incomplete"); мәтін
	// соңғы толық сөйлемге дейін қысқартылған.
	Truncated bool
}

// Provider — AI провайдерінің келісімшарты. Тек сервер шақырады.
type Provider interface {
	Name() string
	Model() string
	Generate(ctx context.Context, prompt Prompt) (Completion, error)
}

// OpenAI — Responses API және дауысты мәтінге айналдыру клиенті. Кілт тек осы жерде оқылады.
type OpenAI struct {
	cfg    config.OpenAI
	client *http.Client
	// audio — дауыс жүктеуге: уақытты сұраныс контексі шектейді (speechTimeout),
	// OPENAI_TIMEOUT емес — 4 MiB жүктеу мен жауап мәтіндік генерациядан ұзағырақ.
	audio         *http.Client
	speechTimeout time.Duration
}

// NewOpenAI — клиент.
func NewOpenAI(cfg config.OpenAI) *OpenAI {
	return &OpenAI{cfg: cfg, client: &http.Client{Timeout: cfg.Timeout}, audio: &http.Client{},
		speechTimeout: speechTimeout}
}

// Name — провайдер аты.
func (o *OpenAI) Name() string { return "openai" }

// Model — сервер бекіткен модель (клиент таңдай алмайды).
func (o *OpenAI) Model() string { return o.cfg.Model }

type responsesRequest struct {
	Model           string `json:"model"`
	MaxOutputTokens int    `json:"max_output_tokens"`
	// Temperature — nil болса өріс жіберілмейді (reasoning модельдері үшін).
	Temperature *float64         `json:"temperature,omitempty"`
	Store       bool             `json:"store"`
	Input       []responsesInput `json:"input"`
}

// responsesInput — Content is a string, or content parts when the user
// message carries images.
type responsesInput struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

// contentPart — input_text or input_image of a user message.
type contentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

// responsesInputs — the developer and user messages of a prompt. Images go as
// data URLs after the user text; they are never written anywhere else.
func responsesInputs(prompt Prompt) []responsesInput {
	var user any = prompt.User
	if len(prompt.Images) > 0 {
		parts := []contentPart{{Type: "input_text", Text: prompt.User}}
		for _, image := range prompt.Images {
			detail := image.Detail
			if detail == "" {
				detail = "high"
			}
			parts = append(parts, contentPart{Type: "input_image", Detail: detail,
				ImageURL: "data:" + image.MIME + ";base64," + base64.StdEncoding.EncodeToString(image.Data)})
		}
		user = parts
	}
	return []responsesInput{{Role: "developer", Content: prompt.Developer}, {Role: "user", Content: user}}
}

type responsesReply struct {
	Model  string `json:"model"`
	Status string `json:"status"`
	Output []struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
	OutputText string `json:"output_text"`
	Usage      struct {
		InputTokens        int `json:"input_tokens"`
		OutputTokens       int `json:"output_tokens"`
		InputTokensDetails struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"input_tokens_details"`
		OutputTokensDetails struct {
			ReasoningTokens int `json:"reasoning_tokens"`
		} `json:"output_tokens_details"`
	} `json:"usage"`
}

// visionTimeout — a request with a screenshot: the upload and reading the
// image take longer than a text reply (OPENAI_TIMEOUT stays the floor).
const visionTimeout = 45 * time.Second

// Generate — бір жауап. store=false: мәтін провайдерде де сақталмайды.
func (o *OpenAI) Generate(ctx context.Context, prompt Prompt) (Completion, error) {
	maxTokens := o.cfg.MaxOutputTokens
	if prompt.MaxOutputTokens > 0 {
		maxTokens = prompt.MaxOutputTokens
	}
	payload, err := json.Marshal(responsesRequest{
		Model:           o.cfg.Model,
		MaxOutputTokens: maxTokens,
		Temperature:     o.cfg.Temperature,
		Store:           false,
		Input:           responsesInputs(prompt),
	})
	if err != nil {
		return Completion{}, err
	}

	timeout, client := o.cfg.Timeout, o.client
	if len(prompt.Images) > 0 {
		timeout, client = max(o.cfg.Timeout, visionTimeout), o.audio
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.cfg.BaseURL+"/responses", bytes.NewReader(payload))
	if err != nil {
		return Completion{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.cfg.APIKey)

	started := time.Now()
	res, err := client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return Completion{}, domain.ErrProviderTimeout
		}
		if errors.Is(err, context.Canceled) {
			// The person left (keyboard closed): same answer, but no outage to report.
			return Completion{}, fmt.Errorf("%w: %w", domain.ErrProviderDown, context.Canceled)
		}
		return Completion{}, domain.ErrProviderDown
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return Completion{}, domain.ErrProviderDown
	}
	elapsed := int(time.Since(started).Milliseconds())

	if res.StatusCode != http.StatusOK {
		// Жауап денесінде промпт болуы мүмкін — ол ешқашан журналға да, клиентке де кетпейді.
		if res.StatusCode == http.StatusTooManyRequests {
			return Completion{}, domain.ErrRateLimited
		}
		return Completion{}, fmt.Errorf("%w: status %d", domain.ErrProviderDown, res.StatusCode)
	}

	var parsed responsesReply
	if err := json.Unmarshal(body, &parsed); err != nil {
		return Completion{}, domain.ErrProviderDown
	}
	// Тазарту (тырнақша, кіріспе, markdown) — CleanOutput, бір жерде.
	text := strings.TrimSpace(extractText(parsed))
	truncated := parsed.Status == "incomplete"
	if truncated {
		text = trimToSentence(text)
	}
	if text == "" {
		return Completion{}, domain.ErrEmptyCompletion
	}
	model := parsed.Model
	if model == "" {
		model = o.cfg.Model
	}
	return Completion{
		Text:            text,
		Model:           model,
		InputTokens:     parsed.Usage.InputTokens,
		OutputTokens:    parsed.Usage.OutputTokens,
		CachedTokens:    parsed.Usage.InputTokensDetails.CachedTokens,
		ReasoningTokens: parsed.Usage.OutputTokensDetails.ReasoningTokens,
		ProviderMS:      elapsed,
		Truncated:       truncated,
	}, nil
}

// TokenCounter — a provider that can count a prompt's input tokens exactly
// without generating anything (the OpenAI input-token endpoint).
type TokenCounter interface {
	CountInputTokens(ctx context.Context, prompt Prompt) (int, error)
}

// countTimeout — counting must not hold up a reply: past it the server falls
// back to its own generous estimate.
const countTimeout = 4 * time.Second

// CountInputTokens — POST /responses/input_tokens with the same model and
// input a generation would send (store is irrelevant: nothing is generated).
// The prompt, image included, goes only to OpenAI and only for this count.
func (o *OpenAI) CountInputTokens(ctx context.Context, prompt Prompt) (int, error) {
	payload, err := json.Marshal(map[string]any{"model": o.cfg.Model, "input": responsesInputs(prompt)})
	if err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(ctx, countTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.cfg.BaseURL+"/responses/input_tokens", bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.cfg.APIKey)
	res, err := o.audio.Do(req)
	if err != nil {
		return 0, domain.ErrProviderDown
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	if err != nil || res.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("%w: input token count status %d", domain.ErrProviderDown, res.StatusCode)
	}
	var parsed struct {
		InputTokens int `json:"input_tokens"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil || parsed.InputTokens <= 0 {
		return 0, domain.ErrProviderDown
	}
	return parsed.InputTokens, nil
}

func extractText(r responsesReply) string {
	var b strings.Builder
	for _, item := range r.Output {
		if item.Type != "message" {
			continue
		}
		for _, part := range item.Content {
			if part.Type == "output_text" {
				b.WriteString(part.Text)
			}
		}
	}
	if out := strings.TrimSpace(b.String()); out != "" {
		return out
	}
	return strings.TrimSpace(r.OutputText)
}
