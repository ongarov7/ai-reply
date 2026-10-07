package ai

import (
	"bytes"
	"context"
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
	ProviderMS   int
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

// OpenAI — Responses API клиенті. Кілт тек осы жерде оқылады.
type OpenAI struct {
	cfg    config.OpenAI
	client *http.Client
}

// NewOpenAI — клиент.
func NewOpenAI(cfg config.OpenAI) *OpenAI {
	return &OpenAI{cfg: cfg, client: &http.Client{Timeout: cfg.Timeout}}
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

type responsesInput struct {
	Role    string `json:"role"`
	Content string `json:"content"`
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
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

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
		Input: []responsesInput{
			{Role: "developer", Content: prompt.Developer},
			{Role: "user", Content: prompt.User},
		},
	})
	if err != nil {
		return Completion{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, o.cfg.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.cfg.BaseURL+"/responses", bytes.NewReader(payload))
	if err != nil {
		return Completion{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.cfg.APIKey)

	started := time.Now()
	res, err := o.client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return Completion{}, domain.ErrProviderTimeout
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
		Text:         text,
		Model:        model,
		InputTokens:  parsed.Usage.InputTokens,
		OutputTokens: parsed.Usage.OutputTokens,
		ProviderMS:   elapsed,
		Truncated:    truncated,
	}, nil
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
