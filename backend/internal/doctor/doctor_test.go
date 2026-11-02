package doctor_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"chessbox-informant/database"
	"chessbox-informant/internal/config"
	"chessbox-informant/internal/doctor"
	"chessbox-informant/internal/ollama"
	"chessbox-informant/internal/testdb"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const unreachableDatabase = "postgres://postgres:postgres@127.0.0.1:1/postgres"

// fakeOllama serves the endpoints doctor uses. pulled maps each pulled model
// to its capabilities.
type fakeOllama struct {
	version   string
	pulled    map[string][]string
	systemOne bool
	running   ollama.RunningModel

	mu    sync.Mutex
	chats []map[string]any
}

func healthyOllama() *fakeOllama {
	return &fakeOllama{
		version: "0.40.13",
		pulled: map[string][]string{
			"qwen3.5:9b": {"completion", "thinking"},
			"laya":       {"decision"},
		},
		systemOne: true,
		running:   ollama.RunningModel{Model: "qwen3.5:9b", Size: 7e9, SizeVRAM: 7e9, ContextLength: 8192},
	}
}

func (f *fakeOllama) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	json.NewDecoder(r.Body).Decode(&body)
	model, _ := body["model"].(string)
	notPulled := func() {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, `{"error":"model '%s' not found"}`, model)
	}

	switch r.URL.Path {
	case "/api/version":
		fmt.Fprintf(w, `{"version":%q}`, f.version)
	case "/api/show":
		caps, ok := f.pulled[model]
		if !ok {
			notPulled()
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"capabilities": caps})
	case "/v1/systemone":
		if !f.systemOne {
			http.NotFound(w, r)
			return
		}
		if _, ok := f.pulled[model]; !ok {
			notPulled()
			return
		}
		fmt.Fprint(w, `{"answers":{"moves":{"type":"noul","noul":0.9}}}`)
	case "/api/chat":
		f.mu.Lock()
		f.chats = append(f.chats, body)
		f.mu.Unlock()
		fmt.Fprint(w, `{"message":{"content":""},"done":true,"done_reason":"load"}`)
	case "/api/ps":
		json.NewEncoder(w).Encode(map[string]any{"models": []ollama.RunningModel{f.running}})
	default:
		http.NotFound(w, r)
	}
}

// run runs doctor against f, or an unreachable Ollama when f is nil, and an
// unreachable database unless configure sets one.
func run(t *testing.T, f *fakeOllama, load bool, configure ...func(*config.Config)) map[string]doctor.Result {
	t.Helper()
	cfg := config.Default()
	cfg.DatabaseURL = unreachableDatabase
	cfg.OllamaURL = "http://127.0.0.1:1"
	if f != nil {
		srv := httptest.NewServer(f)
		t.Cleanup(srv.Close)
		cfg.OllamaURL = srv.URL
	}
	for _, c := range configure {
		c(&cfg)
	}

	results := map[string]doctor.Result{}
	var order []string
	doctor.New(cfg, doctor.Options{Models: true, Load: load}).Run(context.Background(), func(r doctor.Result) {
		results[r.Check] = r
		order = append(order, r.Check)
	})
	want := []string{doctor.CheckDatabase, doctor.CheckOllama, doctor.CheckLLM, doctor.CheckSystemOne, doctor.CheckVRAM}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Fatalf("checks = %v, want %v", order, want)
	}
	return results
}

func expect(t *testing.T, results map[string]doctor.Result, check string, status doctor.Status, detail string) {
	t.Helper()
	r := results[check]
	if r.Status != status || !strings.Contains(r.Detail, detail) {
		t.Errorf("%s = %s %q, want %s containing %q", check, r.Status, r.Detail, status, detail)
	}
}

func TestHealthy(t *testing.T) {
	results := run(t, healthyOllama(), false)
	expect(t, results, doctor.CheckOllama, doctor.OK, "0.40.13 at http://127.0.0.1")
	expect(t, results, doctor.CheckLLM, doctor.OK, "qwen3.5:9b is pulled")
	expect(t, results, doctor.CheckSystemOne, doctor.OK, "laya answered in")
	expect(t, results, doctor.CheckVRAM, doctor.Skip, "chessinfo doctor --load")
}

func TestOllamaVersion(t *testing.T) {
	tests := []struct {
		version string
		status  doctor.Status
	}{
		{"0.40.0", doctor.OK},
		{"0.41.0-rc1", doctor.OK},
		{"1.2.0", doctor.OK},
		{"0.39.9", doctor.Fail},
		{"0.4.0", doctor.Fail},
		{"dev", doctor.Warn},
	}
	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			f := healthyOllama()
			f.version = tt.version
			results := run(t, f, false)
			expect(t, results, doctor.CheckOllama, tt.status, tt.version)
			expect(t, results, doctor.CheckLLM, doctor.OK, "")
		})
	}
}

func TestOllamaUnreachable(t *testing.T) {
	results := run(t, nil, true)
	expect(t, results, doctor.CheckOllama, doctor.Fail, "cannot reach Ollama")
	for _, check := range []string{doctor.CheckLLM, doctor.CheckSystemOne, doctor.CheckVRAM} {
		expect(t, results, check, doctor.Skip, "Ollama is not reachable")
	}
}

