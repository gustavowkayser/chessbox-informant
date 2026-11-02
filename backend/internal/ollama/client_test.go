package ollama_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"chessbox-informant/internal/ollama"
)

func serve(t *testing.T, handler http.HandlerFunc) *ollama.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return ollama.New(srv.URL + "/")
}

func TestPost(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/echo" {
			t.Errorf("got %s %s, want POST /api/echo", r.Method, r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q", ct)
		}
		body, _ := io.ReadAll(r.Body)
		w.Write(body)
	})

	var out struct{ Word string }
	if err := c.Post(context.Background(), "/api/echo", map[string]string{"word": "hello"}, &out); err != nil {
		t.Fatal(err)
	}
	if out.Word != "hello" {
		t.Errorf("Word = %q, want hello", out.Word)
	}
}

func TestPostAPIError(t *testing.T) {
	tests := []struct {
		name, body string
		status     int
		want       string
	}{
		{"json error", `{"error":"model runner crashed"}`, 500, "model runner crashed"},
		{"plain text", "404 page not found", 404, "404 page not found"},
		{"empty body", "", 503, "Service Unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := serve(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				io.WriteString(w, tt.body)
			})
			err := c.Post(context.Background(), "/api/x", struct{}{}, nil)
			var apiErr *ollama.APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("err = %v, want *APIError", err)
			}
			if apiErr.Status != tt.status || apiErr.Message != tt.want {
				t.Errorf("got %d %q, want %d %q", apiErr.Status, apiErr.Message, tt.status, tt.want)
			}
		})
	}
}

func TestPostUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()

	err := ollama.New(url).Post(context.Background(), "/api/x", struct{}{}, nil)
	if err == nil || !strings.Contains(err.Error(), "cannot reach Ollama at "+url) {
		t.Errorf("err = %v, want a cannot reach error", err)
	}
}

func TestPostCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	release := make(chan struct{})
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		cancel()
		<-release
	})

	err := c.Post(ctx, "/api/x", struct{}{}, nil)
	close(release)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestCheckModel(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/show" {
			t.Errorf("path = %s, want /api/show", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), `"model":"missing"`) {
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"error":"model 'missing' not found"}`)
			return
		}
		io.WriteString(w, `{"modelfile":"..."}`)
	})

	if err := c.CheckModel(context.Background(), "laya"); err != nil {
		t.Errorf("pulled model: %v", err)
	}
	err := c.CheckModel(context.Background(), "missing")
	if !errors.Is(err, ollama.ErrModelNotPulled) {
		t.Fatalf("err = %v, want ErrModelNotPulled", err)
	}
	if !strings.Contains(err.Error(), "ollama pull missing") {
		t.Errorf("err = %v, want the pull hint", err)
	}
}

func TestVersionAndRunning(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("%s %s, want GET", r.Method, r.URL.Path)
		}
		switch r.URL.Path {
		case "/api/version":
			io.WriteString(w, `{"version":"0.40.13"}`)
		case "/api/ps":
			io.WriteString(w, `{"models":[{"name":"laya:latest","model":"laya:latest","size":873778379,"size_vram":0,"context_length":512}]}`)
		default:
			http.NotFound(w, r)
		}
	})

	v, err := c.Version(context.Background())
	if err != nil || v != "0.40.13" {
		t.Errorf("Version = %q, %v", v, err)
	}
	running, err := c.Running(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []ollama.RunningModel{{Model: "laya:latest", Size: 873778379, ContextLength: 512}}
	if !reflect.DeepEqual(running, want) {
		t.Errorf("Running = %+v, want %+v", running, want)
	}
}

func TestShow(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"capabilities":["decision"],"details":{"family":"modern-bert"}}`)
	})
	info, err := c.Show(context.Background(), "laya")
	if err != nil || !reflect.DeepEqual(info.Capabilities, []string{"decision"}) {
		t.Errorf("Show = %+v, %v", info, err)
	}
}
