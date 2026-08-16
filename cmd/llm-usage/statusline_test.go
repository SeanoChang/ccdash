package main

import (
	"strings"
	"testing"
)

func TestTeeSnippetReferencesCapturePathAndInput(t *testing.T) {
	snippet := teeSnippet("/tmp/cap.jsonl")
	if !strings.Contains(snippet, "/tmp/cap.jsonl") || !strings.Contains(snippet, "$input") {
		t.Errorf("snippet = %s", snippet)
	}
}

func TestTeeSnippetShellQuotesPath(t *testing.T) {
	if snippet := teeSnippet("/tmp/it's here/cap.jsonl"); !strings.Contains(snippet, `'"'"'`) {
		t.Errorf("snippet is not shell-quoted: %s", snippet)
	}
}

func TestAlreadyInstalledDetectsPriorTee(t *testing.T) {
	script := "#!/bin/sh\ninput=$(cat)\n" + teeSnippet("/tmp/cap.jsonl")
	if !alreadyInstalled(script, "/tmp/cap.jsonl") {
		t.Fatal("existing tee not detected")
	}
	if alreadyInstalled("#!/bin/sh\ninput=$(cat)\n", "/tmp/cap.jsonl") {
		t.Fatal("clean script reported installed")
	}
}

func TestStatuslineDiffShowsOnlyAddedLines(t *testing.T) {
	diff := statuslineDiff("/tmp/status.sh", "#!/bin/sh\n", teeSnippet("/tmp/cap.jsonl"))
	for _, want := range []string{"--- /tmp/status.sh", "+++ /tmp/status.sh", "+# llm-usage-dashboard capture", "+printf"} {
		if !strings.Contains(diff, want) {
			t.Errorf("diff missing %q:\n%s", want, diff)
		}
	}
}