func TestLLM(t *testing.T) {
	t.Run("not pulled", func(t *testing.T) {
		f := healthyOllama()
		delete(f.pulled, "qwen3.5:9b")
		results := run(t, f, true)
		expect(t, results, doctor.CheckLLM, doctor.Fail, "ollama pull qwen3.5:9b")
		expect(t, results, doctor.CheckVRAM, doctor.Skip, "not available")
		if len(f.chats) != 0 {
			t.Errorf("chats = %v, want none", f.chats)
		}
	})
	t.Run("not generative", func(t *testing.T) {
		f := healthyOllama()
		f.pulled["qwen3.5:9b"] = []string{"embedding"}
		expect(t, run(t, f, false), doctor.CheckLLM, doctor.Fail, "not a generative model")
	})
}

func TestSystemOne(t *testing.T) {
	tests := []struct {
		name   string
		breaks func(*fakeOllama)
		detail string
	}{
		{"not pulled", func(f *fakeOllama) { delete(f.pulled, "laya") }, "ollama pull laya"},
		{"not a decision model", func(f *fakeOllama) { f.pulled["laya"] = []string{"completion"} }, "not a System One model"},
		{"no endpoint", func(f *fakeOllama) { f.systemOne = false }, "no /v1/systemone endpoint"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := healthyOllama()
			tt.breaks(f)
			results := run(t, f, false)
			expect(t, results, doctor.CheckSystemOne, doctor.Warn, tt.detail)
			expect(t, results, doctor.CheckSystemOne, doctor.Warn, "without triage")
		})
	}
}

func TestVRAM(t *testing.T) {
	tests := []struct {
		name     string
		sizeVRAM int64
		status   doctor.Status
		detail   string
	}{
		{"all on the GPU", 7e9, doctor.OK, "qwen3.5:9b at num_ctx 8192 takes 7.0 GB, 100% on the GPU"},
		{"split", 4.9e9, doctor.Warn, "70% on the GPU and 30% on the CPU"},
		{"CPU only", 0, doctor.Warn, "all on the CPU"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := healthyOllama()
			f.running.Model = "qwen3.5:9b"
			f.running.SizeVRAM = tt.sizeVRAM
			expect(t, run(t, f, true), doctor.CheckVRAM, tt.status, tt.detail)

			if len(f.chats) != 2 {
				t.Fatalf("chats = %v, want a load and an unload", f.chats)
			}
			if opts, _ := f.chats[0]["options"].(map[string]any); opts["num_ctx"] != 8192.0 {
				t.Errorf("load options = %v, want num_ctx 8192", opts)
			}
			if f.chats[1]["keep_alive"] != "0s" {
				t.Errorf("unload keep_alive = %v, want 0s", f.chats[1]["keep_alive"])
			}
		})
	}

	t.Run("untagged model name", func(t *testing.T) {
		f := healthyOllama()
		f.pulled["laya"] = []string{"completion"}
		f.running.Model = "laya:latest"
		results := run(t, f, true, func(c *config.Config) { c.LLMModel = "laya" })
		expect(t, results, doctor.CheckVRAM, doctor.OK, "laya at num_ctx")
	})
}

func TestDefaultChecksOnlyTheDatabase(t *testing.T) {
	f := healthyOllama()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	cfg := config.Default()
	cfg.DatabaseURL = unreachableDatabase
	cfg.OllamaURL = srv.URL

	var checks []string
	doctor.New(cfg, doctor.Options{}).Run(context.Background(), func(r doctor.Result) {
		checks = append(checks, r.Check)
	})
	if strings.Join(checks, ",") != doctor.CheckDatabase {
		t.Errorf("checks = %v, want only %s", checks, doctor.CheckDatabase)
	}
}

func TestLoadImpliesModels(t *testing.T) {
	cfg := config.Default()
	cfg.DatabaseURL = unreachableDatabase
	cfg.OllamaURL = "http://127.0.0.1:1"

	var checks []string
	doctor.New(cfg, doctor.Options{Load: true}).Run(context.Background(), func(r doctor.Result) {
		checks = append(checks, r.Check)
	})
	if len(checks) != 5 {
		t.Errorf("checks = %v, want all five", checks)
	}
}

func withDatabase(url string) func(*config.Config) {
	return func(c *config.Config) { c.DatabaseURL = url }
}

func TestDatabase(t *testing.T) {
	expect(t, run(t, healthyOllama(), false), doctor.CheckDatabase, doctor.Fail, "cannot connect")

	url := testdb.Create(t)
	expect(t, run(t, healthyOllama(), false, withDatabase(url)), doctor.CheckDatabase, doctor.Fail, "run `chessinfo migrate`")

	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	migrator, err := database.NewMigrator(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrator.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	expect(t, run(t, healthyOllama(), false, withDatabase(url)), doctor.CheckDatabase, doctor.OK, "schema at version")
}
