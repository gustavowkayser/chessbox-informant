// Package ollama is the HTTP client for the Ollama API, shared by the llm and
// systemone providers and by chessinfo doctor.
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

var ErrModelNotPulled = errors.New("model is not pulled")

// maxErrorBody caps how much of an error response is read; Ollama's error
// bodies are one short JSON object.
const maxErrorBody = 64 << 10

type Client struct {
	baseURL string
	http    *http.Client
}

func New(baseURL string) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), http: &http.Client{}}
}

type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("ollama: %s (HTTP %d)", e.Message, e.Status)
}

// Post sends in as JSON and decodes the response into out, unless out is nil.
// Errors answered by Ollama are returned as *APIError.
func (c *Client) Post(ctx context.Context, path string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodPost, path, bytes.NewReader(body), out)
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodGet, path, nil, out)
}

func (c *Client) do(ctx context.Context, method, path string, body io.Reader, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("cannot reach Ollama at %s: %w", c.baseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return readAPIError(resp)
	}
	if out == nil {
		_, err := io.Copy(io.Discard, resp.Body)
		return err
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("ollama %s: decode response: %w", path, err)
	}
	return nil
}

func readAPIError(resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	msg := strings.TrimSpace(string(raw))
	var body struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(raw, &body) == nil && body.Error != "" {
		msg = body.Error
	}
	if msg == "" {
		msg = http.StatusText(resp.StatusCode)
	}
	return &APIError{Status: resp.StatusCode, Message: msg}
}

func (c *Client) BaseURL() string {
	return c.baseURL
}

func (c *Client) Version(ctx context.Context) (string, error) {
	var resp struct {
		Version string `json:"version"`
	}
	err := c.get(ctx, "/api/version", &resp)
	return resp.Version, err
}

type ModelInfo struct {
	// Capabilities are what the model can do, such as "completion", "vision"
	// or "decision" (System One).
	Capabilities []string `json:"capabilities"`
}

// Show returns an error wrapping ErrModelNotPulled when the model is not
// pulled.
func (c *Client) Show(ctx context.Context, model string) (ModelInfo, error) {
	var info ModelInfo
	err := c.Post(ctx, "/api/show", map[string]string{"model": model}, &info)
	return info, ModelError(model, err)
}

// CheckModel returns an error wrapping ErrModelNotPulled when the model is not
// pulled.
func (c *Client) CheckModel(ctx context.Context, model string) error {
	_, err := c.Show(ctx, model)
	return err
}

type RunningModel struct {
	Model         string `json:"model"`
	Size          int64  `json:"size"`
	SizeVRAM      int64  `json:"size_vram"`
	ContextLength int    `json:"context_length"`
}

// Running lists the loaded models. Their names always carry a tag, such as
// "laya:latest" for "laya".
func (c *Client) Running(ctx context.Context) ([]RunningModel, error) {
	var resp struct {
		Models []RunningModel `json:"models"`
	}
	err := c.get(ctx, "/api/ps", &resp)
	return resp.Models, err
}

// ModelError turns the 404 that Ollama answers for a model that is not pulled
// into an error wrapping ErrModelNotPulled, and returns other errors as they
// are. Use it only on endpoints that exist in every Ollama version, where a 404
// cannot mean a missing endpoint.
func ModelError(model string, err error) error {
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
		return fmt.Errorf("%w: %s (run `ollama pull %s`)", ErrModelNotPulled, model, model)
	}
	return err
}
