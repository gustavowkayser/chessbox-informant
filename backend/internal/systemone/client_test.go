package systemone_test

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
	"chessbox-informant/internal/ollama"
	"chessbox-informant/internal/systemone"
)

var questions = map[string]systemone.Question{
	"kind": systemone.Choice("What kind of passage is this?", map[string]string{
		"game_score": "Moves with little commentary",
		"other":      "Non-chess text",
	}),
	"discusses_previous": systemone.Noul("Does it talk about the moves before it?"),
	"instructive":        systemone.Score("How useful is it?", "Not useful", "Useful"),
}

const answers = `{
	"model": "laya",
	"answers": {
		"kind": {
			"type": "choice",
			"choice": "game_score",
			"probabilities": {"game_score": 0.75, "other": 0.25},
			"confidence": 0.19
		},
		"discusses_previous": {"type": "noul", "noul": 0.24},
		"instructive": {
			"type": "score",
			"score": 0.4,
			"legend": {"0": "Not useful", "1": "Useful"},
			"probabilities": {"0": 0.6, "1": 0.4},
			"confidence": 0.03
		}
	},
	"usage": {"input_tokens": 444, "output_tokens": 0}
}`

func serve(t *testing.T, status int, response string, got *map[string]any) *systemone.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/systemone" {
			t.Errorf("path = %s, want /v1/systemone", r.URL.Path)
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
	return systemone.New(ollama.New(srv.URL))
}

func ptr[T any](v T) *T { return &v }

func TestAskRequest(t *testing.T) {
	var got map[string]any
	c := serve(t, http.StatusOK, answers, &got)
	_, err := c.Ask(context.Background(), systemone.Request{
		Model:     "laya",
		State:     map[string]string{"previous": "1.e4 e5", "passage": "2.Nf3"},
		Questions: questions,
		KeepAlive: ptr(time.Duration(0)),
	})
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]any{
		"model": "laya",
		"state": map[string]any{"previous": "1.e4 e5", "passage": "2.Nf3"},
		"questions": map[string]any{
			"kind": map[string]any{
				"type":         "choice",
				"instructions": "What kind of passage is this?",
				"criteria":     map[string]any{"game_score": "Moves with little commentary", "other": "Non-chess text"},
			},
			"discusses_previous": map[string]any{
				"type":         "noul",
				"instructions": "Does it talk about the moves before it?",
			},
			"instructive": map[string]any{
				"type":         "score",
				"instructions": "How useful is it?",
				"criteria":     []any{"Not useful", "Useful"},
			},
		},
		"keep_alive": "0s",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("request body\n got %v\nwant %v", got, want)
	}
}

func TestAskResponse(t *testing.T) {
	c := serve(t, http.StatusOK, answers, nil)
	got, err := c.Ask(context.Background(), systemone.Request{Model: "laya", State: "2.Nf3", Questions: questions})
	if err != nil {
		t.Fatal(err)
	}

	want := systemone.Response{
		Answers: map[string]systemone.Answer{
			"kind": {
				Type:          "choice",
				Choice:        "game_score",
				Probabilities: map[string]float64{"game_score": 0.75, "other": 0.25},
				Confidence:    0.19,
			},
			"discusses_previous": {Type: "noul", Noul: 0.24},
			"instructive": {
				Type:          "score",
				Score:         0.4,
				Probabilities: map[string]float64{"0": 0.6, "1": 0.4},
				Confidence:    0.03,
			},
		},
		InputTokens: 444,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestAskErrors(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		response string
		want     error
	}{
		{"model not pulled", http.StatusNotFound, `{"error":"model 'laya' not found"}`, ollama.ErrModelNotPulled},
		{"Ollama without System One", http.StatusNotFound, "404 page not found", systemone.ErrUnsupported},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := serve(t, tt.status, tt.response, nil)
			_, err := c.Ask(context.Background(), systemone.Request{Model: "laya", State: "x", Questions: questions})
			if !errors.Is(err, tt.want) {
				t.Errorf("err = %v, want %v", err, tt.want)
			}
		})
	}

	t.Run("invalid question", func(t *testing.T) {
		c := serve(t, http.StatusBadRequest, `{"error":"question \"q\": score criteria must be an array of 2–10 levels"}`, nil)
		_, err := c.Ask(context.Background(), systemone.Request{Model: "laya", State: "x", Questions: questions})
		var apiErr *ollama.APIError
		if !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadRequest {
			t.Errorf("err = %v, want the APIError from Ollama", err)
		}
	})

	t.Run("missing answer", func(t *testing.T) {
		c := serve(t, http.StatusOK, `{"answers":{"kind":{"type":"choice","choice":"other"}}}`, nil)
		_, err := c.Ask(context.Background(), systemone.Request{Model: "laya", State: "x", Questions: questions})
		if err == nil {
			t.Error("want an error for the questions without an answer")
		}
	})
}

// TestAskLive runs against a real Ollama when CHESSINFO_TEST_OLLAMA_URL is set.
// CHESSINFO_TEST_SYSTEMONE_MODEL picks the model; it defaults to the System One
// default.
func TestAskLive(t *testing.T) {
	url := os.Getenv("CHESSINFO_TEST_OLLAMA_URL")
	if url == "" {
		t.Skip("CHESSINFO_TEST_OLLAMA_URL is not set")
	}
	model := os.Getenv("CHESSINFO_TEST_SYSTEMONE_MODEL")
	if model == "" {
		model = config.Default().SystemOneModel
	}

	resp, err := systemone.New(ollama.New(url)).Ask(context.Background(), systemone.Request{
		Model:     model,
		State:     map[string]string{"previous": "1.e4 e5 2.Nf3 Nc6", "passage": "3.Bb5 The Ruy Lopez."},
		Questions: questions,
		KeepAlive: ptr(time.Duration(0)),
	})
	if err != nil {
		t.Fatal(err)
	}
	kind := resp.Answers["kind"]
	if _, ok := kind.Probabilities[kind.Choice]; !ok {
		t.Errorf("choice %q is not one of the options %v", kind.Choice, kind.Probabilities)
	}
	t.Logf("%s: %+v, %d input tokens", model, resp.Answers, resp.InputTokens)
}
