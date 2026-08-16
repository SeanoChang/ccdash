package codex

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/seanochang/llm-usage-dashboard/internal/model"
	"github.com/seanochang/llm-usage-dashboard/internal/source"
)

func fixtureRef(t *testing.T) source.FileRef {
	t.Helper()
	path := filepath.Join("testdata", "rollout.jsonl")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return source.FileRef{Path: path, Size: info.Size(), Mtime: info.ModTime().Unix()}
}

func parseFixture(t *testing.T) source.Result {
	t.Helper()
	result, err := New("").Parse(fixtureRef(t), 0)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestDuplicateTotalsProduceNoRecord(t *testing.T) {
	if got := len(parseFixture(t).Records); got != 3 {
		t.Fatalf("got %d records, want 3", got)
	}
}

func TestDeltasAreDifferencesNotCumulative(t *testing.T) {
	record := parseFixture(t).Records[1]
	if record.InputTok != 300 || record.CacheReadTok != 1200 ||
		record.OutputTok != 200 || record.ThinkingTok != 50 {
		t.Errorf("delta record = %+v", record)
	}
}

func TestAccumulatorRestartIsFlaggedNotClamped(t *testing.T) {
	record := parseFixture(t).Records[2]
	if !record.Anomaly || record.InputTok != 100 || record.CacheReadTok != 300 {
		t.Errorf("restart record = %+v", record)
	}
}

func TestZeroValuedRestartStillEmitsAnomaly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	contents := `{"type":"session_meta","payload":{"id":"s","cwd":"/p"}}
{"type":"turn_context","payload":{"model":"gpt-5"}}
{"type":"event_msg","timestamp":"2026-08-15T00:00:00Z","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":50,"output_tokens":10,"total_tokens":110}}}}
{"type":"event_msg","timestamp":"2026-08-15T00:01:00Z","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":0,"cached_input_tokens":0,"output_tokens":0,"total_tokens":258400}}}}
`
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	result, err := New("").Parse(source.FileRef{Path: path, Size: info.Size(), Mtime: info.ModTime().Unix()}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Records) != 2 || !result.Records[1].Anomaly {
		t.Fatalf("zero restart records = %+v", result.Records)
	}
}

func TestModelAndProjectFromContext(t *testing.T) {
	for _, record := range parseFixture(t).Records {
		if record.Model != "gpt-5.6-luna" || record.Project != "/home/u/projB" ||
			record.Session != "sess-1" {
			t.Errorf("identity = %+v", record)
		}
	}
}

func TestWindowMinutesMapToLimitKind(t *testing.T) {
	kinds := make(map[model.LimitKind]float64)
	for _, limit := range parseFixture(t).Limits {
		kinds[limit.Kind] = limit.Percent
	}
	if kinds[model.KindCodex5h] != 8 || kinds[model.KindCodexWeekly] != 34 {
		t.Errorf("limits = %v", kinds)
	}
}

func TestIncrementalParseReconstructsCumulativePrefix(t *testing.T) {
	ref := fixtureRef(t)
	data, err := os.ReadFile(ref.Path)
	if err != nil {
		t.Fatal(err)
	}
	last := bytes.LastIndex(data, []byte(`{"type":"event_msg"`))
	if last < 0 {
		t.Fatal("last event not found")
	}
	result, err := New("").Parse(ref, int64(last))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Records) != 1 {
		t.Fatalf("records = %d, want only appended event", len(result.Records))
	}
	record := result.Records[0]
	if record.ID != "sess-1:6" || record.Model != "gpt-5.6-luna" ||
		record.InputTok != 100 || !record.Anomaly {
		t.Fatalf("incremental record = %+v", record)
	}
}

func TestMissingRootDiscoversNoFiles(t *testing.T) {
	files, err := New(filepath.Join(t.TempDir(), "missing")).Discover()
	if err != nil || len(files) != 0 {
		t.Fatalf("files=%v err=%v", files, err)
	}
}
