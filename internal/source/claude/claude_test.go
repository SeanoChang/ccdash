package claude

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seanochang/ccdash/internal/source"
)

func parseFixture(t *testing.T, name string) source.Result {
	t.Helper()
	path := filepath.Join("testdata", name)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	result, err := New("").Parse(source.FileRef{
		Path: path, Size: info.Size(), Mtime: info.ModTime().Unix(),
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestDedupeByRequestID(t *testing.T) {
	result := parseFixture(t, "basic.jsonl")
	if got := len(result.Records); got != 2 {
		t.Fatalf("got %d records, want 2", got)
	}
	if result.Candidates != 4 || result.Duplicates != 2 {
		t.Fatalf("candidates=%d duplicates=%d", result.Candidates, result.Duplicates)
	}
}

func TestCacheTiersSplit(t *testing.T) {
	result := parseFixture(t, "basic.jsonl")
	byID := make(map[string]int)
	for i, record := range result.Records {
		byID[record.ID] = i
	}
	a := result.Records[byID["req_A"]]
	if a.CacheWrite1h != 100 || a.CacheWrite5m != 0 || a.CacheReadTok != 200 ||
		a.InputTok != 2 || a.ThinkingTok != 4 {
		t.Errorf("req_A tokens = %+v", a)
	}
	b := result.Records[byID["req_B"]]
	if b.CacheWrite5m != 50 || b.CacheWrite1h != 0 {
		t.Errorf("req_B cache tiers = %+v", b)
	}
}

func TestProjectSessionAndNormalizedModelCaptured(t *testing.T) {
	result := parseFixture(t, "basic.jsonl")
	for _, record := range result.Records {
		if record.Project != "/home/u/projA" || record.Session != "s1" {
			t.Errorf("record identity = %+v", record)
		}
	}
	if result.Records[1].Model != "claude-haiku-4-5" {
		t.Errorf("model = %q, want normalized dated ID", result.Records[1].Model)
	}
}

func TestSubagentAttributionFromPath(t *testing.T) {
	agent, workflow := attribution("/x/p/s/subagents/workflows/wf_83fc/agent-a9caf.jsonl")
	if agent != "agent-a9caf" || workflow != "wf_83fc" {
		t.Errorf("attribution = %q/%q", agent, workflow)
	}
	agent, workflow = attribution("/x/p/session.jsonl")
	if agent != "" || workflow != "" {
		t.Errorf("main-loop attribution = %q/%q", agent, workflow)
	}
}

func TestPartialFinalLineDoesNotAdvanceCursor(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "active.jsonl")
	complete := []byte(`{"type":"assistant","timestamp":"2026-08-15T00:00:00Z","requestId":"one","message":{"model":"claude-opus-5","usage":{"output_tokens":1}}}` + "\n")
	partial := []byte(`{"type":"assistant","timestamp":"2026-08-15T00:00:01Z","requestId":"two","message":{"model":"claude-opus-5","usage":`)
	if err := os.WriteFile(path, append(append([]byte{}, complete...), partial...), 0o644); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	first, err := New("").Parse(source.FileRef{Path: path, Size: info.Size(), Mtime: info.ModTime().Unix()}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if first.Offset != int64(len(complete)) || len(first.Records) != 1 {
		t.Fatalf("offset=%d records=%d, want %d/1", first.Offset, len(first.Records), len(complete))
	}
	finished := append(partial, []byte(`{"output_tokens":2}}}`+"\n")...)
	if err := os.WriteFile(path, append(append([]byte{}, complete...), finished...), 0o644); err != nil {
		t.Fatal(err)
	}
	info, _ = os.Stat(path)
	second, err := New("").Parse(source.FileRef{Path: path, Size: info.Size(), Mtime: info.ModTime().Unix()}, first.Offset)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Records) != 1 || second.Records[0].ID != "two" {
		t.Fatalf("completed retry = %+v", second.Records)
	}
}

func TestAgentMetadataDepth(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "subagents", "workflows", "wf_a")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "agent-abc.jsonl")
	line := `{"type":"assistant","timestamp":"2026-08-15T00:00:00Z","requestId":"r","message":{"model":"claude-opus-5","usage":{"output_tokens":1}}}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(strings.TrimSuffix(path, ".jsonl")+".meta.json", []byte(`{"spawnDepth":3}`), 0o644); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	result, err := New("").Parse(source.FileRef{Path: path, Size: info.Size(), Mtime: info.ModTime().Unix()}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if result.Records[0].Depth != 3 {
		t.Fatalf("depth = %d, want 3", result.Records[0].Depth)
	}
}
