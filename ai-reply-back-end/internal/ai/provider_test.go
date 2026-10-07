package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/config"
)

// fakeResponses — Responses API-дің бір жауабы; келген сұраныс денесін сақтайды.
func fakeResponses(t *testing.T, reply string) (*httptest.Server, map[string]any) {
	t.Helper()
	got := map[string]any{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		clear(got)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(server.Close)
	return server, got
}

func openAIFor(server *httptest.Server, temperature *float64) *OpenAI {
	return NewOpenAI(config.OpenAI{APIKey: "sk-test", Model: "test-model", BaseURL: server.URL,
		MaxOutputTokens: 180, Timeout: 5 * time.Second, Temperature: temperature})
}

const completeReply = `{"model":"test-model","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"Да, смогу."}]}],"usage":{"input_tokens":10,"output_tokens":3}}`

func TestOpenAITemperatureIsOptional(t *testing.T) {
	server, got := fakeResponses(t, completeReply)
	warm := 0.7
	if _, err := openAIFor(server, &warm).Generate(context.Background(), Prompt{Developer: "d", User: "u"}); err != nil {
		t.Fatal(err)
	}
	if got["temperature"] != 0.7 || got["store"] != false {
		t.Fatalf("payload = %v", got)
	}

	if _, err := openAIFor(server, nil).Generate(context.Background(), Prompt{Developer: "d", User: "u"}); err != nil {
		t.Fatal(err)
	}
	if _, present := got["temperature"]; present {
		t.Fatalf("temperature must be omitted for models that reject it: %v", got)
	}
}

func TestOpenAITrimsAnIncompleteReply(t *testing.T) {
	server, _ := fakeResponses(t, `{"model":"test-model","status":"incomplete","output":[{"type":"message","content":[{"type":"output_text","text":"Спасибо за вопрос! Доставка занимает два дня. Если нужно, могу уточнить у кур"}]}],"usage":{"input_tokens":10,"output_tokens":20}}`)
	completion, err := openAIFor(server, nil).Generate(context.Background(), Prompt{Developer: "d", User: "u"})
	if err != nil {
		t.Fatal(err)
	}
	if !completion.Truncated || completion.Text != "Спасибо за вопрос! Доставка занимает два дня." {
		t.Fatalf("completion = %+v", completion)
	}

	whole, _ := fakeResponses(t, completeReply)
	if c, _ := openAIFor(whole, nil).Generate(context.Background(), Prompt{Developer: "d", User: "u"}); c.Truncated || c.Text != "Да, смогу." {
		t.Fatalf("a complete reply is left alone: %+v", c)
	}
}
