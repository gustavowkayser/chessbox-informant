// Package llm is the generative model behind the parse stage.
package llm

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

type Provider interface {
	Chat(ctx context.Context, req Request) (Response, error)
	// CheckModel fails when the model is not available to the provider.
	CheckModel(ctx context.Context, model string) error
}

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
}

type Request struct {
	Model    string
	Messages []Message
	// Schema is the JSON schema the output must follow; nil asks for free text.
	Schema json.RawMessage
	NumCtx int
	// KeepAlive is how long the model stays loaded after the call: nil leaves
	// it to the server, and 0 unloads the model right away.
	KeepAlive *time.Duration
	// Think turns the reasoning of a thinking model on or off: nil leaves it to
	// the model.
	Think *bool
}

type Response struct {
	Content      string
	PromptTokens int
	OutputTokens int
	Duration     time.Duration
}

var ErrTruncated = errors.New("output was cut off at the token limit")
