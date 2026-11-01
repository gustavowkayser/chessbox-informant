// Package doctor checks that the database, Ollama and the models chessinfo
// needs are ready.
package doctor

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"chessbox-informant/database"
	"chessbox-informant/internal/config"
	"chessbox-informant/internal/llm"
	"chessbox-informant/internal/ollama"
	"chessbox-informant/internal/systemone"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type Status string

const (
	OK   Status = "ok"
	Warn Status = "warn"
	Fail Status = "fail"
	Skip Status = "skip"
)

const (
	CheckDatabase  = "database"
	CheckOllama    = "ollama"
	CheckLLM       = "llm"
	CheckSystemOne = "system one"
	CheckVRAM      = "vram"
)

type Result struct {
	Check  string
	Status Status
	Detail string
}

const MinOllamaVersion = "0.40.0"

const (
	quickTimeout = 10 * time.Second
	// Loading a model from disk, on CPU or a slow disk, can take minutes.
	loadTimeout = 5 * time.Minute
)

const noTriage = "; indexing will run without triage"

type Doctor struct {
	databaseURL    string
	ollama         *ollama.Client
	llm            llm.Provider
	systemOne      *systemone.Client
	llmModel       string
	numCtx         int
	systemOneModel string
	load           bool
}

// New returns a doctor for cfg. With load, the vram check loads the LLM to see
// how much of it fits in VRAM.
func New(cfg config.Config, load bool) Doctor {
	client := ollama.New(cfg.OllamaURL)
	return Doctor{
		databaseURL:    cfg.DatabaseURL,
		ollama:         client,
		llm:            llm.NewOllama(client),
		systemOne:      systemone.New(client),
		llmModel:       cfg.LLMModel,
		numCtx:         cfg.LLMNumCtx,
		systemOneModel: cfg.SystemOneModel,
		load:           load,
	}
}

// Run reports each result as soon as its check is done, since the model
// checks can take a while.
func (d Doctor) Run(ctx context.Context, report func(Result)) {
	report(d.checkDatabase(ctx))

	res, reachable := d.checkOllama(ctx)
	report(res)
	if !reachable {
		for _, check := range []string{CheckLLM, CheckSystemOne, CheckVRAM} {
			report(Result{check, Skip, "Ollama is not reachable"})
		}
		return
	}

	llmRes := d.checkLLM(ctx)
	report(llmRes)
	report(d.checkSystemOne(ctx))
	report(d.checkVRAM(ctx, llmRes.Status == OK))
}

func (d Doctor) checkDatabase(ctx context.Context) Result {
	ctx, cancel := context.WithTimeout(ctx, quickTimeout)
	defer cancel()
	db, err := sql.Open("pgx", d.databaseURL)
	if err != nil {
		return Result{CheckDatabase, Fail, err.Error()}
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return Result{CheckDatabase, Fail, fmt.Sprintf("cannot connect: %v", err)}
	}
	migrator, err := database.NewMigrator(db)
	if err != nil {
		return Result{CheckDatabase, Fail, err.Error()}
	}
	current, target, err := migrator.GetVersions(ctx)
	if err != nil {
		return Result{CheckDatabase, Fail, err.Error()}
	}
	if current < target {
		return Result{CheckDatabase, Fail, fmt.Sprintf("schema at version %d of %d; run `chessinfo migrate`", current, target)}
	}
	return Result{CheckDatabase, OK, fmt.Sprintf("schema at version %d", current)}
}

func (d Doctor) checkOllama(ctx context.Context) (res Result, reachable bool) {
	ctx, cancel := context.WithTimeout(ctx, quickTimeout)
	defer cancel()
	v, err := d.ollama.Version(ctx)
	if err != nil {
		return Result{CheckOllama, Fail, err.Error()}, false
	}
	at := fmt.Sprintf("%s at %s", v, d.ollama.BaseURL())
	got, ok := parseVersion(v)
	if !ok {
		return Result{CheckOllama, Warn, fmt.Sprintf("%s; cannot tell whether it is %s or newer", at, MinOllamaVersion)}, true
	}
	min, _ := parseVersion(MinOllamaVersion)
	if slices.Compare(got[:], min[:]) < 0 {
		return Result{CheckOllama, Fail, fmt.Sprintf("%s; chessinfo needs Ollama %s or newer", at, MinOllamaVersion)}, true
	}
	return Result{CheckOllama, OK, at}, true
}

