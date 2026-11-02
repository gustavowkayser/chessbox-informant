// Package systemone is the client for System One decision models such as
// Laya, served by Ollama at POST /v1/systemone.
package systemone

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"chessbox-informant/internal/ollama"
)

var ErrUnsupported = errors.New("no /v1/systemone endpoint; upgrade to Ollama 0.40 or newer")

// missingEndpoint is the plain-text body of Ollama's router for an unknown
// path. A missing model is a 404 too, but with a JSON error.
const missingEndpoint = "404 page not found"

type Question struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions"`
	Criteria     any    `json:"criteria,omitempty"`
}

// Choice asks which option fits, given a description per option key.
func Choice(instructions string, options map[string]string) Question {
	return Question{Type: "choice", Instructions: instructions, Criteria: options}
}

// Noul asks for the probability that a statement is true.
func Noul(instructions string) Question {
	return Question{Type: "noul", Instructions: instructions}
}

// Score asks for a level, given the levels from lowest to highest.
func Score(instructions string, levels ...string) Question {
	return Question{Type: "score", Instructions: instructions, Criteria: levels}
}

type Request struct {
	Model string
	// State is a string, or any other value, which is sent as JSON.
	State     any
	Questions map[string]Question
	// KeepAlive is how long the model stays loaded after the call: nil leaves
	// it to the server, and 0 unloads the model right away.
	KeepAlive *time.Duration
}

// Answer holds the fields of every question type; only those of its Type are
// set. Score is the probability-weighted level, from 0 to the number of levels
// minus 1. Confidence measures how concentrated the answer is, not whether it
// is right, and is not given for noul questions.
type Answer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice"`
	Noul          float64            `json:"noul"`
	Score         float64            `json:"score"`
	Probabilities map[string]float64 `json:"probabilities"`
	Confidence    float64            `json:"confidence"`
}

type Response struct {
	Answers     map[string]Answer
	InputTokens int
}

type Client struct {
	client *ollama.Client
}

func New(client *ollama.Client) *Client {
	return &Client{client: client}
}

type wireRequest struct {
	Model     string              `json:"model"`
	State     any                 `json:"state"`
	Questions map[string]Question `json:"questions"`
	KeepAlive string              `json:"keep_alive,omitempty"`
}

type wireResponse struct {
	Answers map[string]Answer `json:"answers"`
	Usage   struct {
		InputTokens int `json:"input_tokens"`
	} `json:"usage"`
}

func (c *Client) Ask(ctx context.Context, req Request) (Response, error) {
	body := wireRequest{Model: req.Model, State: req.State, Questions: req.Questions}
	if req.KeepAlive != nil {
		body.KeepAlive = req.KeepAlive.String()
	}

	var resp wireResponse
	if err := c.client.Post(ctx, "/v1/systemone", body, &resp); err != nil {
		var apiErr *ollama.APIError
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound && apiErr.Message == missingEndpoint {
			return Response{}, ErrUnsupported
		}
		return Response{}, ollama.ModelError(req.Model, err)
	}
	for name, q := range req.Questions {
		if a, ok := resp.Answers[name]; !ok || a.Type != q.Type {
			return Response{}, fmt.Errorf("systemone: no %s answer for question %q", q.Type, name)
		}
	}
	return Response{Answers: resp.Answers, InputTokens: resp.Usage.InputTokens}, nil
}
