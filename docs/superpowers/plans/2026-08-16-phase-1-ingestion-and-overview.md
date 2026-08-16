# ccdash Phase 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ingest Claude Code and Codex usage from local transcripts into a durable SQLite archive, and render one TUI screen showing tokens, cost, and quota limits.

**Architecture:** Format-aware parsers live behind a single `Source` interface and emit a normalized `Record`/`LimitSample`; everything downstream is format-agnostic. The store is an archive, not a cache — rows outlive the files they came from, because Claude prunes transcripts on a rolling window. Cost is computed at query time from an editable rate table, never stored, so re-pricing never requires re-ingest.

**Tech Stack:** Go 1.26 · `modernc.org/sqlite` v1.56.0 (pure Go, no CGO) · `github.com/charmbracelet/bubbletea` v1.3.10 · `github.com/charmbracelet/lipgloss` v1.1.0. Renderers are hand-rolled (spec §11).

**Spec:** `docs/superpowers/specs/2026-08-16-llm-usage-dashboard-design.md`

## Global Constraints

- **Go module path:** `github.com/seanochang/ccdash`. Go 1.26.
- **No CGO.** `modernc.org/sqlite` only. The binary must build with `CGO_ENABLED=0`.
- **Source directories are read-only.** Never write to, move, or delete anything under `~/.claude` or `~/.codex`. The single exception is `setup-statusline` (Task 8), which is confirmed and backed up.
- **Never drop a row you cannot price.** Unpriceable rows are stored, counted in `unpriced`, and surfaced in the UI. Silent exclusion is a spec violation.
- **Every dollar figure is labelled "at API rates"**, never "spent". These are subscription plans.
- **Every limit carries a provenance** (`live` | `cached`) and its age.
- **Canonical token semantics:** `InputTok` **excludes** cache reads (Claude's convention). Codex adapters subtract `cached_input_tokens` at the edge.
- **Rates are absolute USD per million tokens**, never multipliers. An omitted rate means no charge for that component.
- **Store times as Unix seconds (`int64`)** in SQLite; convert at the boundary.
- Tests run with `go test ./... -race`. Every task ends green.

---

## File Structure

| File | Responsibility |
|---|---|
| `internal/model/types.go` | `Tool`, `Record`, `LimitKind`, `Provenance`, `LimitSample` |
| `internal/model/normalize.go` | Model-ID normalization (strip `-YYYYMMDD`) |
| `internal/model/pricing.go` | `Rate`, `Pricing`, TOML load/write, `Cost()` |
| `internal/store/schema.go` | DDL + migration |
| `internal/store/store.go` | Open, upsert, cursors, unpriced, limit change-detection |
| `internal/source/source.go` | `Source` interface, `FileRef`, `Result` |
| `internal/source/claude/claude.go` | Claude transcript parser |
| `internal/source/codex/codex.go` | Codex rollout parser (cumulative → delta) |
| `internal/source/limits/limits.go` | `~/.claude.json` + statusline capture parsers |
| `internal/ingest/ingest.go` | Discover → parse → store loop, bounded fan-out |
| `internal/agg/agg.go` | Totals, ByDay, ByModel, ByProject, LatestLimits |
| `internal/render/render.go` | `Bar`, `Sparkline`, `Braille` — pure string functions |
| `internal/tui/tui.go` | Bubble Tea model + Overview view |
| `cmd/ccdash/main.go` | Subcommand dispatch |
| `cmd/ccdash/statusline.go` | `setup-statusline` |

---

## Task 1: Module scaffolding, core types, model normalization

**Files:**
- Create: `go.mod`, `internal/model/types.go`, `internal/model/normalize.go`
- Test: `internal/model/normalize_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `model.Tool` (`ToolClaude`, `ToolCodex`), `model.Record`, `model.LimitKind` (`KindSession`, `KindWeeklyAll`, `KindWeeklyScoped`, `KindCodex5h`, `KindCodexWeekly`), `model.Provenance` (`ProvLive`, `ProvCached`), `model.LimitSample`, `model.NormalizeModel(string) string`

- [x] **Step 1: Initialize the module**

```bash
cd ccdash
go mod init github.com/seanochang/ccdash
```

- [x] **Step 2: Write the failing test**

Create `internal/model/normalize_test.go`:

```go
package model

import "testing"

func TestNormalizeModel(t *testing.T) {
	cases := []struct{ in, want string }{
		{"claude-haiku-4-5-20251001", "claude-haiku-4-5"},
		{"claude-haiku-4-5", "claude-haiku-4-5"},
		{"claude-opus-5", "claude-opus-5"},
		{"gpt-5.6-luna", "gpt-5.6-luna"},
		{"gpt-5-codex", "gpt-5-codex"},
		{"<synthetic>", "<synthetic>"},
		{"", ""},
		// an 8-digit trailing segment that is not a date-like suffix position
		{"claude-4-5", "claude-4-5"},
	}
	for _, c := range cases {
		if got := NormalizeModel(c.in); got != c.want {
			t.Errorf("NormalizeModel(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
```

- [x] **Step 3: Run test to verify it fails**

Run: `go test ./internal/model -run TestNormalizeModel -v`
Expected: FAIL — build error, `undefined: NormalizeModel`

- [x] **Step 4: Write the types**

Create `internal/model/types.go`:

```go
package model

import "time"

type Tool string

const (
	ToolClaude Tool = "claude"
	ToolCodex  Tool = "codex"
)

// Record is one priced unit of work. InputTok EXCLUDES cache reads.
type Record struct {
	ID       string
	Tool     Tool
	TS       time.Time
	Model    string
	Project  string
	Session  string
	Agent    string // "" for main loop
	Workflow string // "" when not part of a workflow
	Depth    int

	InputTok     int64
	OutputTok    int64
	ThinkingTok  int64
	CacheReadTok int64
	CacheWrite5m int64
	CacheWrite1h int64

	Anomaly bool
}

type LimitKind string

const (
	KindSession      LimitKind = "session"
	KindWeeklyAll    LimitKind = "weekly_all"
	KindWeeklyScoped LimitKind = "weekly_scoped"
	KindCodex5h      LimitKind = "codex_5h"
	KindCodexWeekly  LimitKind = "codex_weekly"
)

type Provenance string

const (
	ProvLive   Provenance = "live"
	ProvCached Provenance = "cached"
)

type LimitSample struct {
	Tool       Tool
	Kind       LimitKind
	Scope      string // model display name; "" when unscoped
	Percent    float64
	ResetsAt   *time.Time
	IsActive   bool
	ObservedAt time.Time
	Provenance Provenance
}
```

- [x] **Step 5: Write the implementation**

Create `internal/model/normalize.go`:

```go
package model

import "strings"

// NormalizeModel strips a trailing -YYYYMMDD snapshot suffix so that
// "claude-haiku-4-5-20251001" prices the same as "claude-haiku-4-5".
func NormalizeModel(id string) string {
	i := strings.LastIndex(id, "-")
	if i < 0 || i == len(id)-1 {
		return id
	}
	suffix := id[i+1:]
	if len(suffix) != 8 {
		return id
	}
	for _, r := range suffix {
		if r < '0' || r > '9' {
			return id
		}
	}
	return id[:i]
}
```

- [x] **Step 6: Run test to verify it passes**

Run: `go test ./internal/model -run TestNormalizeModel -v`
Expected: PASS

- [x] **Step 7: Commit**

```bash
git add go.mod internal/model
git commit -m "feat(model): core types and model-ID normalization"
```

---

## Task 2: Pricing table

**Files:**
- Create: `internal/model/pricing.go`
- Test: `internal/model/pricing_test.go`

**Interfaces:**
- Consumes: `model.Record` (Task 1)
- Produces: `model.Rate{Input, CachedInput, CacheWrite5m, CacheWrite1h, Output float64}`, `model.Pricing`, `model.DefaultPricing() *Pricing`, `model.LoadPricing(path string) (*Pricing, error)`, `(*Pricing).Cost(r Record) (float64, bool)`

- [x] **Step 1: Write the failing test**

Create `internal/model/pricing_test.go`:

```go
package model

import (
	"math"
	"testing"
	"time"
)

func approx(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestCostCacheTiers(t *testing.T) {
	p := DefaultPricing()
	// claude-opus-5: input 5.00, cached 0.50, w5m 6.25, w1h 10.00, output 25.00
	r := Record{
		Model:        "claude-opus-5",
		InputTok:     1_000_000,
		OutputTok:    1_000_000,
		CacheReadTok: 1_000_000,
		CacheWrite5m: 1_000_000,
		CacheWrite1h: 1_000_000,
	}
	got, ok := p.Cost(r)
	if !ok {
		t.Fatal("expected priced")
	}
	approx(t, got, 5.00+25.00+0.50+6.25+10.00)
}

func TestCostDatedModelID(t *testing.T) {
	p := DefaultPricing()
	r := Record{Model: "claude-haiku-4-5-20251001", OutputTok: 1_000_000}
	got, ok := p.Cost(r)
	if !ok {
		t.Fatal("dated model ID should price as its base ID")
	}
	approx(t, got, 5.00)
}

func TestCostUnpriced(t *testing.T) {
	p := DefaultPricing()
	for _, m := range []string{"gpt-5-codex", "<synthetic>", "totally-unknown"} {
		if _, ok := p.Cost(Record{Model: m, OutputTok: 1000}); ok {
			t.Errorf("%q must be unpriced, not guessed", m)
		}
	}
}

func TestCostOmittedRateMeansNoCharge(t *testing.T) {
	p := DefaultPricing()
	// gpt-5.5 publishes no cache-write rate; writes must cost nothing.
	r := Record{Model: "gpt-5.5", CacheWrite5m: 1_000_000}
	got, ok := p.Cost(r)
	if !ok {
		t.Fatal("expected priced")
	}
	approx(t, got, 0)
}

func TestLoadPricingCreatesDefault(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/pricing.toml"
	p, err := LoadPricing(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.Cost(Record{Model: "claude-opus-5", OutputTok: 1_000_000}); !ok {
		t.Fatal("default pricing should price claude-opus-5")
	}
	if _, err := LoadPricing(path); err != nil {
		t.Fatalf("reload failed: %v", err)
	}
	_ = time.Now
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/model -run TestCost -v`
Expected: FAIL — `undefined: DefaultPricing`

- [x] **Step 3: Write the implementation**

Create `internal/model/pricing.go`. Rates are absolute USD per million tokens; a zero field means no charge.

```go
package model

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const perMillion = 1_000_000.0

// Rate holds absolute USD-per-million-token rates. Zero means no charge.
type Rate struct {
	Input        float64
	CachedInput  float64
	CacheWrite5m float64
	CacheWrite1h float64
	Output       float64
}

type Pricing struct{ rates map[string]Rate }

// anth builds a Rate from Anthropic's published input/output pair, applying the
// documented cache multipliers (x0.10 read, x1.25 write 5m, x2.00 write 1h).
func anth(in, out float64) Rate {
	return Rate{
		Input: in, CachedInput: in * 0.10,
		CacheWrite5m: in * 1.25, CacheWrite1h: in * 2.00,
		Output: out,
	}
}

// oai builds a Rate from OpenAI's published columns. Pass 0 for a "-" cell.
func oai(in, cached, write, out float64) Rate {
	return Rate{Input: in, CachedInput: cached, CacheWrite5m: write, CacheWrite1h: write, Output: out}
}

func DefaultPricing() *Pricing {
	return &Pricing{rates: map[string]Rate{
		// Anthropic — claude-api skill table, cached 2026-06-24
		"claude-fable-5":    anth(10.00, 50.00),
		"claude-mythos-5":   anth(10.00, 50.00),
		"claude-opus-5":     anth(5.00, 25.00),
		"claude-opus-4-8":   anth(5.00, 25.00),
		"claude-opus-4-7":   anth(5.00, 25.00),
		"claude-opus-4-6":   anth(5.00, 25.00),
		"claude-sonnet-5":   anth(3.00, 15.00),
		"claude-sonnet-4-6": anth(3.00, 15.00),
		"claude-haiku-4-5":  anth(1.00, 5.00),

		// OpenAI — developers.openai.com/api/docs/pricing, retrieved 2026-08-16
		// short-context tier; "-" cells passed as 0 (no charge)
		"gpt-5.6-sol":   oai(5.00, 0.50, 6.25, 30.00),
		"gpt-5.6-terra": oai(2.00, 0.20, 2.50, 12.00),
		"gpt-5.6-luna":  oai(0.20, 0.02, 0.25, 1.20),
		"gpt-5.5":       oai(5.00, 0.50, 0, 30.00),
		"gpt-5.4":       oai(2.50, 0.25, 0, 15.00),
		"gpt-5.2":       oai(1.75, 0.175, 0, 14.00),
		"gpt-5.1":       oai(1.25, 0.125, 0, 10.00),
		"gpt-5":         oai(1.25, 0.125, 0, 10.00),
		"gpt-5-mini":    oai(0.25, 0.025, 0, 2.00),
		"gpt-5-nano":    oai(0.05, 0.005, 0, 0.40),

		// NOT priced, deliberately: gpt-5-codex, gpt-5.3-codex,
		// gpt-5.1-codex-max, gpt-5.1-codex, gpt-5.2-codex,
		// gpt-5.1-codex-mini, codex-auto-review.
		// Absent from OpenAI's published table; aliasing them to their
		// non-codex counterparts is plausible but unverified, and would
		// misprice ~78% of local Codex usage while looking authoritative.
	}}
}

// Cost returns the record's cost in USD, and false if the model has no rate.
func (p *Pricing) Cost(r Record) (float64, bool) {
	rate, ok := p.rates[NormalizeModel(r.Model)]
	if !ok {
		return 0, false
	}
	c := float64(r.InputTok)/perMillion*rate.Input +
		float64(r.OutputTok)/perMillion*rate.Output +
		float64(r.CacheReadTok)/perMillion*rate.CachedInput +
		float64(r.CacheWrite5m)/perMillion*rate.CacheWrite5m +
		float64(r.CacheWrite1h)/perMillion*rate.CacheWrite1h
	return c, true
}

// LoadPricing reads path, writing the default table there first if absent.
func LoadPricing(path string) (*Pricing, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, []byte(defaultTOML()), 0o644); err != nil {
			return nil, err
		}
		return DefaultPricing(), nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseTOML(string(b))
}

// parseTOML reads the minimal dialect this tool writes: [models."id"] tables
// with float key = value lines. A full TOML library is not warranted.
func parseTOML(s string) (*Pricing, error) {
	p := &Pricing{rates: map[string]Rate{}}
	var cur string
	for n, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[models.") {
			cur = strings.Trim(strings.TrimSuffix(strings.TrimPrefix(line, "[models."), "]"), `"`)
			if _, ok := p.rates[cur]; !ok {
				p.rates[cur] = Rate{}
			}
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || cur == "" {
			continue
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return nil, fmt.Errorf("pricing line %d: %w", n+1, err)
		}
		r := p.rates[cur]
		switch strings.TrimSpace(k) {
		case "input":
			r.Input = f
		case "cached_input":
			r.CachedInput = f
		case "cache_write_5m", "cache_write":
			r.CacheWrite5m = f
			if strings.TrimSpace(k) == "cache_write" {
				r.CacheWrite1h = f
			}
		case "cache_write_1h":
			r.CacheWrite1h = f
		case "output":
			r.Output = f
		}
		p.rates[cur] = r
	}
	return p, nil
}

func defaultTOML() string {
	var b strings.Builder
	b.WriteString("# ccdash pricing, USD per million tokens.\n")
	b.WriteString("# Anthropic: claude-api skill table, cached 2026-06-24.\n")
	b.WriteString("# OpenAI:    https://developers.openai.com/api/docs/pricing, retrieved 2026-08-16.\n")
	b.WriteString("# An omitted key means no charge for that component.\n\n")
	for _, m := range []string{
		"claude-fable-5", "claude-mythos-5", "claude-opus-5", "claude-opus-4-8",
		"claude-opus-4-7", "claude-opus-4-6", "claude-sonnet-5",
		"claude-sonnet-4-6", "claude-haiku-4-5",
		"gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna", "gpt-5.5", "gpt-5.4",
		"gpt-5.2", "gpt-5.1", "gpt-5", "gpt-5-mini", "gpt-5-nano",
	} {
		r := DefaultPricing().rates[m]
		fmt.Fprintf(&b, "[models.%q]\n", m)
		fmt.Fprintf(&b, "input = %g\n", r.Input)
		fmt.Fprintf(&b, "cached_input = %g\n", r.CachedInput)
		if r.CacheWrite5m != 0 {
			fmt.Fprintf(&b, "cache_write_5m = %g\n", r.CacheWrite5m)
		}
		if r.CacheWrite1h != 0 && r.CacheWrite1h != r.CacheWrite5m {
			fmt.Fprintf(&b, "cache_write_1h = %g\n", r.CacheWrite1h)
		}
		fmt.Fprintf(&b, "output = %g\n\n", r.Output)
	}
	b.WriteString("# Absent from OpenAI's published table. Uncomment and fill in\n")
	b.WriteString("# to price them; until then they show tokens but no cost.\n")
	for _, m := range []string{
		"gpt-5-codex", "gpt-5.3-codex", "gpt-5.1-codex-max", "gpt-5.1-codex",
		"gpt-5.2-codex", "gpt-5.1-codex-mini", "codex-auto-review",
	} {
		fmt.Fprintf(&b, "# [models.%q]\n# input = 0.0\n# cached_input = 0.0\n# output = 0.0\n\n", m)
	}
	return b.String()
}
```

- [x] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/model -v`
Expected: PASS (all five pricing tests plus normalization)

- [x] **Step 5: Commit**

```bash
git add internal/model
git commit -m "feat(model): absolute-rate pricing table with Anthropic and OpenAI defaults"
```

---

## Task 3: Store — schema, cursors, idempotent upsert, limit change-detection

**Files:**
- Create: `internal/store/schema.go`, `internal/store/store.go`
- Test: `internal/store/store_test.go`

**Interfaces:**
- Consumes: `model.Record`, `model.LimitSample`, `model.Tool` (Task 1)
- Produces: `store.Store`, `store.Open(path string) (*Store, error)`, `(*Store).Close() error`, `(*Store).UpsertRecords([]model.Record) (int, error)`, `(*Store).Cursor(path string) (size, mtime, offset int64, ok bool)`, `(*Store).SetCursor(path string, tool model.Tool, size, mtime, offset int64) error`, `(*Store).DeleteCursor(path string) error`, `(*Store).NoteUnpriced(modelID string, at time.Time) error`, `(*Store).Unpriced() (map[string]int, error)`, `(*Store).InsertLimitIfChanged(model.LimitSample) (bool, error)`, `(*Store).DB() *sql.DB`

- [x] **Step 1: Write the failing test**

Create `internal/store/store_test.go`:

```go
package store

import (
	"testing"
	"time"

	"github.com/seanochang/ccdash/internal/model"
)

func openTmp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir() + "/usage.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestUpsertIsIdempotent(t *testing.T) {
	s := openTmp(t)
	recs := []model.Record{
		{ID: "req_1", Tool: model.ToolClaude, TS: time.Unix(1000, 0), Model: "claude-opus-5", OutputTok: 10},
		{ID: "req_2", Tool: model.ToolClaude, TS: time.Unix(1001, 0), Model: "claude-opus-5", OutputTok: 20},
	}
	n, err := s.UpsertRecords(recs)
	if err != nil || n != 2 {
		t.Fatalf("first insert: n=%d err=%v", n, err)
	}
	n, err = s.UpsertRecords(recs)
	if err != nil || n != 0 {
		t.Fatalf("re-insert must be a no-op: n=%d err=%v", n, err)
	}
	var count int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM request`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("row count = %d, want 2", count)
	}
}

func TestArchiveSurvivesCursorDeletion(t *testing.T) {
	s := openTmp(t)
	if _, err := s.UpsertRecords([]model.Record{
		{ID: "req_1", Tool: model.ToolClaude, TS: time.Unix(1000, 0), Model: "claude-opus-5"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCursor("/gone.jsonl", model.ToolClaude, 10, 20, 30); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteCursor("/gone.jsonl"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM request`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("deleting a source_file row must NOT delete request rows — the store is an archive")
	}
}

func TestCursorRoundTrip(t *testing.T) {
	s := openTmp(t)
	if _, _, _, ok := s.Cursor("/a.jsonl"); ok {
		t.Fatal("unknown path should report ok=false")
	}
	if err := s.SetCursor("/a.jsonl", model.ToolCodex, 100, 200, 300); err != nil {
		t.Fatal(err)
	}
	size, mtime, off, ok := s.Cursor("/a.jsonl")
	if !ok || size != 100 || mtime != 200 || off != 300 {
		t.Fatalf("got %d %d %d %v", size, mtime, off, ok)
	}
}

func TestLimitChangeDetection(t *testing.T) {
	s := openTmp(t)
	base := model.LimitSample{
		Tool: model.ToolClaude, Kind: model.KindSession, Percent: 16,
		ObservedAt: time.Unix(1000, 0), Provenance: model.ProvLive,
	}
	for i := 0; i < 50; i++ {
		sample := base
		sample.ObservedAt = time.Unix(int64(1000+i), 0)
		inserted, err := s.InsertLimitIfChanged(sample)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 && !inserted {
			t.Fatal("first sample must insert")
		}
		if i > 0 && inserted {
			t.Fatalf("unchanged sample %d must not insert", i)
		}
	}
	changed := base
	changed.Percent = 17
	changed.ObservedAt = time.Unix(2000, 0)
	if inserted, err := s.InsertLimitIfChanged(changed); err != nil || !inserted {
		t.Fatalf("changed percent must insert: %v %v", inserted, err)
	}
	var count int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM limit_sample`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("limit_sample rows = %d, want 2", count)
	}
}

func TestUnpricedTracking(t *testing.T) {
	s := openTmp(t)
	for i := 0; i < 3; i++ {
		if err := s.NoteUnpriced("gpt-5-codex", time.Unix(int64(1000+i), 0)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Unpriced()
	if err != nil {
		t.Fatal(err)
	}
	if got["gpt-5-codex"] != 3 {
		t.Fatalf("count = %d, want 3", got["gpt-5-codex"])
	}
}
```

- [x] **Step 2: Run test to verify it fails**

```bash
go get modernc.org/sqlite@v1.56.0
go test ./internal/store -v
```
Expected: FAIL — `undefined: Open`

- [x] **Step 3: Write the schema**

Create `internal/store/schema.go`:

```go
package store

const schemaSQL = `
CREATE TABLE IF NOT EXISTS request (
  id TEXT PRIMARY KEY, tool TEXT NOT NULL, ts INTEGER NOT NULL,
  model TEXT NOT NULL, project TEXT, session TEXT,
  agent TEXT, workflow TEXT, depth INTEGER DEFAULT 0,
  in_tok INTEGER, out_tok INTEGER, think_tok INTEGER,
  cache_read INTEGER, cache_w5m INTEGER, cache_w1h INTEGER,
  anomaly INTEGER DEFAULT 0
);
CREATE INDEX IF NOT EXISTS request_ts      ON request(ts);
CREATE INDEX IF NOT EXISTS request_project ON request(project, ts);
CREATE INDEX IF NOT EXISTS request_model   ON request(model, ts);

-- Deliberately NOT foreign-keyed to request: removing a cursor must never
-- delete archived rows. Claude prunes transcripts; the archive outlives them.
CREATE TABLE IF NOT EXISTS source_file (
  path TEXT PRIMARY KEY, tool TEXT, size INTEGER,
  mtime INTEGER, offset INTEGER, last_seen INTEGER
);

CREATE TABLE IF NOT EXISTS unpriced (
  model TEXT PRIMARY KEY, count INTEGER, first_seen INTEGER, last_seen INTEGER
);

CREATE TABLE IF NOT EXISTS limit_sample (
  tool TEXT NOT NULL, kind TEXT NOT NULL,
  scope TEXT NOT NULL DEFAULT '',
  percent REAL NOT NULL, resets_at INTEGER,
  is_active INTEGER DEFAULT 0, observed_at INTEGER NOT NULL,
  provenance TEXT NOT NULL,
  PRIMARY KEY (tool, kind, scope, observed_at)
);
CREATE INDEX IF NOT EXISTS limit_latest
  ON limit_sample(tool, kind, scope, observed_at DESC);

CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT);
`
```

- [x] **Step 4: Write the store**

Create `internal/store/store.go`:

```go
package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"time"

	"github.com/seanochang/ccdash/internal/model"
	_ "modernc.org/sqlite"
)

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error  { return s.db.Close() }
func (s *Store) DB() *sql.DB   { return s.db }

// UpsertRecords inserts records, ignoring any whose ID already exists.
// Returns the number actually inserted.
func (s *Store) UpsertRecords(recs []model.Record) (int, error) {
	if len(recs) == 0 {
		return 0, nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO request
      (id,tool,ts,model,project,session,agent,workflow,depth,
       in_tok,out_tok,think_tok,cache_read,cache_w5m,cache_w1h,anomaly)
      VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()
	total := 0
	for _, r := range recs {
		res, err := stmt.Exec(r.ID, string(r.Tool), r.TS.Unix(), r.Model,
			r.Project, r.Session, r.Agent, r.Workflow, r.Depth,
			r.InputTok, r.OutputTok, r.ThinkingTok,
			r.CacheReadTok, r.CacheWrite5m, r.CacheWrite1h, boolInt(r.Anomaly))
		if err != nil {
			return 0, err
		}
		n, _ := res.RowsAffected()
		total += int(n)
	}
	return total, tx.Commit()
}

func (s *Store) Cursor(path string) (size, mtime, offset int64, ok bool) {
	err := s.db.QueryRow(
		`SELECT size,mtime,offset FROM source_file WHERE path=?`, path,
	).Scan(&size, &mtime, &offset)
	if err != nil {
		return 0, 0, 0, false
	}
	return size, mtime, offset, true
}

func (s *Store) SetCursor(path string, tool model.Tool, size, mtime, offset int64) error {
	_, err := s.db.Exec(`INSERT INTO source_file(path,tool,size,mtime,offset,last_seen)
      VALUES(?,?,?,?,?,?)
      ON CONFLICT(path) DO UPDATE SET
        size=excluded.size, mtime=excluded.mtime,
        offset=excluded.offset, last_seen=excluded.last_seen`,
		path, string(tool), size, mtime, offset, time.Now().Unix())
	return err
}

// DeleteCursor forgets a file. It must never touch request rows.
func (s *Store) DeleteCursor(path string) error {
	_, err := s.db.Exec(`DELETE FROM source_file WHERE path=?`, path)
	return err
}

func (s *Store) NoteUnpriced(modelID string, at time.Time) error {
	_, err := s.db.Exec(`INSERT INTO unpriced(model,count,first_seen,last_seen)
      VALUES(?,1,?,?)
      ON CONFLICT(model) DO UPDATE SET
        count=count+1, last_seen=excluded.last_seen`,
		modelID, at.Unix(), at.Unix())
	return err
}

func (s *Store) Unpriced() (map[string]int, error) {
	rows, err := s.db.Query(`SELECT model,count FROM unpriced ORDER BY count DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var m string
		var c int
		if err := rows.Scan(&m, &c); err != nil {
			return nil, err
		}
		out[m] = c
	}
	return out, rows.Err()
}

// InsertLimitIfChanged writes a sample only when percent or resets_at differs
// from the newest existing sample for the same (tool, kind, scope). The
// statusline fires on every render; storing each observation would be enormous
// and almost entirely redundant.
func (s *Store) InsertLimitIfChanged(ls model.LimitSample) (bool, error) {
	var prevPct sql.NullFloat64
	var prevReset sql.NullInt64
	err := s.db.QueryRow(`SELECT percent,resets_at FROM limit_sample
        WHERE tool=? AND kind=? AND scope=?
        ORDER BY observed_at DESC LIMIT 1`,
		string(ls.Tool), string(ls.Kind), ls.Scope).Scan(&prevPct, &prevReset)
	if err != nil && err != sql.ErrNoRows {
		return false, err
	}
	var reset sql.NullInt64
	if ls.ResetsAt != nil {
		reset = sql.NullInt64{Int64: ls.ResetsAt.Unix(), Valid: true}
	}
	if err != sql.ErrNoRows &&
		prevPct.Valid && prevPct.Float64 == ls.Percent &&
		prevReset.Valid == reset.Valid && prevReset.Int64 == reset.Int64 {
		return false, nil
	}
	_, err = s.db.Exec(`INSERT OR IGNORE INTO limit_sample
        (tool,kind,scope,percent,resets_at,is_active,observed_at,provenance)
        VALUES(?,?,?,?,?,?,?,?)`,
		string(ls.Tool), string(ls.Kind), ls.Scope, ls.Percent,
		reset, boolInt(ls.IsActive), ls.ObservedAt.Unix(), string(ls.Provenance))
	return err == nil, err
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
```

- [x] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/store -v -race`
Expected: PASS — all five tests

- [x] **Step 6: Commit**

```bash
git add internal/store go.mod go.sum
git commit -m "feat(store): archive schema, cursors, idempotent upsert, limit change-detection"
```

---

## Task 4: Source interface and Claude transcript parser

**Files:**
- Create: `internal/source/source.go`, `internal/source/claude/claude.go`
- Test: `internal/source/claude/claude_test.go`, `internal/source/claude/testdata/basic.jsonl`

**Interfaces:**
- Consumes: `model.Record`, `model.Tool` (Task 1)
- Produces: `source.FileRef{Path string; Size, Mtime int64}`, `source.Result{Records []model.Record; Limits []model.LimitSample; Offset int64}`, `source.Source` interface with `Name() model.Tool`, `Discover() ([]FileRef, error)`, `Parse(FileRef, int64) (Result, error)`; `claude.New(root string) *Source`

- [x] **Step 1: Write the fixture**

Create `internal/source/claude/testdata/basic.jsonl`. Lines 1–3 are the same request written three times (streaming duplication); line 4 is a distinct request with a 5-minute cache write.

```
{"type":"assistant","timestamp":"2026-08-15T23:58:43.829Z","sessionId":"s1","cwd":"/home/u/projA","requestId":"req_A","message":{"model":"claude-opus-5","usage":{"input_tokens":2,"cache_creation_input_tokens":100,"cache_read_input_tokens":200,"output_tokens":10,"output_tokens_details":{"thinking_tokens":4},"cache_creation":{"ephemeral_1h_input_tokens":100,"ephemeral_5m_input_tokens":0}}}}
{"type":"assistant","timestamp":"2026-08-15T23:58:43.900Z","sessionId":"s1","cwd":"/home/u/projA","requestId":"req_A","message":{"model":"claude-opus-5","usage":{"input_tokens":2,"cache_creation_input_tokens":100,"cache_read_input_tokens":200,"output_tokens":10,"output_tokens_details":{"thinking_tokens":4},"cache_creation":{"ephemeral_1h_input_tokens":100,"ephemeral_5m_input_tokens":0}}}}
{"type":"assistant","timestamp":"2026-08-15T23:58:44.010Z","sessionId":"s1","cwd":"/home/u/projA","requestId":"req_A","message":{"model":"claude-opus-5","usage":{"input_tokens":2,"cache_creation_input_tokens":100,"cache_read_input_tokens":200,"output_tokens":10,"output_tokens_details":{"thinking_tokens":4},"cache_creation":{"ephemeral_1h_input_tokens":100,"ephemeral_5m_input_tokens":0}}}}
{"type":"user","timestamp":"2026-08-15T23:58:45.000Z","message":{"content":"no usage here"}}
{"type":"assistant","timestamp":"2026-08-15T23:58:46.000Z","sessionId":"s1","cwd":"/home/u/projA","requestId":"req_B","message":{"model":"claude-haiku-4-5-20251001","usage":{"input_tokens":5,"cache_creation_input_tokens":50,"cache_read_input_tokens":0,"output_tokens":7,"cache_creation":{"ephemeral_1h_input_tokens":0,"ephemeral_5m_input_tokens":50}}}}
```

- [x] **Step 2: Write the failing test**

Create `internal/source/claude/claude_test.go`:

```go
package claude

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/seanochang/ccdash/internal/source"
)

func parseFixture(t *testing.T, name string) source.Result {
	t.Helper()
	p := filepath.Join("testdata", name)
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	res, err := New("").Parse(source.FileRef{Path: p, Size: st.Size(), Mtime: st.ModTime().Unix()}, 0)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestDedupeByRequestID(t *testing.T) {
	res := parseFixture(t, "basic.jsonl")
	if len(res.Records) != 2 {
		t.Fatalf("got %d records, want 2 (three duplicates of req_A collapse to one)", len(res.Records))
	}
}

func TestCacheTiersSplit(t *testing.T) {
	res := parseFixture(t, "basic.jsonl")
	byID := map[string]int{}
	for i, r := range res.Records {
		byID[r.ID] = i
	}
	a := res.Records[byID["req_A"]]
	if a.CacheWrite1h != 100 || a.CacheWrite5m != 0 {
		t.Errorf("req_A cache tiers: 1h=%d 5m=%d, want 100/0", a.CacheWrite1h, a.CacheWrite5m)
	}
	if a.CacheReadTok != 200 || a.InputTok != 2 || a.ThinkingTok != 4 {
		t.Errorf("req_A tokens: read=%d in=%d think=%d", a.CacheReadTok, a.InputTok, a.ThinkingTok)
	}
	b := res.Records[byID["req_B"]]
	if b.CacheWrite5m != 50 || b.CacheWrite1h != 0 {
		t.Errorf("req_B cache tiers: 5m=%d 1h=%d, want 50/0", b.CacheWrite5m, b.CacheWrite1h)
	}
}

func TestProjectAndModelCaptured(t *testing.T) {
	res := parseFixture(t, "basic.jsonl")
	for _, r := range res.Records {
		if r.Project != "/home/u/projA" {
			t.Errorf("project = %q", r.Project)
		}
		if r.Session != "s1" {
			t.Errorf("session = %q", r.Session)
		}
	}
}

func TestSubagentAttributionFromPath(t *testing.T) {
	agent, workflow := attribution(
		"/x/projects/p/sess/subagents/workflows/wf_83fc0078-2e2/agent-a9caf5b01.jsonl")
	if workflow != "wf_83fc0078-2e2" {
		t.Errorf("workflow = %q", workflow)
	}
	if agent != "agent-a9caf5b01" {
		t.Errorf("agent = %q", agent)
	}
	agent, workflow = attribution("/x/projects/p/sess.jsonl")
	if agent != "" || workflow != "" {
		t.Errorf("main-loop path should yield empty attribution, got %q/%q", agent, workflow)
	}
}
```

- [x] **Step 3: Run test to verify it fails**

Run: `go test ./internal/source/claude -v`
Expected: FAIL — `undefined: New`

- [x] **Step 4: Write the source interface**

Create `internal/source/source.go`:

```go
package source

import "github.com/seanochang/ccdash/internal/model"

type FileRef struct {
	Path  string
	Size  int64
	Mtime int64
}

type Result struct {
	Records []model.Record
	Limits  []model.LimitSample
	Offset  int64 // byte offset to resume from next time
}

// Source is the only format-aware abstraction. Everything downstream sees
// normalized Records and LimitSamples.
type Source interface {
	Name() model.Tool
	Discover() ([]FileRef, error)
	Parse(f FileRef, from int64) (Result, error)
}
```

- [x] **Step 5: Write the Claude parser**

Create `internal/source/claude/claude.go`:

```go
package claude

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/seanochang/ccdash/internal/model"
	"github.com/seanochang/ccdash/internal/source"
)

// usageMarker is the byte prefilter. Two-thirds of transcript lines fail it,
// and the ones that fail are the fat ones (message text, tool output).
var usageMarker = []byte(`"usage":{`)

type Source struct{ root string }

func New(root string) *Source { return &Source{root: root} }

func (s *Source) Name() model.Tool { return model.ToolClaude }

func (s *Source) Discover() ([]source.FileRef, error) {
	var out []source.FileRef
	err := filepath.WalkDir(s.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable subtree: skip, don't abort the walk
		}
		if d.IsDir() || !strings.HasSuffix(p, ".jsonl") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		out = append(out, source.FileRef{Path: p, Size: info.Size(), Mtime: info.ModTime().Unix()})
		return nil
	})
	return out, err
}

type entry struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	SessionID string `json:"sessionId"`
	Cwd       string `json:"cwd"`
	RequestID string `json:"requestId"`
	Message   struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage *struct {
			InputTokens              int64 `json:"input_tokens"`
			OutputTokens             int64 `json:"output_tokens"`
			CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
			OutputTokensDetails      struct {
				ThinkingTokens int64 `json:"thinking_tokens"`
			} `json:"output_tokens_details"`
			CacheCreation *struct {
				Ephemeral5m int64 `json:"ephemeral_5m_input_tokens"`
				Ephemeral1h int64 `json:"ephemeral_1h_input_tokens"`
			} `json:"cache_creation"`
		} `json:"usage"`
	} `json:"message"`
}

// attribution derives agent and workflow IDs from the transcript path.
// Subagent transcripts live at .../subagents/workflows/<wf>/agent-<id>.jsonl.
func attribution(path string) (agent, workflow string) {
	if !strings.Contains(path, "/subagents/") {
		return "", ""
	}
	agent = strings.TrimSuffix(filepath.Base(path), ".jsonl")
	if i := strings.Index(path, "/workflows/"); i >= 0 {
		rest := path[i+len("/workflows/"):]
		if j := strings.Index(rest, "/"); j >= 0 {
			workflow = rest[:j]
		}
	}
	return agent, workflow
}

func (s *Source) Parse(f source.FileRef, from int64) (source.Result, error) {
	fh, err := os.Open(f.Path)
	if err != nil {
		return source.Result{}, err
	}
	defer fh.Close()
	if from > 0 {
		if _, err := fh.Seek(from, 0); err != nil {
			return source.Result{}, err
		}
	}
	agent, workflow := attribution(f.Path)

	res := source.Result{Offset: from}
	seen := map[string]bool{}
	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		res.Offset += int64(len(line)) + 1
		if !bytes.Contains(line, usageMarker) {
			continue
		}
		var e entry
		if err := json.Unmarshal(line, &e); err != nil {
			continue // malformed line: skip it, keep the file
		}
		if e.Message.Usage == nil {
			continue
		}
		id := e.RequestID
		if id == "" {
			id = e.Message.ID
		}
		if id == "" || seen[id] {
			continue // streaming writes each request up to 3x
		}
		seen[id] = true

		ts, err := time.Parse(time.RFC3339Nano, e.Timestamp)
		if err != nil {
			ts = time.Unix(f.Mtime, 0)
		}
		u := e.Message.Usage
		w5, w1 := int64(0), int64(0)
		if u.CacheCreation != nil {
			w5, w1 = u.CacheCreation.Ephemeral5m, u.CacheCreation.Ephemeral1h
		}
		if w5 == 0 && w1 == 0 {
			w5 = u.CacheCreationInputTokens // pre-split format
		}
		res.Records = append(res.Records, model.Record{
			ID: id, Tool: model.ToolClaude, TS: ts,
			Model: e.Message.Model, Project: e.Cwd, Session: e.SessionID,
			Agent: agent, Workflow: workflow,
			InputTok: u.InputTokens, OutputTok: u.OutputTokens,
			ThinkingTok:  u.OutputTokensDetails.ThinkingTokens,
			CacheReadTok: u.CacheReadInputTokens,
			CacheWrite5m: w5, CacheWrite1h: w1,
		})
	}
	if err := sc.Err(); err != nil {
		return res, err
	}
	return res, nil
}
```

- [x] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/source/claude -v`
Expected: PASS — four tests

- [x] **Step 7: Commit**

```bash
git add internal/source
git commit -m "feat(source/claude): transcript parser with dedupe, cache tiers, subagent attribution"
```

---

## Task 5: Codex rollout parser

**Files:**
- Create: `internal/source/codex/codex.go`
- Test: `internal/source/codex/codex_test.go`, `internal/source/codex/testdata/rollout.jsonl`

**Interfaces:**
- Consumes: `source.FileRef`, `source.Result` (Task 4), `model.Record` (Task 1)
- Produces: `codex.New(root string) *Source` satisfying `source.Source`

- [x] **Step 1: Write the fixture**

Create `internal/source/codex/testdata/rollout.jsonl`. Event 2 repeats event 1's totals unchanged (the 42.7% duplicate case); event 4 restarts the accumulator.

```
{"type":"session_meta","timestamp":"2026-08-15T13:20:36.145Z","payload":{"id":"sess-1","cwd":"/home/u/projB","cli_version":"0.147.0","originator":"Codex CLI"}}
{"type":"turn_context","timestamp":"2026-08-15T13:20:37.000Z","payload":{"cwd":"/home/u/projB","model":"gpt-5.6-luna","effort":"xhigh"}}
{"type":"event_msg","timestamp":"2026-08-15T13:21:00.000Z","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":1000,"cached_input_tokens":800,"cache_write_input_tokens":50,"output_tokens":100,"reasoning_output_tokens":40,"total_tokens":1100}},"rate_limits":{"primary":{"used_percent":8.0,"window_minutes":300,"resets_at":1787404839},"secondary":{"used_percent":34.0,"window_minutes":10080,"resets_at":1787900000}}}}
{"type":"event_msg","timestamp":"2026-08-15T13:21:01.000Z","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":1000,"cached_input_tokens":800,"cache_write_input_tokens":50,"output_tokens":100,"reasoning_output_tokens":40,"total_tokens":1100}}}}
{"type":"event_msg","timestamp":"2026-08-15T13:22:00.000Z","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":2500,"cached_input_tokens":2000,"cache_write_input_tokens":50,"output_tokens":300,"reasoning_output_tokens":90,"total_tokens":2800}}}}
{"type":"event_msg","timestamp":"2026-08-15T13:23:00.000Z","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":400,"cached_input_tokens":300,"cache_write_input_tokens":0,"output_tokens":20,"reasoning_output_tokens":5,"total_tokens":420}}}}
```

- [x] **Step 2: Write the failing test**

Create `internal/source/codex/codex_test.go`:

```go
package codex

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/seanochang/ccdash/internal/model"
	"github.com/seanochang/ccdash/internal/source"
)

func parseFixture(t *testing.T) source.Result {
	t.Helper()
	p := filepath.Join("testdata", "rollout.jsonl")
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	res, err := New("").Parse(source.FileRef{Path: p, Size: st.Size(), Mtime: st.ModTime().Unix()}, 0)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestDuplicateTotalsProduceNoRecord(t *testing.T) {
	res := parseFixture(t)
	// events: #1 first, #2 duplicate (skipped), #3 delta, #4 restart => 3 records
	if len(res.Records) != 3 {
		t.Fatalf("got %d records, want 3 (the unchanged repeat must not emit)", len(res.Records))
	}
}

func TestDeltasAreDifferencesNotCumulative(t *testing.T) {
	res := parseFixture(t)
	// record 2 is event #3: totals went 1000->2500 in, 800->2000 cached,
	// 100->300 out. Canonical input EXCLUDES cache reads:
	//   input = (2500-1000) - (2000-800) = 1500 - 1200 = 300
	r := res.Records[1]
	if r.InputTok != 300 {
		t.Errorf("InputTok = %d, want 300 (delta minus cached delta)", r.InputTok)
	}
	if r.CacheReadTok != 1200 {
		t.Errorf("CacheReadTok = %d, want 1200", r.CacheReadTok)
	}
	if r.OutputTok != 200 {
		t.Errorf("OutputTok = %d, want 200", r.OutputTok)
	}
	if r.ThinkingTok != 50 {
		t.Errorf("ThinkingTok = %d, want 50", r.ThinkingTok)
	}
}

func TestAccumulatorRestartIsFlaggedNotClamped(t *testing.T) {
	res := parseFixture(t)
	r := res.Records[2] // event #4: totals dropped, a context reset
	if !r.Anomaly {
		t.Error("a decreasing counter must set Anomaly")
	}
	// delta is the new value itself (restarted from zero), not zero
	if r.InputTok != 100 { // 400 - 300 cached
		t.Errorf("InputTok = %d, want 100 (restart emits the new value)", r.InputTok)
	}
	if r.CacheReadTok != 300 {
		t.Errorf("CacheReadTok = %d, want 300", r.CacheReadTok)
	}
}

func TestModelAndProjectFromContext(t *testing.T) {
	res := parseFixture(t)
	for _, r := range res.Records {
		if r.Model != "gpt-5.6-luna" {
			t.Errorf("Model = %q", r.Model)
		}
		if r.Project != "/home/u/projB" {
			t.Errorf("Project = %q", r.Project)
		}
		if r.Session != "sess-1" {
			t.Errorf("Session = %q", r.Session)
		}
	}
}

func TestWindowMinutesMapToLimitKind(t *testing.T) {
	res := parseFixture(t)
	kinds := map[model.LimitKind]float64{}
	for _, l := range res.Limits {
		kinds[l.Kind] = l.Percent
	}
	if kinds[model.KindCodex5h] != 8.0 {
		t.Errorf("codex_5h = %v, want 8.0 (window_minutes 300)", kinds[model.KindCodex5h])
	}
	if kinds[model.KindCodexWeekly] != 34.0 {
		t.Errorf("codex_weekly = %v, want 34.0 (window_minutes 10080)", kinds[model.KindCodexWeekly])
	}
}
```

- [x] **Step 3: Run test to verify it fails**

Run: `go test ./internal/source/codex -v`
Expected: FAIL — `undefined: New`

- [x] **Step 4: Write the parser**

Create `internal/source/codex/codex.go`:

```go
package codex

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/seanochang/ccdash/internal/model"
	"github.com/seanochang/ccdash/internal/source"
)

var tokenMarker = []byte(`"token_count"`)

type Source struct{ root string }

func New(root string) *Source { return &Source{root: root} }

func (s *Source) Name() model.Tool { return model.ToolCodex }

func (s *Source) Discover() ([]source.FileRef, error) {
	var out []source.FileRef
	err := filepath.WalkDir(s.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(p, ".jsonl") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		out = append(out, source.FileRef{Path: p, Size: info.Size(), Mtime: info.ModTime().Unix()})
		return nil
	})
	return out, err
}

type usage struct {
	InputTokens     int64 `json:"input_tokens"`
	CachedInput     int64 `json:"cached_input_tokens"`
	CacheWriteInput int64 `json:"cache_write_input_tokens"`
	OutputTokens    int64 `json:"output_tokens"`
	ReasoningOutput int64 `json:"reasoning_output_tokens"`
	TotalTokens     int64 `json:"total_tokens"`
}

type window struct {
	UsedPercent   float64 `json:"used_percent"`
	WindowMinutes int     `json:"window_minutes"`
	ResetsAt      int64   `json:"resets_at"`
}

type line struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Payload   struct {
		Type string `json:"type"`
		// session_meta
		ID  string `json:"id"`
		Cwd string `json:"cwd"`
		// turn_context
		Model string `json:"model"`
		// token_count
		Info *struct {
			TotalTokenUsage *usage `json:"total_token_usage"`
		} `json:"info"`
		RateLimits *struct {
			Primary   *window `json:"primary"`
			Secondary *window `json:"secondary"`
		} `json:"rate_limits"`
	} `json:"payload"`
}

// kindFor maps a limit's window length to its kind. Window length identifies
// the limit; primary/secondary position does not.
func kindFor(minutes int) (model.LimitKind, bool) {
	switch {
	case minutes >= 240 && minutes <= 360: // ~5 hours
		return model.KindCodex5h, true
	case minutes >= 9000 && minutes <= 11000: // ~7 days
		return model.KindCodexWeekly, true
	}
	return "", false
}

// delta returns cur-prev, or cur when the counter restarted (cur < prev).
func delta(prev, cur int64) (int64, bool) {
	if cur < prev {
		return cur, true // accumulator restart, e.g. a context reset
	}
	return cur - prev, false
}

func (s *Source) Parse(f source.FileRef, from int64) (source.Result, error) {
	fh, err := os.Open(f.Path)
	if err != nil {
		return source.Result{}, err
	}
	defer fh.Close()
	if from > 0 {
		if _, err := fh.Seek(from, 0); err != nil {
			return source.Result{}, err
		}
	}

	res := source.Result{Offset: from}
	var sessionID, cwd, modelID string
	var prev usage
	havePrev := false
	idx := 0

	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		raw := sc.Bytes()
		res.Offset += int64(len(raw)) + 1
		idx++

		var l line
		if err := json.Unmarshal(raw, &l); err != nil {
			continue
		}
		switch {
		case l.Type == "session_meta":
			sessionID, cwd = l.Payload.ID, l.Payload.Cwd
			continue
		case l.Type == "turn_context":
			if l.Payload.Model != "" {
				modelID = l.Payload.Model
			}
			if l.Payload.Cwd != "" {
				cwd = l.Payload.Cwd
			}
			continue
		}
		if !bytes.Contains(raw, tokenMarker) || l.Payload.Type != "token_count" {
			continue
		}

		ts, err := time.Parse(time.RFC3339Nano, l.Timestamp)
		if err != nil {
			ts = time.Unix(f.Mtime, 0)
		}

		if rl := l.Payload.RateLimits; rl != nil {
			for _, w := range []*window{rl.Primary, rl.Secondary} {
				if w == nil {
					continue
				}
				kind, ok := kindFor(w.WindowMinutes)
				if !ok {
					continue
				}
				var reset *time.Time
				if w.ResetsAt > 0 {
					r := time.Unix(w.ResetsAt, 0)
					reset = &r
				}
				res.Limits = append(res.Limits, model.LimitSample{
					Tool: model.ToolCodex, Kind: kind, Percent: w.UsedPercent,
					ResetsAt: reset, ObservedAt: ts, Provenance: model.ProvLive,
				})
			}
		}

		if l.Payload.Info == nil || l.Payload.Info.TotalTokenUsage == nil {
			continue
		}
		cur := *l.Payload.Info.TotalTokenUsage
		if !havePrev {
			prev, havePrev = usage{}, true
		}

		dIn, a1 := delta(prev.InputTokens, cur.InputTokens)
		dCached, a2 := delta(prev.CachedInput, cur.CachedInput)
		dWrite, a3 := delta(prev.CacheWriteInput, cur.CacheWriteInput)
		dOut, a4 := delta(prev.OutputTokens, cur.OutputTokens)
		dReason, a5 := delta(prev.ReasoningOutput, cur.ReasoningOutput)
		prev = cur

		if dIn == 0 && dCached == 0 && dWrite == 0 && dOut == 0 {
			continue // unchanged repeat: 42.7% of events
		}

		// Codex input_tokens INCLUDES cached; the canonical form excludes it.
		fresh := dIn - dCached
		if fresh < 0 {
			fresh = 0
		}
		res.Records = append(res.Records, model.Record{
			ID:      fmt.Sprintf("%s:%d", sessionID, idx),
			Tool:    model.ToolCodex,
			TS:      ts,
			Model:   modelID,
			Project: cwd,
			Session: sessionID,
			InputTok:     fresh,
			OutputTok:    dOut,
			ThinkingTok:  dReason,
			CacheReadTok: dCached,
			CacheWrite5m: dWrite,
			Anomaly:      a1 || a2 || a3 || a4 || a5,
		})
	}
	if err := sc.Err(); err != nil {
		return res, err
	}
	return res, nil
}
```

- [x] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/source/codex -v`
Expected: PASS — five tests

- [x] **Step 6: Commit**

```bash
git add internal/source/codex
git commit -m "feat(source/codex): cumulative-to-delta parser with restart flagging"
```

---

## Task 6: Limits sources — `~/.claude.json` and statusline capture

**Files:**
- Create: `internal/source/limits/limits.go`
- Test: `internal/source/limits/limits_test.go`, `internal/source/limits/testdata/claude.json`, `internal/source/limits/testdata/statusline.jsonl`

**Interfaces:**
- Consumes: `source.FileRef`, `source.Result` (Task 4), `model.LimitSample` (Task 1)
- Produces: `limits.NewClaudeJSON(path string) *ClaudeJSON`, `limits.NewStatusline(path string) *Statusline`, both satisfying `source.Source`

- [x] **Step 1: Write the fixtures**

Create `internal/source/limits/testdata/claude.json`:

```json
{"cachedUsageUtilization":{"fetchedAtMs":1786803772956,"utilization":{"limits":[
{"kind":"session","group":"session","percent":16,"severity":"normal","resets_at":"2026-08-15T15:19:59.941567+00:00","scope":null,"is_active":false},
{"kind":"weekly_all","group":"weekly","percent":15,"severity":"normal","resets_at":"2026-08-17T16:59:59.941595+00:00","scope":null,"is_active":false},
{"kind":"weekly_scoped","group":"weekly","percent":19,"severity":"normal","resets_at":"2026-08-17T16:59:59.941797+00:00","scope":{"model":{"id":null,"display_name":"Fable"}},"is_active":true}
]}}}
```

Create `internal/source/limits/testdata/statusline.jsonl`:

```
{"model":{"display_name":"Opus 5"},"rate_limits":{"five_hour":{"used_percentage":21,"resets_at":"2026-08-16T20:00:00+00:00"},"seven_day":{"used_percentage":15,"resets_at":"2026-08-17T16:59:59+00:00"}}}
{"model":{"display_name":"Opus 5"},"rate_limits":{"five_hour":{"used_percentage":22,"resets_at":"2026-08-16T20:00:00+00:00"},"seven_day":{"used_percentage":15,"resets_at":"2026-08-17T16:59:59+00:00"}}}
```

- [x] **Step 2: Write the failing test**

Create `internal/source/limits/limits_test.go`:

```go
package limits

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/seanochang/ccdash/internal/model"
	"github.com/seanochang/ccdash/internal/source"
)

func ref(t *testing.T, name string) source.FileRef {
	t.Helper()
	p := filepath.Join("testdata", name)
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return source.FileRef{Path: p, Size: st.Size(), Mtime: st.ModTime().Unix()}
}

func TestClaudeJSONYieldsThreeLimits(t *testing.T) {
	f := ref(t, "claude.json")
	res, err := NewClaudeJSON(f.Path).Parse(f, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Limits) != 3 {
		t.Fatalf("got %d limits, want 3", len(res.Limits))
	}
	byKind := map[model.LimitKind]model.LimitSample{}
	for _, l := range res.Limits {
		byKind[l.Kind] = l
	}
	if byKind[model.KindSession].Percent != 16 {
		t.Errorf("session percent = %v", byKind[model.KindSession].Percent)
	}
	if byKind[model.KindWeeklyAll].Percent != 15 {
		t.Errorf("weekly_all percent = %v", byKind[model.KindWeeklyAll].Percent)
	}
	scoped := byKind[model.KindWeeklyScoped]
	if scoped.Percent != 19 || scoped.Scope != "Fable" || !scoped.IsActive {
		t.Errorf("weekly_scoped = %+v, want 19%% scope=Fable active=true", scoped)
	}
	for _, l := range res.Limits {
		if l.Provenance != model.ProvCached {
			t.Errorf("%s provenance = %q, want cached", l.Kind, l.Provenance)
		}
		if l.ObservedAt.UnixMilli() != 1786803772956 {
			t.Errorf("ObservedAt must come from fetchedAtMs, got %v", l.ObservedAt)
		}
	}
}

func TestStatuslineYieldsLiveLimits(t *testing.T) {
	f := ref(t, "statusline.jsonl")
	res, err := NewStatusline(f.Path).Parse(f, 0)
	if err != nil {
		t.Fatal(err)
	}
	// two payload lines x two limits each
	if len(res.Limits) != 4 {
		t.Fatalf("got %d limits, want 4", len(res.Limits))
	}
	for _, l := range res.Limits {
		if l.Provenance != model.ProvLive {
			t.Errorf("provenance = %q, want live", l.Provenance)
		}
		if l.Tool != model.ToolClaude {
			t.Errorf("tool = %q", l.Tool)
		}
	}
	if res.Limits[0].Kind != model.KindSession || res.Limits[0].Percent != 21 {
		t.Errorf("first limit = %+v, want session 21%%", res.Limits[0])
	}
	if res.Limits[1].Kind != model.KindWeeklyAll || res.Limits[1].Percent != 15 {
		t.Errorf("second limit = %+v, want weekly_all 15%%", res.Limits[1])
	}
}

func TestClaudeJSONMissingFileIsNotAnError(t *testing.T) {
	res, err := NewClaudeJSON("/nonexistent/claude.json").Parse(
		source.FileRef{Path: "/nonexistent/claude.json"}, 0)
	if err != nil {
		t.Fatalf("a missing optional source must not error: %v", err)
	}
	if len(res.Limits) != 0 {
		t.Fatal("expected no limits")
	}
}
```

- [x] **Step 3: Run test to verify it fails**

Run: `go test ./internal/source/limits -v`
Expected: FAIL — `undefined: NewClaudeJSON`

- [x] **Step 4: Write the implementation**

Create `internal/source/limits/limits.go`:

```go
package limits

import (
	"bufio"
	"encoding/json"
	"os"
	"time"

	"github.com/seanochang/ccdash/internal/model"
	"github.com/seanochang/ccdash/internal/source"
)

func parseTime(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return nil
	}
	return &t
}

// ---------- ~/.claude.json (cached) ----------

type ClaudeJSON struct{ path string }

func NewClaudeJSON(path string) *ClaudeJSON { return &ClaudeJSON{path: path} }

func (c *ClaudeJSON) Name() model.Tool { return model.ToolClaude }

func (c *ClaudeJSON) Discover() ([]source.FileRef, error) {
	st, err := os.Stat(c.path)
	if err != nil {
		return nil, nil // optional source
	}
	return []source.FileRef{{Path: c.path, Size: st.Size(), Mtime: st.ModTime().Unix()}}, nil
}

type claudeJSONDoc struct {
	Cached struct {
		FetchedAtMs int64 `json:"fetchedAtMs"`
		Utilization struct {
			Limits []struct {
				Kind     string  `json:"kind"`
				Percent  float64 `json:"percent"`
				ResetsAt string  `json:"resets_at"`
				IsActive bool    `json:"is_active"`
				Scope    *struct {
					Model *struct {
						DisplayName string `json:"display_name"`
					} `json:"model"`
				} `json:"scope"`
			} `json:"limits"`
		} `json:"utilization"`
	} `json:"cachedUsageUtilization"`
}

func (c *ClaudeJSON) Parse(f source.FileRef, _ int64) (source.Result, error) {
	b, err := os.ReadFile(f.Path)
	if err != nil {
		return source.Result{}, nil // absent is fine
	}
	var doc claudeJSONDoc
	if err := json.Unmarshal(b, &doc); err != nil {
		return source.Result{}, nil
	}
	observed := time.UnixMilli(doc.Cached.FetchedAtMs)
	var res source.Result
	for _, l := range doc.Cached.Utilization.Limits {
		kind := model.LimitKind(l.Kind)
		switch kind {
		case model.KindSession, model.KindWeeklyAll, model.KindWeeklyScoped:
		default:
			continue // unknown kind: ignore rather than guess
		}
		scope := ""
		if l.Scope != nil && l.Scope.Model != nil {
			scope = l.Scope.Model.DisplayName
		}
		res.Limits = append(res.Limits, model.LimitSample{
			Tool: model.ToolClaude, Kind: kind, Scope: scope,
			Percent: l.Percent, ResetsAt: parseTime(l.ResetsAt),
			IsActive: l.IsActive, ObservedAt: observed,
			Provenance: model.ProvCached,
		})
	}
	res.Offset = f.Size
	return res, nil
}

// ---------- statusline capture (live) ----------

type Statusline struct{ path string }

func NewStatusline(path string) *Statusline { return &Statusline{path: path} }

func (s *Statusline) Name() model.Tool { return model.ToolClaude }

func (s *Statusline) Discover() ([]source.FileRef, error) {
	st, err := os.Stat(s.path)
	if err != nil {
		return nil, nil
	}
	return []source.FileRef{{Path: s.path, Size: st.Size(), Mtime: st.ModTime().Unix()}}, nil
}

type statuslinePayload struct {
	RateLimits struct {
		FiveHour *struct {
			UsedPercentage float64 `json:"used_percentage"`
			ResetsAt       string  `json:"resets_at"`
		} `json:"five_hour"`
		SevenDay *struct {
			UsedPercentage float64 `json:"used_percentage"`
			ResetsAt       string  `json:"resets_at"`
		} `json:"seven_day"`
	} `json:"rate_limits"`
}

func (s *Statusline) Parse(f source.FileRef, from int64) (source.Result, error) {
	fh, err := os.Open(f.Path)
	if err != nil {
		return source.Result{}, nil
	}
	defer fh.Close()
	if from > 0 {
		if _, err := fh.Seek(from, 0); err != nil {
			return source.Result{}, err
		}
	}
	res := source.Result{Offset: from}
	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		raw := sc.Bytes()
		res.Offset += int64(len(raw)) + 1
		var p statuslinePayload
		if err := json.Unmarshal(raw, &p); err != nil {
			continue
		}
		// The payload has no timestamp; the capture line's arrival is the
		// best available observation time, approximated by file mtime.
		observed := time.Unix(f.Mtime, 0)
		if p.RateLimits.FiveHour != nil {
			res.Limits = append(res.Limits, model.LimitSample{
				Tool: model.ToolClaude, Kind: model.KindSession,
				Percent:  p.RateLimits.FiveHour.UsedPercentage,
				ResetsAt: parseTime(p.RateLimits.FiveHour.ResetsAt),
				ObservedAt: observed, Provenance: model.ProvLive,
			})
		}
		if p.RateLimits.SevenDay != nil {
			res.Limits = append(res.Limits, model.LimitSample{
				Tool: model.ToolClaude, Kind: model.KindWeeklyAll,
				Percent:  p.RateLimits.SevenDay.UsedPercentage,
				ResetsAt: parseTime(p.RateLimits.SevenDay.ResetsAt),
				ObservedAt: observed, Provenance: model.ProvLive,
			})
		}
	}
	return res, sc.Err()
}
```

- [x] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/source/limits -v`
Expected: PASS — three tests

- [x] **Step 6: Commit**

```bash
git add internal/source/limits
git commit -m "feat(source/limits): cached claude.json and live statusline limit parsers"
```

---

## Task 7: Ingest orchestration and the `ingest` / `limits` commands

**Files:**
- Create: `internal/ingest/ingest.go`, `cmd/ccdash/main.go`
- Test: `internal/ingest/ingest_test.go`

**Interfaces:**
- Consumes: everything from Tasks 1–6
- Produces: `ingest.Stats{Files, Scanned, Inserted, Limits, Unpriced int}`, `ingest.Run(st *store.Store, srcs []source.Source, p *model.Pricing, full bool) (Stats, error)`, `ingest.DefaultSources(home string) []source.Source`

- [x] **Step 1: Write the failing test**

Create `internal/ingest/ingest_test.go`:

```go
package ingest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/seanochang/ccdash/internal/model"
	"github.com/seanochang/ccdash/internal/source/claude"
	"github.com/seanochang/ccdash/internal/store"
)

const fixture = `{"type":"assistant","timestamp":"2026-08-15T00:00:00.000Z","sessionId":"s","cwd":"/p","requestId":"r1","message":{"model":"claude-opus-5","usage":{"input_tokens":1,"output_tokens":2,"cache_read_input_tokens":3,"cache_creation_input_tokens":0}}}
{"type":"assistant","timestamp":"2026-08-15T00:00:01.000Z","sessionId":"s","cwd":"/p","requestId":"r2","message":{"model":"mystery-model","usage":{"input_tokens":1,"output_tokens":2}}}
`

func setup(t *testing.T) (*store.Store, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.jsonl"), []byte(fixture), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st, dir
}

func TestRunIsIdempotent(t *testing.T) {
	st, dir := setup(t)
	srcs := []sourceIface{claude.New(dir)}
	p := model.DefaultPricing()

	s1, err := Run(st, toSources(srcs), p, false)
	if err != nil {
		t.Fatal(err)
	}
	if s1.Inserted != 2 {
		t.Fatalf("first run inserted %d, want 2", s1.Inserted)
	}
	s2, err := Run(st, toSources(srcs), p, false)
	if err != nil {
		t.Fatal(err)
	}
	if s2.Inserted != 0 {
		t.Fatalf("second run inserted %d, want 0 (cursor should skip unchanged file)", s2.Inserted)
	}
}

func TestUnpricedModelIsRecordedNotDropped(t *testing.T) {
	st, dir := setup(t)
	if _, err := Run(st, toSources([]sourceIface{claude.New(dir)}), model.DefaultPricing(), false); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM request`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("both rows must be stored even when one is unpriceable, got %d", count)
	}
	up, err := st.Unpriced()
	if err != nil {
		t.Fatal(err)
	}
	if up["mystery-model"] != 1 {
		t.Fatalf("unpriced tracking = %v, want mystery-model:1", up)
	}
}
```

Add this helper at the bottom of the same test file (Go needs the interface conversion to be explicit):

```go
type sourceIface = interface {
	Name() model.Tool
	Discover() ([]sourceFileRef, error)
	Parse(sourceFileRef, int64) (sourceResult, error)
}
```

> **Note for the implementer:** the two aliases above exist only to keep this
> test file readable. Replace them with direct `source.Source`, `source.FileRef`,
> and `source.Result` references and delete `toSources` — the test should read:
> `Run(st, []source.Source{claude.New(dir)}, p, false)`. Written out here so the
> intent is unambiguous.

- [x] **Step 2: Simplify the test to the real signature**

Replace the alias block and both `toSources(...)` calls so the file uses `[]source.Source{claude.New(dir)}` directly, and add `"github.com/seanochang/ccdash/internal/source"` to the imports.

- [x] **Step 3: Run test to verify it fails**

Run: `go test ./internal/ingest -v`
Expected: FAIL — `undefined: Run`

- [x] **Step 4: Write the implementation**

Create `internal/ingest/ingest.go`:

```go
package ingest

import (
	"runtime"
	"sync"

	"github.com/seanochang/ccdash/internal/model"
	"github.com/seanochang/ccdash/internal/source"
	"github.com/seanochang/ccdash/internal/source/claude"
	"github.com/seanochang/ccdash/internal/source/codex"
	"github.com/seanochang/ccdash/internal/source/limits"
	"github.com/seanochang/ccdash/internal/store"
)

type Stats struct {
	Files    int
	Scanned  int
	Inserted int
	Limits   int
	Unpriced int
}

func DefaultSources(home string) []source.Source {
	return []source.Source{
		claude.New(home + "/.claude/projects"),
		codex.New(home + "/.codex/sessions"),
		limits.NewClaudeJSON(home + "/.claude.json"),
		limits.NewStatusline(home + "/.local/share/ccdash/statusline.jsonl"),
	}
}

// Run discovers, parses, and stores. Fan-out is bounded by NumCPU: unbounded
// concurrent reads over hundreds of files saturate the device queue and starve
// the rest of the machine.
func Run(st *store.Store, srcs []source.Source, p *model.Pricing, full bool) (Stats, error) {
	var stats Stats
	for _, src := range srcs {
		files, err := src.Discover()
		if err != nil {
			return stats, err
		}
		type job struct {
			f   source.FileRef
			res source.Result
			err error
		}
		jobs := make([]job, 0, len(files))
		for _, f := range files {
			from := int64(0)
			if !full {
				size, mtime, off, ok := st.Cursor(f.Path)
				if ok {
					if size == f.Size && mtime == f.Mtime {
						continue // unchanged
					}
					if f.Size >= size {
						from = off
					} // else truncated/rewritten: reparse from 0
				}
			}
			jobs = append(jobs, job{f: f})
			jobs[len(jobs)-1].f.Size = f.Size
			_ = from
		}
		// Second pass computes offsets again (cheap) and parses concurrently.
		sem := make(chan struct{}, runtime.NumCPU())
		var wg sync.WaitGroup
		for i := range jobs {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				f := jobs[i].f
				from := int64(0)
				if !full {
					size, _, off, ok := st.Cursor(f.Path)
					if ok && f.Size >= size {
						from = off
					}
				}
				jobs[i].res, jobs[i].err = src.Parse(f, from)
			}(i)
		}
		wg.Wait()

		for _, j := range jobs {
			if j.err != nil {
				continue // unreadable file: skip, keep going
			}
			stats.Files++
			stats.Scanned += len(j.res.Records)
			n, err := st.UpsertRecords(j.res.Records)
			if err != nil {
				return stats, err
			}
			stats.Inserted += n
			for _, r := range j.res.Records {
				if _, ok := p.Cost(r); !ok {
					if err := st.NoteUnpriced(model.NormalizeModel(r.Model), r.TS); err != nil {
						return stats, err
					}
					stats.Unpriced++
				}
			}
			for _, l := range j.res.Limits {
				ins, err := st.InsertLimitIfChanged(l)
				if err != nil {
					return stats, err
				}
				if ins {
					stats.Limits++
				}
			}
			if err := st.SetCursor(j.f.Path, src.Name(), j.f.Size, j.f.Mtime, j.res.Offset); err != nil {
				return stats, err
			}
		}
	}
	return stats, nil
}
```

- [x] **Step 5: Simplify the double offset computation**

The `jobs` construction above computes `from` twice. Collapse it: store `from` on the job struct in the first pass and use it in the goroutine. Run `go vet ./...` and confirm no unused variables remain.

- [x] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/ingest -v -race`
Expected: PASS — two tests

