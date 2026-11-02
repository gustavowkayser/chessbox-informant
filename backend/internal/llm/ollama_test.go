package llm_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
	"time"

	"chessbox-informant/internal/config"
	"chessbox-informant/internal/llm"
	"chessbox-informant/internal/ollama"
)

const answerSchema = `{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"]}`

func serveChat(t *testing.T, status int, response string, got *map[string]any) *llm.Ollama {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("path = %s, want /api/chat", r.URL.Path)
		}
		if got != nil {
			if err := json.NewDecoder(r.Body).Decode(got); err != nil {
				t.Error(err)
			}
		}
		w.WriteHeader(status)
		io.WriteString(w, response)
	}))
	t.Cleanup(srv.Close)
	return llm.NewOllama(ollama.New(srv.URL))
}

func ptr[T any](v T) *T { return &v }

func TestOllamaChatRequest(t *testing.T) {
	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: "Parse the moves."},
		{Role: llm.RoleUser, Content: "[m1 1.e4]"},
	}
	wantMessages := []any{
		map[string]any{"role": "system", "content": "Parse the moves."},
		map[string]any{"role": "user", "content": "[m1 1.e4]"},
	}
	var schema map[string]any
	json.Unmarshal([]byte(answerSchema), &schema)

	tests := []struct {
		name string
		req  llm.Request
		want map[string]any
	}{
		{
			name: "server defaults",
			req:  llm.Request{Model: "qwen3.5:9b", Messages: messages},
			want: map[string]any{
				"model":    "qwen3.5:9b",
				"messages": wantMessages,
				"stream":   false,
				"options":  map[string]any{"temperature": 0.0},
			},
		},
		{
			name: "every option",
			req: llm.Request{
				Model:     "qwen3.5:9b",
				Messages:  messages,
				Schema:    json.RawMessage(answerSchema),
				NumCtx:    8192,
				KeepAlive: ptr(5 * time.Minute),
				Think:     ptr(false),
			},
			want: map[string]any{
				"model":      "qwen3.5:9b",
				"messages":   wantMessages,
				"stream":     false,
				"format":     schema,
				"options":    map[string]any{"temperature": 0.0, "num_ctx": 8192.0},
				"keep_alive": "5m0s",
				"think":      false,
			},
		},
		{
			name: "unload",
			req:  llm.Request{Model: "qwen3.5:9b", Messages: messages, KeepAlive: ptr(time.Duration(0))},
			want: map[string]any{
				"model":      "qwen3.5:9b",
				"messages":   wantMessages,
				"stream":     false,
				"options":    map[string]any{"temperature": 0.0},
				"keep_alive": "0s",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got map[string]any
			p := serveChat(t, http.StatusOK, `{"message":{"content":"{}"},"done":true}`, &got)
			if _, err := p.Chat(context.Background(), tt.req); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("request body\n got %v\nwant %v", got, tt.want)
			}
		})
	}
}

func TestOllamaChatResponse(t *testing.T) {
	p := serveChat(t, http.StatusOK, `{
		"message": {"role": "assistant", "content": "{\"answer\":\"yes\"}"},
		"done": true,
		"done_reason": "stop",
		"prompt_eval_count": 42,
		"eval_count": 7,
		"total_duration": 1500000000
	}`, nil)

	got, err := p.Chat(context.Background(), llm.Request{Model: "qwen3.5:9b"})
	if err != nil {
		t.Fatal(err)
	}
	want := llm.Response{Content: `{"answer":"yes"}`, PromptTokens: 42, OutputTokens: 7, Duration: 1500 * time.Millisecond}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestOllamaChatErrors(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		response string
		want     error
	}{
		{"model not pulled", http.StatusNotFound, `{"error":"model \"qwen3.5:9b\" not found, try pulling it first"}`, ollama.ErrModelNotPulled},
		{"truncated", http.StatusOK, `{"message":{"content":"{\"ans"},"done":true,"done_reason":"length"}`, llm.ErrTruncated},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := serveChat(t, tt.status, tt.response, nil)
			_, err := p.Chat(context.Background(), llm.Request{Model: "qwen3.5:9b"})
			if !errors.Is(err, tt.want) {
				t.Errorf("err = %v, want %v", err, tt.want)
			}
		})
	}

	t.Run("server error", func(t *testing.T) {
		p := serveChat(t, http.StatusInternalServerError, `{"error":"out of memory"}`, nil)
		_, err := p.Chat(context.Background(), llm.Request{Model: "qwen3.5:9b"})
		var apiErr *ollama.APIError
		if !errors.As(err, &apiErr) || apiErr.Message != "out of memory" {
			t.Errorf("err = %v, want the APIError from Ollama", err)
		}
	})
}

// TestOllamaLive runs against a real Ollama when CHESSINFO_TEST_OLLAMA_URL is
// set. CHESSINFO_TEST_LLM_MODEL picks the model; it defaults to the LLM default.
func TestOllamaLive(t *testing.T) {
	url := os.Getenv("CHESSINFO_TEST_OLLAMA_URL")
	if url == "" {
		t.Skip("CHESSINFO_TEST_OLLAMA_URL is not set")
	}
	model := os.Getenv("CHESSINFO_TEST_LLM_MODEL")
	if model == "" {
		model = config.Default().LLMModel
	}
	ctx := context.Background()
	p := llm.NewOllama(ollama.New(url))
	if err := p.CheckModel(ctx, model); err != nil {
		t.Fatal(err)
	}

	resp, err := p.Chat(ctx, llm.Request{
		Model:     model,
		Messages:  []llm.Message{{Role: llm.RoleUser, Content: `Answer with the word "yes".`}},
		Schema:    json.RawMessage(answerSchema),
		NumCtx:    config.Default().LLMNumCtx,
		KeepAlive: ptr(time.Duration(0)),
		Think:     ptr(false),
	})
	if err != nil {
		t.Fatal(err)
	}
	var out struct{ Answer *string }
	if err := json.Unmarshal([]byte(resp.Content), &out); err != nil || out.Answer == nil {
		t.Errorf("content %q does not follow the schema: %v", resp.Content, err)
	}
	t.Logf("%s: %d prompt tokens, %d output tokens, %s", model, resp.PromptTokens, resp.OutputTokens, resp.Duration)
}
