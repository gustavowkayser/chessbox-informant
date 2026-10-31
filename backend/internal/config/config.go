package config

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/spf13/pflag"
)

const envPrefix = "CHESSINFO_"

type Config struct {
	DatabaseURL    string
	OllamaURL      string
	LLMModel       string
	LLMNumCtx      int
	SystemOneModel string
}

func Default() Config {
	return Config{
		DatabaseURL:    "postgres://postgres:postgres@localhost:5433/postgres",
		OllamaURL:      "http://localhost:11434",
		LLMModel:       "qwen3.5:9b",
		LLMNumCtx:      8192,
		SystemOneModel: "laya",
	}
}

type setting struct {
	env, flag string
	register  func(fs *pflag.FlagSet, def Config)
	apply     func(c *Config, v string) error
}

func stringSetting(name, flag, usage string, field func(*Config) *string) setting {
	env := envPrefix + name
	return setting{
		env:  env,
		flag: flag,
		register: func(fs *pflag.FlagSet, def Config) {
			fs.String(flag, *field(&def), fmt.Sprintf("%s (env %s)", usage, env))
		},
		apply: func(c *Config, v string) error { *field(c) = v; return nil },
	}
}

func positiveIntSetting(name, flag, usage string, field func(*Config) *int) setting {
	env := envPrefix + name
	return setting{
		env:  env,
		flag: flag,
		register: func(fs *pflag.FlagSet, def Config) {
			fs.Int(flag, *field(&def), fmt.Sprintf("%s (env %s)", usage, env))
		},
		apply: func(c *Config, v string) error {
			n, err := strconv.Atoi(v)
			if err != nil {
				return fmt.Errorf("%q is not an integer", v)
			}
			if n <= 0 {
				return errors.New("must be positive")
			}
			*field(c) = n
			return nil
		},
	}
}

var settings = []setting{
	stringSetting("DATABASE_URL", "database-url", "PostgreSQL connection URL",
		func(c *Config) *string { return &c.DatabaseURL }),
	stringSetting("OLLAMA_URL", "ollama-url", "Ollama base URL",
		func(c *Config) *string { return &c.OllamaURL }),
	stringSetting("LLM_MODEL", "model", "LLM model name",
		func(c *Config) *string { return &c.LLMModel }),
	positiveIntSetting("LLM_NUM_CTX", "num-ctx", "LLM context window size",
		func(c *Config) *int { return &c.LLMNumCtx }),
	stringSetting("SYSTEMONE_MODEL", "systemone-model", "System One model name",
		func(c *Config) *string { return &c.SystemOneModel }),
}

func RegisterFlags(fs *pflag.FlagSet) {
	def := Default()
	for _, s := range settings {
		s.register(fs, def)
	}
}

func Load(fs *pflag.FlagSet, getenv func(string) string) (Config, error) {
	c := Default()
	for _, s := range settings {
		if v := getenv(s.env); v != "" {
			if err := s.apply(&c, v); err != nil {
				return Config{}, fmt.Errorf("%s: %w", s.env, err)
			}
		}
		if f := fs.Lookup(s.flag); f != nil && f.Changed {
			if err := s.apply(&c, f.Value.String()); err != nil {
				return Config{}, fmt.Errorf("--%s: %w", s.flag, err)
			}
		}
	}
	return c, nil
}