- [x] **Step 7: Write the CLI**

Create `cmd/ccdash/main.go`:

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/seanochang/ccdash/internal/agg"
	"github.com/seanochang/ccdash/internal/ingest"
	"github.com/seanochang/ccdash/internal/model"
	"github.com/seanochang/ccdash/internal/store"
)

const version = "0.1.0"

func dataDir() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "ccdash")
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".local", "share", "ccdash")
}

func configDir() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "ccdash")
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".config", "ccdash")
}

func main() {
	cmd := ""
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	switch cmd {
	case "version":
		fmt.Println(version)
	case "setup-statusline":
		if err := setupStatusline(); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "ingest", "":
		full := false
		for _, a := range os.Args[2:] {
			if a == "--full" {
				full = true
			}
		}
		if err := runIngest(full); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "limits":
		if err := printLimits(); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", cmd)
		os.Exit(2)
	}
}

func open() (*store.Store, *model.Pricing, error) {
	st, err := store.Open(filepath.Join(dataDir(), "usage.db"))
	if err != nil {
		return nil, nil, err
	}
	p, err := model.LoadPricing(filepath.Join(configDir(), "pricing.toml"))
	if err != nil {
		st.Close()
		return nil, nil, err
	}
	return st, p, nil
}

func runIngest(full bool) error {
	st, p, err := open()
	if err != nil {
		return err
	}
	defer st.Close()
	home, _ := os.UserHomeDir()
	stats, err := ingest.Run(st, ingest.DefaultSources(home), p, full)
	if err != nil {
		return err
	}
	fmt.Printf("files %d · records %d · inserted %d · limit samples %d\n",
		stats.Files, stats.Scanned, stats.Inserted, stats.Limits)

	tot, err := agg.Totals(st.DB(), p, agg.Filter{})
	if err != nil {
		return err
	}
	fmt.Printf("%d requests · %.1fM tokens · $%.2f at API rates\n",
		tot.Requests, float64(tot.Tokens)/1e6, tot.Cost)

	up, err := st.Unpriced()
	if err != nil {
		return err
	}
	if len(up) > 0 {
		fmt.Printf("unpriced models (%d): ", len(up))
		for m, c := range up {
			fmt.Printf("%s×%d ", m, c)
		}
		fmt.Println()
	}
	return nil
}

