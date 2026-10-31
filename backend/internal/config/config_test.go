package config_test

import (
	"strings"
	"testing"

	"chessbox-informant/internal/config"

	"github.com/spf13/pflag"
)

func newFlagSet(t *testing.T, args ...string) *pflag.FlagSet {
	t.Helper()
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	config.RegisterFlags(fs)
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}
	return fs
}

func envOf(vars map[string]string) func(string) string {
	return func(key string) string { return vars[key] }
}

func TestLoad(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		args []string
		want func(*config.Config)
	}{
		{
			name: "defaults",
			want: func(*config.Config) {},
		},
		{
			name: "env overrides default",
			env: map[string]string{
				"CHESSINFO_DATABASE_URL":    "postgres://env",
				"CHESSINFO_OLLAMA_URL":      "http://ollama:11434",
				"CHESSINFO_LLM_MODEL":       "qwen3.5:4b",
				"CHESSINFO_LLM_NUM_CTX":     "4096",
				"CHESSINFO_SYSTEMONE_MODEL": "laya-multilingual",
			},
			want: func(c *config.Config) {
				c.DatabaseURL = "postgres://env"
				c.OllamaURL = "http://ollama:11434"
				c.LLMModel = "qwen3.5:4b"
				c.LLMNumCtx = 4096
				c.SystemOneModel = "laya-multilingual"
			},
		},
		{
			name: "flag overrides env",
			env:  map[string]string{"CHESSINFO_DATABASE_URL": "postgres://env", "CHESSINFO_LLM_NUM_CTX": "4096"},
			args: []string{"--database-url", "postgres://flag", "--num-ctx", "2048"},
			want: func(c *config.Config) {
				c.DatabaseURL = "postgres://flag"
				c.LLMNumCtx = 2048
			},
		},
		{
			name: "empty env is ignored",
			env:  map[string]string{"CHESSINFO_DATABASE_URL": ""},
			want: func(*config.Config) {},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := config.Default()
			tt.want(&want)

			got, err := config.Load(newFlagSet(t, tt.args...), envOf(tt.env))
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Errorf("got %+v, want %+v", got, want)
			}
		})
	}
}

func TestLoadSkipsUnregisteredFlags(t *testing.T) {
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)

	got, err := config.Load(fs, envOf(map[string]string{"CHESSINFO_LLM_MODEL": "qwen3.5:4b"}))
	if err != nil {
		t.Fatal(err)
	}
	if got.LLMModel != "qwen3.5:4b" {
		t.Errorf("LLMModel = %q, want qwen3.5:4b", got.LLMModel)
	}
}

func TestLoadRejectsInvalidNumCtx(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		args    []string
		wantErr string
	}{
		{"env not an integer", map[string]string{"CHESSINFO_LLM_NUM_CTX": "big"}, nil, "CHESSINFO_LLM_NUM_CTX"},
		{"env not positive", map[string]string{"CHESSINFO_LLM_NUM_CTX": "0"}, nil, "CHESSINFO_LLM_NUM_CTX"},
		{"flag not positive", nil, []string{"--num-ctx", "-1"}, "--num-ctx"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.Load(newFlagSet(t, tt.args...), envOf(tt.env))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("err = %v, want it to mention %s", err, tt.wantErr)
			}
		})
	}
}
