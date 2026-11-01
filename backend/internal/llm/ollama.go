package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"chessbox-informant/internal/ollama"
)

type Ollama struct {
	client *ollama.Client
}

var _ Provider = (*Ollama)(nil)

func NewOllama(client *ollama.Client) *Ollama {
	return &Ollama{client: client}
}

type ollamaChatRequest struct {
	Model     string          `json:"model"`
	Messages  []Message       `json:"messages"`
	Stream    bool            `json:"stream"`
	Format    json.RawMessage `json:"format,omitempty"`
	Options   ollamaOptions   `json:"options"`
	KeepAlive string          `json:"keep_alive,omitempty"`
	Think     *bool           `json:"think,omitempty"`
}

type ollamaOptions struct {
	// Indexing the same book twice must give the same quotes.
	Temperature float64 `json:"temperature"`
	NumCtx      int     `json:"num_ctx,omitempty"`
}

type ollamaChatResponse struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	DoneReason      string        `json:"done_reason"`
	PromptEvalCount int           `json:"prompt_eval_count"`
	EvalCount       int           `json:"eval_count"`
	TotalDuration   time.Duration `json:"total_duration"`
}

func (o *Ollama) Chat(ctx context.Context, req Request) (Response, error) {
	body := ollamaChatRequest{
		Model:    req.Model,
		Messages: req.Messages,
		Format:   req.Schema,
		Options:  ollamaOptions{NumCtx: req.NumCtx},
		Think:    req.Think,
	}
	if req.KeepAlive != nil {
		body.KeepAlive = req.KeepAlive.String()
	}

	var resp ollamaChatResponse
	if err := o.client.Post(ctx, "/api/chat", body, &resp); err != nil {
		return Response{}, ollama.ModelError(req.Model, err)
	}
	if resp.DoneReason == "length" {
		return Response{}, fmt.Errorf("%s: %w", req.Model, ErrTruncated)
	}
	return Response{
		Content:      resp.Message.Content,
		PromptTokens: resp.PromptEvalCount,
		OutputTokens: resp.EvalCount,
		Duration:     resp.TotalDuration,
	}, nil
}

func (o *Ollama) CheckModel(ctx context.Context, model string) error {
	return o.client.CheckModel(ctx, model)
}