func printLimits() error {
	st, _, err := open()
	if err != nil {
		return err
	}
	defer st.Close()
	states, err := agg.LatestLimits(st.DB())
	if err != nil {
		return err
	}
	if len(states) == 0 {
		fmt.Println("no limit data — run `ccdash setup-statusline` for live Claude limits")
		return nil
	}
	for _, s := range states {
		scope := s.Scope
		if scope == "" {
			scope = string(s.Kind)
		}
		fmt.Printf("%-7s %-14s %5.1f%%  %s (%s, %s old)\n",
			s.Tool, scope, s.Percent, resetIn(s), s.Provenance, s.Age.Round(60_000_000_000))
	}
	return nil
}
```

Add `resetIn` to the same file:

```go
func resetIn(s agg.LimitState) string {
	if s.ResetsAt == nil {
		return "no reset time"
	}
	d := time.Until(*s.ResetsAt)
	if d <= 0 {
		return "resetting"
	}
	return fmt.Sprintf("resets in %dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}
```

with `"time"` added to imports.

- [x] **Step 8: Build and run against the real corpus**

```bash
go build -o ccdash ./cmd/ccdash
./ccdash ingest
```
Expected: a summary line with a request count in the tens of thousands and a
dollar figure. Compare against the reference oracle:
```bash
python3 testdata/reference/snapshot.py
```
The Claude request count and cost must agree.

- [x] **Step 9: Commit**

```bash
git add internal/ingest cmd/ccdash
git commit -m "feat(ingest): orchestration with bounded fan-out and the ingest/limits commands"
```

---

## Task 8: `setup-statusline`

**Files:**
- Create: `cmd/ccdash/statusline.go`
- Test: `cmd/ccdash/statusline_test.go`

**Interfaces:**
- Consumes: `dataDir()` (Task 7)
- Produces: `setupStatusline() error`, `teeSnippet(capturePath string) string`, `alreadyInstalled(script, capturePath string) bool`

- [x] **Step 1: Write the failing test**

Create `cmd/ccdash/statusline_test.go`:

```go
package main

import "testing"

func TestTeeSnippetReferencesCapturePath(t *testing.T) {
	s := teeSnippet("/tmp/cap.jsonl")
	if !contains(s, "/tmp/cap.jsonl") {
		t.Errorf("snippet must reference the capture path:\n%s", s)
	}
	if !contains(s, "$input") {
		t.Errorf("snippet must tee the statusline payload variable:\n%s", s)
	}
}

func TestAlreadyInstalledDetectsPriorTee(t *testing.T) {
	script := "#!/bin/bash\ninput=$(cat)\n" + teeSnippet("/tmp/cap.jsonl")
	if !alreadyInstalled(script, "/tmp/cap.jsonl") {
		t.Error("should detect an existing tee and refuse to double-install")
	}
	if alreadyInstalled("#!/bin/bash\ninput=$(cat)\n", "/tmp/cap.jsonl") {
		t.Error("should not report installed on a clean script")
	}
}

func contains(hay, needle string) bool {
	return len(hay) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(hay); i++ {
			if hay[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/ccdash -v`
Expected: FAIL — `undefined: teeSnippet`

- [x] **Step 3: Write the implementation**

Create `cmd/ccdash/statusline.go`:

```go
package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const teeMarker = "# ccdash capture"

func teeSnippet(capturePath string) string {
	return fmt.Sprintf("\n%s\nprintf '%%s\\n' \"$input\" >> %s\n", teeMarker, capturePath)
}

func alreadyInstalled(script, capturePath string) bool {
	return strings.Contains(script, teeMarker) || strings.Contains(script, capturePath)
}

// setupStatusline appends the capture tee to the user's statusline script.
// It shows the exact change and requires confirmation; it never edits silently.
func setupStatusline() error {
	home, _ := os.UserHomeDir()
	script := filepath.Join(home, ".claude", "statusline-command.sh")
	capture := filepath.Join(dataDir(), "statusline.jsonl")

	b, err := os.ReadFile(script)
	if err != nil {
		return fmt.Errorf("no statusline script at %s — configure one in Claude Code first", script)
	}
	if alreadyInstalled(string(b), capture) {
		fmt.Println("capture already installed; nothing to do")
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(capture), 0o755); err != nil {
		return err
	}

	snippet := teeSnippet(capture)
	fmt.Printf("Will append to %s:\n%s\n", script, snippet)
	fmt.Printf("A backup will be written alongside it.\n")
	fmt.Print("Proceed? [y/N] ")
	ans, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	if strings.ToLower(strings.TrimSpace(ans)) != "y" {
		fmt.Println("aborted; to add it manually, append the snippet above")
		return nil
	}

	backup := fmt.Sprintf("%s.bak.%d", script, time.Now().Unix())
	if err := os.WriteFile(backup, b, 0o644); err != nil {
		return err
	}
	f, err := os.OpenFile(script, os.O_APPEND|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.WriteString(snippet); err != nil {
		return err
	}
	fmt.Printf("installed; backup at %s\ncapturing to %s\n", backup, capture)
	return nil
}
```

- [x] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/ccdash -v`
Expected: PASS — two tests

- [x] **Step 5: Commit**

```bash
git add cmd/ccdash/statusline.go cmd/ccdash/statusline_test.go
git commit -m "feat(cli): setup-statusline with confirmation and backup"
```

---

## Task 9: Aggregation queries

**Files:**
- Create: `internal/agg/agg.go`
- Test: `internal/agg/agg_test.go`

**Interfaces:**
- Consumes: `*sql.DB` from `store.DB()`, `*model.Pricing`
- Produces: `agg.Filter{From, To time.Time; Tool model.Tool; Project string}`, `agg.Totals(db, p, f) (Totals, error)` where `Totals{Requests int; Tokens, Input, Output, CacheRead, CacheWrite int64; Cost, MainCost, SubCost float64; From, To time.Time}`, `agg.ByDay(db, p, f) ([]DayBucket, error)` where `DayBucket{Day time.Time; Tokens int64; Cost float64}`, `agg.ByModel(db, p, f) ([]ModelBucket, error)` where `ModelBucket{Model string; Requests int; OutputTok int64; Cost float64}`, `agg.ByProject(db, p, f) ([]ProjectBucket, error)` where `ProjectBucket{Project string; Cost float64; Spark []float64}`, `agg.LatestLimits(db) ([]LimitState, error)` where `LimitState{model.LimitSample; Age time.Duration}`

- [x] **Step 1: Write the failing test**

Create `internal/agg/agg_test.go`:

```go
package agg

import (
	"testing"
	"time"

	"github.com/seanochang/ccdash/internal/model"
	"github.com/seanochang/ccdash/internal/store"
)

func seeded(t *testing.T) (*store.Store, *model.Pricing) {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/u.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	day := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	recs := []model.Record{
		{ID: "a", Tool: model.ToolClaude, TS: day, Model: "claude-opus-5",
			Project: "/p1", OutputTok: 1_000_000},
		{ID: "b", Tool: model.ToolClaude, TS: day.Add(time.Hour), Model: "claude-opus-5",
			Project: "/p2", Agent: "agent-x", OutputTok: 2_000_000},
		{ID: "c", Tool: model.ToolCodex, TS: day.AddDate(0, 0, 1), Model: "gpt-5",
			Project: "/p1", OutputTok: 1_000_000},
	}
	if _, err := st.UpsertRecords(recs); err != nil {
		t.Fatal(err)
	}
	return st, model.DefaultPricing()
}

func TestTotalsSplitsMainAndSubagent(t *testing.T) {
	st, p := seeded(t)
	tot, err := Totals(st.DB(), p, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if tot.Requests != 3 {
		t.Fatalf("requests = %d, want 3", tot.Requests)
	}
	// opus-5 output $25/M: a=25, b=50 (subagent); gpt-5 output $10/M: c=10
	if diff := tot.Cost - 85.0; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("cost = %v, want 85", tot.Cost)
	}
	if diff := tot.SubCost - 50.0; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("subagent cost = %v, want 50", tot.SubCost)
	}
}

func TestByDayGroupsByCalendarDay(t *testing.T) {
	st, p := seeded(t)
	days, err := ByDay(st.DB(), p, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 2 {
		t.Fatalf("got %d days, want 2", len(days))
	}
	if days[0].Cost != 75.0 {
		t.Errorf("day 1 cost = %v, want 75", days[0].Cost)
	}
}

func TestByModelAndProject(t *testing.T) {
	st, p := seeded(t)
	ms, err := ByModel(st.DB(), p, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 2 || ms[0].Model != "claude-opus-5" {
		t.Fatalf("models = %+v, want opus-5 first", ms)
	}
	ps, err := ByProject(st.DB(), p, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 2 {
		t.Fatalf("projects = %d, want 2", len(ps))
	}
}

func TestFilterByTool(t *testing.T) {
	st, p := seeded(t)
	tot, err := Totals(st.DB(), p, Filter{Tool: model.ToolCodex})
	if err != nil {
		t.Fatal(err)
	}
	if tot.Requests != 1 {
		t.Errorf("codex-only requests = %d, want 1", tot.Requests)
	}
}

func TestLatestLimitsReturnsNewestPerKind(t *testing.T) {
	st, _ := seeded(t)
	older := time.Now().Add(-2 * time.Hour)
	newer := time.Now().Add(-1 * time.Hour)
	for _, s := range []model.LimitSample{
		{Tool: model.ToolClaude, Kind: model.KindSession, Percent: 10,
			ObservedAt: older, Provenance: model.ProvLive},
		{Tool: model.ToolClaude, Kind: model.KindSession, Percent: 20,
			ObservedAt: newer, Provenance: model.ProvLive},
	} {
		if _, err := st.InsertLimitIfChanged(s); err != nil {
			t.Fatal(err)
		}
	}
	states, err := LatestLimits(st.DB())
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || states[0].Percent != 20 {
		t.Fatalf("states = %+v, want a single newest sample at 20%%", states)
	}
	if states[0].Age < 50*time.Minute {
		t.Errorf("age = %v, want ~1h", states[0].Age)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agg -v`
Expected: FAIL — `undefined: Totals`

- [x] **Step 3: Write the implementation**

Create `internal/agg/agg.go`. Cost is computed in Go from stored token columns rather than in SQL, so editing `pricing.toml` re-prices history with no re-ingest.

```go
package agg

import (
	"database/sql"
	"sort"
	"strings"
	"time"

	"github.com/seanochang/ccdash/internal/model"
)

type Filter struct {
	From, To time.Time
	Tool     model.Tool
	Project  string
}

func (f Filter) where() (string, []any) {
	var cond []string
	var args []any
	if !f.From.IsZero() {
		cond = append(cond, "ts >= ?")
		args = append(args, f.From.Unix())
	}
	if !f.To.IsZero() {
		cond = append(cond, "ts <= ?")
		args = append(args, f.To.Unix())
	}
	if f.Tool != "" {
		cond = append(cond, "tool = ?")
		args = append(args, string(f.Tool))
	}
	if f.Project != "" {
		cond = append(cond, "project = ?")
		args = append(args, f.Project)
	}
	if len(cond) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(cond, " AND "), args
}

const cols = `model,agent,project,ts,in_tok,out_tok,think_tok,cache_read,cache_w5m,cache_w1h`

type row struct {
	rec   model.Record
	agent string
}

func scanRows(db *sql.DB, f Filter) ([]row, error) {
	w, args := f.where()
	rs, err := db.Query(`SELECT `+cols+` FROM request`+w, args...)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	var out []row
	for rs.Next() {
		var r model.Record
		var ts int64
		var agent, project sql.NullString
		if err := rs.Scan(&r.Model, &agent, &project, &ts, &r.InputTok, &r.OutputTok,
			&r.ThinkingTok, &r.CacheReadTok, &r.CacheWrite5m, &r.CacheWrite1h); err != nil {
			return nil, err
		}
		r.TS = time.Unix(ts, 0).UTC()
		r.Agent = agent.String
		r.Project = project.String
		out = append(out, row{rec: r, agent: agent.String})
	}
	return out, rs.Err()
}

type Totals struct {
	Requests                              int
	Tokens, Input, Output                 int64
	CacheRead, CacheWrite                 int64
	Cost, MainCost, SubCost               float64
	From, To                              time.Time
}

func Totals(db *sql.DB, p *model.Pricing, f Filter) (Totals, error) {
	rows, err := scanRows(db, f)
	if err != nil {
		return Totals{}, err
	}
	var t Totals
	for _, r := range rows {
		rec := r.rec
		t.Requests++
		t.Input += rec.InputTok
		t.Output += rec.OutputTok
		t.CacheRead += rec.CacheReadTok
		t.CacheWrite += rec.CacheWrite5m + rec.CacheWrite1h
		t.Tokens += rec.InputTok + rec.OutputTok + rec.CacheReadTok +
			rec.CacheWrite5m + rec.CacheWrite1h
		if c, ok := p.Cost(rec); ok {
			t.Cost += c
			if r.agent == "" {
				t.MainCost += c
			} else {
				t.SubCost += c
			}
		}
		if t.From.IsZero() || rec.TS.Before(t.From) {
			t.From = rec.TS
		}
		if rec.TS.After(t.To) {
			t.To = rec.TS
		}
	}
	return t, nil
}

type DayBucket struct {
	Day    time.Time
	Tokens int64
	Cost   float64
}

func ByDay(db *sql.DB, p *model.Pricing, f Filter) ([]DayBucket, error) {
	rows, err := scanRows(db, f)
	if err != nil {
		return nil, err
	}
	m := map[time.Time]*DayBucket{}
	for _, r := range rows {
		d := r.rec.TS.Truncate(24 * time.Hour)
		b, ok := m[d]
		if !ok {
			b = &DayBucket{Day: d}
			m[d] = b
		}
		b.Tokens += r.rec.InputTok + r.rec.OutputTok + r.rec.CacheReadTok
		if c, ok := p.Cost(r.rec); ok {
			b.Cost += c
		}
	}
	out := make([]DayBucket, 0, len(m))
	for _, b := range m {
		out = append(out, *b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Day.Before(out[j].Day) })
	return out, nil
}

type ModelBucket struct {
	Model     string
	Requests  int
	OutputTok int64
	Cost      float64
}

func ByModel(db *sql.DB, p *model.Pricing, f Filter) ([]ModelBucket, error) {
	rows, err := scanRows(db, f)
	if err != nil {
		return nil, err
	}
	m := map[string]*ModelBucket{}
	for _, r := range rows {
		k := model.NormalizeModel(r.rec.Model)
		b, ok := m[k]
		if !ok {
			b = &ModelBucket{Model: k}
			m[k] = b
		}
		b.Requests++
		b.OutputTok += r.rec.OutputTok
		if c, ok := p.Cost(r.rec); ok {
			b.Cost += c
		}
	}
	out := make([]ModelBucket, 0, len(m))
	for _, b := range m {
		out = append(out, *b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Cost > out[j].Cost })
	return out, nil
}

type ProjectBucket struct {
	Project string
	Cost    float64
	Spark   []float64
}

const sparkPoints = 14

func ByProject(db *sql.DB, p *model.Pricing, f Filter) ([]ProjectBucket, error) {
	rows, err := scanRows(db, f)
	if err != nil {
		return nil, err
	}
	type acc struct {
		cost float64
		days map[time.Time]float64
	}
	m := map[string]*acc{}
	for _, r := range rows {
		a, ok := m[r.rec.Project]
		if !ok {
			a = &acc{days: map[time.Time]float64{}}
			m[r.rec.Project] = a
		}
		if c, ok := p.Cost(r.rec); ok {
			a.cost += c
			a.days[r.rec.TS.Truncate(24*time.Hour)] += c
		}
	}
	out := make([]ProjectBucket, 0, len(m))
	for name, a := range m {
		var days []time.Time
		for d := range a.days {
			days = append(days, d)
		}
		sort.Slice(days, func(i, j int) bool { return days[i].Before(days[j]) })
		if len(days) > sparkPoints {
			days = days[len(days)-sparkPoints:]
		}
		spark := make([]float64, 0, len(days))
		for _, d := range days {
			spark = append(spark, a.days[d])
		}
		out = append(out, ProjectBucket{Project: name, Cost: a.cost, Spark: spark})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Cost > out[j].Cost })
	return out, nil
}

type LimitState struct {
	model.LimitSample
	Age time.Duration
}

func LatestLimits(db *sql.DB) ([]LimitState, error) {
	rs, err := db.Query(`
      SELECT l.tool,l.kind,l.scope,l.percent,l.resets_at,l.is_active,
             l.observed_at,l.provenance
      FROM limit_sample l
      JOIN (SELECT tool,kind,scope,MAX(observed_at) AS m
            FROM limit_sample GROUP BY tool,kind,scope) x
        ON l.tool=x.tool AND l.kind=x.kind AND l.scope=x.scope
       AND l.observed_at=x.m
      ORDER BY l.tool, l.kind, l.scope`)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	now := time.Now()
	var out []LimitState
	for rs.Next() {
		var s model.LimitSample
		var tool, kind, prov string
		var reset sql.NullInt64
		var active int
		var observed int64
		if err := rs.Scan(&tool, &kind, &s.Scope, &s.Percent, &reset,
			&active, &observed, &prov); err != nil {
			return nil, err
		}
		s.Tool = model.Tool(tool)
		s.Kind = model.LimitKind(kind)
		s.Provenance = model.Provenance(prov)
		s.IsActive = active == 1
		s.ObservedAt = time.Unix(observed, 0)
		if reset.Valid {
			r := time.Unix(reset.Int64, 0)
			s.ResetsAt = &r
		}
		out = append(out, LimitState{LimitSample: s, Age: now.Sub(s.ObservedAt)})
	}
	return out, rs.Err()
}
```

- [x] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/agg -v -race`
Expected: PASS — five tests

- [x] **Step 5: Commit**

```bash
git add internal/agg
git commit -m "feat(agg): totals, day/model/project rollups, latest limits"
```

---

## Task 10: Render primitives

**Files:**
- Create: `internal/render/render.go`
- Test: `internal/render/render_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `render.Bar(frac float64, width int) string`, `render.Sparkline(vals []float64) string`, `render.Braille(series []float64, w, h int) string`

- [x] **Step 1: Write the failing test**

Create `internal/render/render_test.go`:

```go
package render

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestBarWidthAndClamping(t *testing.T) {
	if got := utf8.RuneCountInString(Bar(0.5, 10)); got != 10 {
		t.Errorf("Bar width = %d runes, want 10", got)
	}
	if got := Bar(1.0, 4); got != "████" {
		t.Errorf("Bar(1.0,4) = %q, want full", got)
	}
	if got := Bar(0, 4); strings.TrimSpace(got) != "" {
		t.Errorf("Bar(0,4) = %q, want blank", got)
	}
	if got := utf8.RuneCountInString(Bar(2.0, 6)); got != 6 {
		t.Errorf("over-range fraction must clamp to width, got %d", got)
	}
	if got := utf8.RuneCountInString(Bar(-1, 6)); got != 6 {
		t.Errorf("negative fraction must clamp, got %d", got)
	}
}

func TestBarUsesPartialBlocks(t *testing.T) {
	// 0.5 of 4 cells = 2 full cells exactly; 0.55 should add a partial
	if !strings.ContainsAny(Bar(0.55, 4), "▏▎▍▌▋▊▉") {
		t.Errorf("Bar(0.55,4) = %q, want a partial block for sub-cell precision", Bar(0.55, 4))
	}
}

func TestSparklineLengthAndRange(t *testing.T) {
	s := Sparkline([]float64{1, 5, 3, 9, 0})
	if utf8.RuneCountInString(s) != 5 {
		t.Errorf("Sparkline length = %d, want 5", utf8.RuneCountInString(s))
	}
	for _, r := range s {
		if !strings.ContainsRune("▁▂▃▄▅▆▇█", r) {
			t.Errorf("unexpected rune %q in sparkline", r)
		}
	}
	if Sparkline(nil) != "" {
		t.Error("empty input must yield empty string")
	}
	if got := Sparkline([]float64{3, 3, 3}); got != "▁▁▁" {
		t.Errorf("flat series = %q, want all-low (no divide-by-zero)", got)
	}
}

func TestBrailleDimensions(t *testing.T) {
	out := Braille([]float64{1, 2, 3, 4, 5}, 20, 4)
	lines := strings.Split(out, "\n")
	if len(lines) != 4 {
		t.Fatalf("got %d lines, want 4", len(lines))
	}
	for i, l := range lines {
		if utf8.RuneCountInString(l) != 20 {
			t.Errorf("line %d width = %d, want 20", i, utf8.RuneCountInString(l))
		}
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/render -v`
Expected: FAIL — `undefined: Bar`

- [x] **Step 3: Write the implementation**

Create `internal/render/render.go`:

```go
package render

import "strings"

var (
	partials = []rune(" ▏▎▍▌▋▊▉█")
	spark    = []rune("▁▂▃▄▅▆▇█")
	// brailleDots maps (dx,dy) to its bit in the U+2800 block.
	brailleDots = [8]rune{0x01, 0x02, 0x04, 0x40, 0x08, 0x10, 0x20, 0x80}
)

func clamp01(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}

// Bar renders frac of width cells using block elements, with sub-cell
// precision from partial blocks. Always returns exactly width runes.
func Bar(frac float64, width int) string {
	if width <= 0 {
		return ""
	}
	frac = clamp01(frac)
	exact := frac * float64(width)
	full := int(exact)
	rem := exact - float64(full)
	var b strings.Builder
	for i := 0; i < full && i < width; i++ {
		b.WriteRune('█')
	}
	n := full
	if n < width {
		idx := int(rem * 8)
		if idx < 0 {
			idx = 0
		}
		if idx > 8 {
			idx = 8
		}
		b.WriteRune(partials[idx])
		n++
	}
	for ; n < width; n++ {
		b.WriteRune(' ')
	}
	return b.String()
}

// Sparkline renders one cell per value, scaled to the series range.
func Sparkline(vals []float64) string {
	if len(vals) == 0 {
		return ""
	}
	min, max := vals[0], vals[0]
	for _, v := range vals {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	span := max - min
	var b strings.Builder
	for _, v := range vals {
		idx := 0
		if span > 0 {
			idx = int((v - min) / span * float64(len(spark)-1))
		}
		if idx < 0 {
			idx = 0
		}
		if idx >= len(spark) {
			idx = len(spark) - 1
		}
		b.WriteRune(spark[idx])
	}
	return b.String()
}

// Braille plots a series into a w x h cell grid at 2x4 subpixel resolution.
func Braille(series []float64, w, h int) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	px, py := w*2, h*4
	grid := make([][]bool, py)
	for i := range grid {
		grid[i] = make([]bool, px)
	}
	if len(series) > 0 {
		min, max := series[0], series[0]
		for _, v := range series {
			if v < min {
				min = v
			}
			if v > max {
				max = v
			}
		}
		span := max - min
		for x := 0; x < px; x++ {
			i := 0
			if len(series) > 1 {
				i = x * (len(series) - 1) / (px - 1)
			}
			norm := 0.5
			if span > 0 {
				norm = (series[i] - min) / span
			}
			y := int((1 - norm) * float64(py-1))
			if y >= 0 && y < py {
				grid[y][x] = true
			}
		}
	}
	var b strings.Builder
	for cy := 0; cy < h; cy++ {
		for cx := 0; cx < w; cx++ {
			var mask rune
			for dy := 0; dy < 4; dy++ {
				for dx := 0; dx < 2; dx++ {
					if grid[cy*4+dy][cx*2+dx] {
						mask |= brailleDots[dx*4+dy]
					}
				}
			}
			if mask == 0 {
				b.WriteRune(' ')
			} else {
				b.WriteRune(0x2800 + mask)
			}
		}
		if cy < h-1 {
			b.WriteRune('\n')
		}
	}
	return b.String()
}
```

- [x] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/render -v`
Expected: PASS — four tests

- [x] **Step 5: Commit**

```bash
git add internal/render
git commit -m "feat(render): bar, sparkline, and braille primitives"
```

---

## Task 11: TUI Overview screen

**Files:**
- Create: `internal/tui/tui.go`
- Modify: `cmd/ccdash/main.go` (dispatch the no-arg case to the TUI)
- Test: `internal/tui/tui_test.go`

**Interfaces:**
- Consumes: `agg.*` (Task 9), `render.*` (Task 10), `store.Store` (Task 3), `model.Pricing` (Task 2)
- Produces: `tui.New(st *store.Store, p *model.Pricing) Model`, `tui.Model` satisfying `tea.Model`, `tui.Run(st, p) error`

- [x] **Step 1: Write the failing test**

Create `internal/tui/tui_test.go`:

```go
package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/seanochang/ccdash/internal/agg"
	"github.com/seanochang/ccdash/internal/model"
)

func TestViewRendersEmptyState(t *testing.T) {
	m := Model{width: 80, height: 24, loaded: true}
	out := m.View()
	if !strings.Contains(out, "ccdash ingest") {
		t.Errorf("empty state must tell the user how to populate it:\n%s", out)
	}
}

func TestViewLabelsCostAsAPIRates(t *testing.T) {
	m := Model{
		width: 80, height: 24, loaded: true,
		totals: agg.Totals2{Requests: 5, Tokens: 1_000_000, Cost: 12.34},
	}
	out := m.View()
	if !strings.Contains(out, "at API rates") {
		t.Errorf("dollar figures must be labelled 'at API rates', never 'spent':\n%s", out)
	}
	if strings.Contains(strings.ToLower(out), "spent") {
		t.Errorf("must not claim money was spent:\n%s", out)
	}
}

func TestViewShowsStalenessForCachedLimits(t *testing.T) {
	m := Model{
		width: 80, height: 24, loaded: true,
		limits: []agg.LimitState{{
			LimitSample: model.LimitSample{
				Tool: model.ToolClaude, Kind: model.KindSession,
				Percent: 16, Provenance: model.ProvCached,
			},
			Age: 26 * time.Hour,
		}},
	}
	out := m.View()
	if !strings.Contains(out, "cached") || !strings.Contains(out, "26h") {
		t.Errorf("cached limits must show provenance and age:\n%s", out)
	}
}

func TestQuitKey(t *testing.T) {
	m := Model{}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Error("'q' must produce a quit command")
	}
}

func TestToolFilterKeys(t *testing.T) {
	m := Model{}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	if next.(Model).filter.Tool != model.ToolClaude {
		t.Errorf("'2' should filter to claude, got %q", next.(Model).filter.Tool)
	}
	next, _ = next.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	if next.(Model).filter.Tool != "" {
		t.Errorf("'1' should clear the tool filter, got %q", next.(Model).filter.Tool)
	}
}
```

> **Implementer note:** the test references `agg.Totals2` because `agg.Totals`
> is a function name and cannot also be a type. Rename the struct in
> `internal/agg/agg.go` from `Totals` to `TotalsResult` and the function stays
> `Totals`. Update Task 9's references and this test to `agg.TotalsResult`
> before running.

- [x] **Step 2: Apply the rename**

In `internal/agg/agg.go`, rename the struct `Totals` → `TotalsResult`; the
function signature becomes `func Totals(db *sql.DB, p *model.Pricing, f Filter) (TotalsResult, error)`.
Update `internal/agg/agg_test.go` and `cmd/ccdash/main.go` accordingly, then
replace `agg.Totals2` with `agg.TotalsResult` in the test above.

Run: `go build ./... && go test ./internal/agg -v`
Expected: PASS

- [x] **Step 3: Run the TUI test to verify it fails**

Run: `go test ./internal/tui -v`
Expected: FAIL — `undefined: Model`

- [x] **Step 4: Write the implementation**

```bash
go get github.com/charmbracelet/bubbletea@v1.3.10
go get github.com/charmbracelet/lipgloss@v1.1.0
```

Create `internal/tui/tui.go`:

```go
package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/seanochang/ccdash/internal/agg"
	"github.com/seanochang/ccdash/internal/model"
	"github.com/seanochang/ccdash/internal/render"
	"github.com/seanochang/ccdash/internal/store"
)

var (
	dim    = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	accent = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	warn   = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	head   = lipgloss.NewStyle().Bold(true)
)

type Model struct {
	st     *store.Store
	pr     *model.Pricing
	filter agg.Filter

	totals   agg.TotalsResult
	days     []agg.DayBucket
	models   []agg.ModelBucket
	projects []agg.ProjectBucket
	limits   []agg.LimitState
	unpriced int

	width, height int
	loaded        bool
	err           error
}

type loadedMsg struct {
	totals   agg.TotalsResult
	days     []agg.DayBucket
	models   []agg.ModelBucket
	projects []agg.ProjectBucket
	limits   []agg.LimitState
	unpriced int
	err      error
}

func New(st *store.Store, p *model.Pricing) Model {
	return Model{st: st, pr: p}
}

func (m Model) Init() tea.Cmd { return m.load() }

func (m Model) load() tea.Cmd {
	st, p, f := m.st, m.pr, m.filter
	return func() tea.Msg {
		if st == nil {
			return loadedMsg{}
		}
		var out loadedMsg
		var err error
		if out.totals, err = agg.Totals(st.DB(), p, f); err != nil {
			return loadedMsg{err: err}
		}
		if out.days, err = agg.ByDay(st.DB(), p, f); err != nil {
			return loadedMsg{err: err}
		}
		if out.models, err = agg.ByModel(st.DB(), p, f); err != nil {
			return loadedMsg{err: err}
		}
		if out.projects, err = agg.ByProject(st.DB(), p, f); err != nil {
			return loadedMsg{err: err}
		}
		if out.limits, err = agg.LatestLimits(st.DB()); err != nil {
			return loadedMsg{err: err}
		}
		up, err := st.Unpriced()
		if err != nil {
			return loadedMsg{err: err}
		}
		out.unpriced = len(up)
		return out
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case loadedMsg:
		m.totals, m.days, m.models = msg.totals, msg.days, msg.models
		m.projects, m.limits, m.unpriced = msg.projects, msg.limits, msg.unpriced
		m.err, m.loaded = msg.err, true
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "1":
			m.filter.Tool = ""
			return m, m.load()
		case "2":
			m.filter.Tool = model.ToolClaude
			return m, m.load()
		case "3":
			m.filter.Tool = model.ToolCodex
			return m, m.load()
		case "r":
			return m, m.load()
		}
	}
	return m, nil
}

func (m Model) View() string {
	if m.err != nil {
		return fmt.Sprintf("error: %v\n\npress q to quit\n", m.err)
	}
	if !m.loaded {
		return "loading…\n"
	}
	w := m.width
	if w < 40 {
		w = 80
	}
	var b strings.Builder

	b.WriteString(head.Render("ccdash"))
	if m.filter.Tool != "" {
		b.WriteString(dim.Render(fmt.Sprintf("  [%s only]", m.filter.Tool)))
	}
	b.WriteString("\n")

	if m.totals.Requests == 0 {
		b.WriteString(dim.Render("no data yet — run `ccdash ingest`\n"))
		b.WriteString(dim.Render("\n[q]uit\n"))
		return b.String()
	}

	b.WriteString(fmt.Sprintf("%s tokens   %s   %d requests\n",
		accent.Render(fmt.Sprintf("%.1fM", float64(m.totals.Tokens)/1e6)),
		accent.Render(fmt.Sprintf("$%.2f at API rates", m.totals.Cost)),
		m.totals.Requests))

	if len(m.days) > 1 {
		vals := make([]float64, 0, len(m.days))
		for _, d := range m.days {
			vals = append(vals, d.Cost)
		}
		b.WriteString(dim.Render("\ncost / day\n"))
		b.WriteString(accent.Render(render.Braille(vals, min(w-2, 72), 5)))
		b.WriteString("\n")
	}

	if len(m.models) > 0 {
		b.WriteString(dim.Render("\nby model\n"))
		for _, mb := range m.models {
			b.WriteString(fmt.Sprintf("  %-22s %6d req  $%9.2f\n",
				mb.Model, mb.Requests, mb.Cost))
		}
	}

	if len(m.projects) > 0 {
		b.WriteString(dim.Render("\nby project\n"))
		top := m.projects[0].Cost
		for i, p := range m.projects {
			if i >= 6 {
				break
			}
			frac := 0.0
			if top > 0 {
				frac = p.Cost / top
			}
			name := p.Project
			if len(name) > 28 {
				name = "…" + name[len(name)-27:]
			}
			b.WriteString(fmt.Sprintf("  %-28s %s $%8.2f  %s\n",
				name, accent.Render(render.Bar(frac, 16)), p.Cost,
				render.Sparkline(p.Spark)))
		}
	}

	if len(m.limits) > 0 {
		b.WriteString(dim.Render("\nlimits\n"))
		for _, l := range m.limits {
			label := l.Scope
			if label == "" {
				label = string(l.Kind)
			}
			prov := string(l.Provenance)
			if l.Provenance == model.ProvCached {
				prov = warn.Render(fmt.Sprintf("cached %dh", int(l.Age.Hours())))
			}
			marker := " "
			if l.IsActive {
				marker = "◀"
			}
			b.WriteString(fmt.Sprintf("  %-7s %-14s %s %5.1f%%  %-16s %s %s\n",
				l.Tool, label, accent.Render(render.Bar(l.Percent/100, 10)),
				l.Percent, resetIn(l.ResetsAt), marker, prov))
		}
	}

	b.WriteString(dim.Render(fmt.Sprintf(
		"\nmain %.1f%% · subagent %.1f%%", pct(m.totals.MainCost, m.totals.Cost),
		pct(m.totals.SubCost, m.totals.Cost))))
	if m.unpriced > 0 {
		b.WriteString(warn.Render(fmt.Sprintf("   ⚠ %d unpriced models", m.unpriced)))
	}
	b.WriteString(dim.Render("   [1]all [2]claude [3]codex [r]eload [q]uit\n"))
	return b.String()
}

func pct(part, whole float64) float64 {
	if whole == 0 {
		return 0
	}
	return part / whole * 100
}

func resetIn(t *time.Time) string {
	if t == nil {
		return ""
	}
	d := time.Until(*t)
	if d <= 0 {
		return "resetting"
	}
	if d > 24*time.Hour {
		return fmt.Sprintf("resets %dd %dh", int(d.Hours())/24, int(d.Hours())%24)
	}
	return fmt.Sprintf("resets %dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func Run(st *store.Store, p *model.Pricing) error {
	_, err := tea.NewProgram(New(st, p), tea.WithAltScreen()).Run()
	return err
}
```

- [x] **Step 5: Wire the no-arg case to the TUI**

In `cmd/ccdash/main.go`, split the `case "ingest", "":` branch so that `""`
ingests and then launches the TUI:

```go
	case "":
		if err := runIngest(false); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		st, p, err := open()
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		defer st.Close()
		if err := tui.Run(st, p); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "ingest":
		// …unchanged…
```

with `"github.com/seanochang/ccdash/internal/tui"` imported.

- [x] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/tui -v`
Expected: PASS — five tests

- [x] **Step 7: Full build and manual smoke test**

```bash
CGO_ENABLED=0 go build -o ccdash ./cmd/ccdash
go test ./... -race
./ccdash
```
Expected: tests all green; the TUI opens showing totals, a cost/day chart, model
and project breakdowns, and a limits panel. Press `q` to exit.

- [x] **Step 8: Commit**

```bash
git add internal/tui cmd/ccdash internal/agg
git commit -m "feat(tui): Overview screen with totals, charts, projects, and limits"
```

---

## Self-Review Notes

**Spec coverage.** Every §3 hazard maps to a task: dedupe (T4), Codex delta
conversion and restart flagging (T5), input normalization (T5), cache tiers
(T2/T4), dated model IDs (T1/T2), unpriced tracking (T2/T7), archive survival
(T3), limits from all three sources (T6), change-detection (T3), statusline
install (T8), Overview (T11). §9's differential test against
`testdata/reference/` runs manually in T7 step 8; automating it in CI is the
first Phase 2 chore, not a Phase 1 gate.

**Known rough edges the implementer should fix in place.** Task 7 step 5 and
Task 11 step 2 both call for a small cleanup rather than shipping the code
verbatim — the double offset computation and the `Totals` name collision. Both
are flagged inline with the exact change to make.

**Not covered by Phase 1.** Long-context pricing tiers, Codex `-codex` model
rates, workflow phase names from `meta.phases`, and quota history charting. All
are recorded in spec §13 as stated assumptions.