// parseVersion reads "major.minor.patch", ignoring a pre-release suffix such
// as "-rc1".
func parseVersion(s string) ([3]int, bool) {
	s, _, _ = strings.Cut(s, "-")
	parts := strings.Split(s, ".")
	var v [3]int
	if len(parts) != len(v) {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

func (d Doctor) checkLLM(ctx context.Context) Result {
	ctx, cancel := context.WithTimeout(ctx, quickTimeout)
	defer cancel()
	info, err := d.ollama.Show(ctx, d.llmModel)
	if err != nil {
		return Result{CheckLLM, Fail, err.Error()}
	}
	if !can(info, "completion") {
		return Result{CheckLLM, Fail, fmt.Sprintf("%s is not a generative model", d.llmModel)}
	}
	return Result{CheckLLM, OK, fmt.Sprintf("%s is pulled", d.llmModel)}
}

func (d Doctor) checkSystemOne(ctx context.Context) Result {
	ctx, cancel := context.WithTimeout(ctx, loadTimeout)
	defer cancel()
	info, err := d.ollama.Show(ctx, d.systemOneModel)
	if err != nil {
		return Result{CheckSystemOne, Warn, err.Error() + noTriage}
	}
	if !can(info, "decision") {
		return Result{CheckSystemOne, Warn, fmt.Sprintf("%s is not a System One model%s", d.systemOneModel, noTriage)}
	}

	start := time.Now()
	_, err = d.systemOne.Ask(ctx, systemone.Request{
		Model:     d.systemOneModel,
		State:     "1.e4 e5 2.Nf3 Nc6 3.Bb5",
		Questions: map[string]systemone.Question{"moves": systemone.Noul("Does the text contain chess moves?")},
		KeepAlive: ptr(time.Duration(0)),
	})
	if err != nil {
		return Result{CheckSystemOne, Warn, err.Error() + noTriage}
	}
	return Result{CheckSystemOne, OK, fmt.Sprintf("%s answered in %s, including loading", d.systemOneModel, time.Since(start).Round(100*time.Millisecond))}
}

func (d Doctor) checkVRAM(ctx context.Context, llmPulled bool) Result {
	if !d.load {
		return Result{CheckVRAM, Skip, fmt.Sprintf("run `chessinfo doctor --load` to load %s and measure it", d.llmModel)}
	}
	if !llmPulled {
		return Result{CheckVRAM, Skip, "the LLM is not available"}
	}
	ctx, cancel := context.WithTimeout(ctx, loadTimeout)
	defer cancel()

	// A chat without messages only loads the model.
	if _, err := d.llm.Chat(ctx, llm.Request{Model: d.llmModel, NumCtx: d.numCtx}); err != nil {
		return Result{CheckVRAM, Fail, fmt.Sprintf("cannot load %s: %v", d.llmModel, err)}
	}
	res := d.measure(ctx)
	if _, err := d.llm.Chat(ctx, llm.Request{Model: d.llmModel, KeepAlive: ptr(time.Duration(0))}); err != nil {
		res.Detail += fmt.Sprintf("; cannot unload it: %v", err)
	}
	return res
}

func (d Doctor) measure(ctx context.Context) Result {
	running, err := d.ollama.Running(ctx)
	if err != nil {
		return Result{CheckVRAM, Fail, err.Error()}
	}
	i := slices.IndexFunc(running, func(m ollama.RunningModel) bool { return m.Model == withTag(d.llmModel) })
	if i < 0 {
		return Result{CheckVRAM, Fail, fmt.Sprintf("%s is not loaded after loading it", d.llmModel)}
	}
	m := running[i]

	loaded := fmt.Sprintf("%s at num_ctx %d takes %.1f GB", d.llmModel, m.ContextLength, float64(m.Size)/1e9)
	switch {
	case m.Size > 0 && m.SizeVRAM >= m.Size:
		return Result{CheckVRAM, OK, loaded + ", 100% on the GPU"}
	case m.SizeVRAM == 0:
		return Result{CheckVRAM, Warn, loaded + ", all on the CPU; indexing will be slow"}
	default:
		gpu := int(100 * m.SizeVRAM / m.Size)
		return Result{CheckVRAM, Warn, fmt.Sprintf("%s, %d%% on the GPU and %d%% on the CPU; try a smaller --model or --num-ctx", loaded, gpu, 100-gpu)}
	}
}

// can reports whether the model has the capability. Capabilities missing from
// /api/show are taken as present, since there is nothing to check against.
func can(info ollama.ModelInfo, capability string) bool {
	return len(info.Capabilities) == 0 || slices.Contains(info.Capabilities, capability)
}

func withTag(model string) string {
	name := model[strings.LastIndex(model, "/")+1:]
	if strings.Contains(name, ":") {
		return model
	}
	return model + ":latest"
}

func ptr[T any](v T) *T { return &v }
