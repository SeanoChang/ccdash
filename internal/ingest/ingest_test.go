package ingest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/seanochang/ccdash/internal/model"
	"github.com/seanochang/ccdash/internal/source"
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
	t.Cleanup(func() { _ = st.Close() })
	return st, dir
}

func TestRunIsIdempotent(t *testing.T) {
	st, dir := setup(t)
	sources := []source.Source{claude.New(dir)}
	first, err := Run(st, sources, model.DefaultPricing(), false)
	if err != nil {
		t.Fatal(err)
	}
	if first.Inserted != 2 {
		t.Fatalf("first run inserted %d, want 2", first.Inserted)
	}
	second, err := Run(st, sources, model.DefaultPricing(), false)
	if err != nil {
		t.Fatal(err)
	}
	if second.Inserted != 0 || second.Skipped != 1 {
		t.Fatalf("second run = %+v", second)
	}
}

func TestUnpricedModelIsStoredAndCountedOnce(t *testing.T) {
	st, dir := setup(t)
	sources := []source.Source{claude.New(dir)}
	if _, err := Run(st, sources, model.DefaultPricing(), false); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(st, sources, model.DefaultPricing(), true); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM request`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("stored rows = %d, want 2", count)
	}
	unpriced, err := st.Unpriced()
	if err != nil {
		t.Fatal(err)
	}
	if unpriced["mystery-model"] != 1 {
		t.Fatalf("unpriced = %v, want mystery-model:1", unpriced)
	}
}

func TestPricingEditClearsPreviouslyUnpricedModel(t *testing.T) {
	st, dir := setup(t)
	sources := []source.Source{claude.New(dir)}
	if _, err := Run(st, sources, model.DefaultPricing(), false); err != nil {
		t.Fatal(err)
	}
	pricingPath := filepath.Join(t.TempDir(), "pricing.toml")
	if err := os.WriteFile(pricingPath, []byte("[models.\"mystery-model\"]\noutput = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pricing, err := model.LoadPricing(pricingPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Run(st, sources, pricing, false); err != nil {
		t.Fatal(err)
	}
	unpriced, err := st.Unpriced()
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := unpriced["mystery-model"]; exists {
		t.Fatalf("priced model remained in unpriced table: %v", unpriced)
	}
}

func TestAppendUsesCursor(t *testing.T) {
	st, dir := setup(t)
	sources := []source.Source{claude.New(dir)}
	if _, err := Run(st, sources, model.DefaultPricing(), false); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "a.jsonl")
	fh, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := fh.WriteString(`{"type":"assistant","timestamp":"2026-08-15T00:00:02Z","requestId":"r3","message":{"model":"claude-opus-5","usage":{"output_tokens":4}}}` + "\n")
	closeErr := fh.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatalf("write=%v close=%v", writeErr, closeErr)
	}
	stats, err := Run(st, sources, model.DefaultPricing(), false)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Scanned != 1 || stats.Inserted != 1 {
		t.Fatalf("append stats = %+v", stats)
	}
}

func TestRunCountsCrossFileDuplicates(t *testing.T) {
	dir := t.TempDir()
	line := `{"type":"assistant","timestamp":"2026-08-15T00:00:00Z","requestId":"same","message":{"model":"claude-opus-5","usage":{"output_tokens":1}}}` + "\n"
	for _, name := range []string{"a.jsonl", "b.jsonl"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(line), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	st, err := store.Open(filepath.Join(dir, "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	stats, err := Run(st, []source.Source{claude.New(dir)}, model.DefaultPricing(), true)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Candidates != 2 || stats.Duplicates != 1 || stats.Inserted != 1 {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestDefaultSourcesTolerateMissingToolHomes(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	stats, err := Run(st, DefaultSources(t.TempDir()), model.DefaultPricing(), false)
	if err != nil || stats.Files != 0 {
		t.Fatalf("stats=%+v err=%v", stats, err)
	}
}
