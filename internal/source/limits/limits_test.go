package limits

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/seanochang/llm-usage-dashboard/internal/model"
	"github.com/seanochang/llm-usage-dashboard/internal/source"
)

func ref(t *testing.T, name string) source.FileRef {
	t.Helper()
	path := filepath.Join("testdata", name)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return source.FileRef{Path: path, Size: info.Size(), Mtime: info.ModTime().Unix()}
}

func TestClaudeJSONYieldsThreeLimits(t *testing.T) {
	file := ref(t, "claude.json")
	result, err := NewClaudeJSON(file.Path).Parse(file, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Limits) != 3 {
		t.Fatalf("got %d limits, want 3", len(result.Limits))
	}
	byKind := make(map[model.LimitKind]model.LimitSample)
	for _, limit := range result.Limits {
		byKind[limit.Kind] = limit
		if limit.Provenance != model.ProvCached || limit.ObservedAt.UnixMilli() != 1786803772956 {
			t.Errorf("limit provenance/time = %+v", limit)
		}
	}
	if byKind[model.KindSession].Percent != 16 || byKind[model.KindWeeklyAll].Percent != 15 {
		t.Errorf("unscoped limits = %+v", byKind)
	}
	scoped := byKind[model.KindWeeklyScoped]
	if scoped.Percent != 19 || scoped.Scope != "Fable" || !scoped.IsActive {
		t.Errorf("scoped limit = %+v", scoped)
	}
}

func TestStatuslineYieldsLiveLimits(t *testing.T) {
	file := ref(t, "statusline.jsonl")
	result, err := NewStatusline(file.Path).Parse(file, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Limits) != 4 {
		t.Fatalf("got %d limits, want 4", len(result.Limits))
	}
	for _, limit := range result.Limits {
		if limit.Provenance != model.ProvLive || limit.Tool != model.ToolClaude {
			t.Errorf("limit = %+v", limit)
		}
	}
	if result.Limits[0].Kind != model.KindSession || result.Limits[0].Percent != 21 ||
		result.Limits[1].Kind != model.KindWeeklyAll || result.Limits[1].Percent != 15 {
		t.Errorf("first payload = %+v", result.Limits[:2])
	}
	if !result.Limits[2].ObservedAt.After(result.Limits[0].ObservedAt) {
		t.Error("statusline rows need stable increasing observation times")
	}
}

func TestClaudeJSONMissingFileIsNotAnError(t *testing.T) {
	result, err := NewClaudeJSON("/nonexistent/claude.json").Parse(
		source.FileRef{Path: "/nonexistent/claude.json"}, 0)
	if err != nil || len(result.Limits) != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestStatuslinePartialLineIsRetried(t *testing.T) {
	path := filepath.Join(t.TempDir(), "statusline.jsonl")
	if err := os.WriteFile(path, []byte(`{"rate_limits":{"five_hour":`), 0o644); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	result, err := NewStatusline(path).Parse(source.FileRef{Path: path, Size: info.Size(), Mtime: info.ModTime().Unix()}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if result.Offset != 0 {
		t.Fatalf("partial line advanced offset to %d", result.Offset)
	}
}
