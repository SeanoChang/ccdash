package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseArgsAllowsGlobalDBBeforeOrAfterCommand(t *testing.T) {
	for _, args := range [][]string{
		{"--db", "/tmp/u.db", "ingest", "--full"},
		{"ingest", "--full", "--db=/tmp/u.db"},
	} {
		options, err := parseArgs(args)
		if err != nil {
			t.Fatal(err)
		}
		if options.command != "ingest" || options.dbPath != "/tmp/u.db" || !options.full {
			t.Errorf("options = %+v", options)
		}
	}
}

func TestParseArgsRejectsCommandSpecificFlagsElsewhere(t *testing.T) {
	if _, err := parseArgs([]string{"limits", "--json"}); err == nil {
		t.Fatal("--json must only apply to ingest")
	}
}

func TestParseArgsRejectsMissingDBValue(t *testing.T) {
	if _, err := parseArgs([]string{"--db", "--full"}); err == nil {
		t.Fatal("--db must not consume another option as its path")
	}
}

func TestJSONAloneSelectsHeadlessIngest(t *testing.T) {
	options, err := parseArgs([]string{"--json"})
	if err != nil || options.command != "ingest" || !options.json {
		t.Fatalf("options=%+v err=%v", options, err)
	}
}

func TestUsageDocumentsDBOverride(t *testing.T) {
	if !strings.Contains(usage(), "--db PATH") {
		t.Fatal("usage must document --db")
	}
}

func TestRunCLIHeadlessJSONWithEmptyHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	var stdout, stderr bytes.Buffer
	code := runCLI([]string{"--db", filepath.Join(home, "usage.db"), "ingest", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	var payload ingestJSON
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout.String())
	}
	if payload.All.Totals.Requests != 0 || payload.All.Models == nil {
		t.Fatalf("empty snapshot = %+v", payload.All)
	}
}
