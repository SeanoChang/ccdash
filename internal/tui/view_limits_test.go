package tui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/SeanoChang/ccdash/internal/model"
	"github.com/SeanoChang/ccdash/internal/store"
)

// seedLimits gives both tools a live limit sample, plus one scoped weekly limit
// that is not in the expected set, so a tool filter has something of each shape
// to keep or drop.
func seedLimits(t *testing.T) *store.Store {
	t.Helper()
	s := seedStore(t)
	reset := time.Now().Add(3 * time.Hour)
	samples := []model.LimitSample{
		{Tool: model.ToolClaude, Kind: model.KindSession, Percent: 41,
			ResetsAt: &reset, IsActive: true, ObservedAt: time.Now(),
			Provenance: model.ProvLive},
		{Tool: model.ToolClaude, Kind: model.KindWeeklyAll, Percent: 12,
			ResetsAt: &reset, ObservedAt: time.Now(), Provenance: model.ProvLive},
		{Tool: model.ToolClaude, Kind: model.KindWeeklyScoped, Scope: "opus",
			Percent: 63, ResetsAt: &reset, ObservedAt: time.Now(),
			Provenance: model.ProvLive},
		{Tool: model.ToolCodex, Kind: model.KindCodex5h, Percent: 77,
			ResetsAt: &reset, ObservedAt: time.Now(), Provenance: model.ProvLive},
		{Tool: model.ToolCodex, Kind: model.KindCodexWeekly, Percent: 8,
			ResetsAt: &reset, ObservedAt: time.Now(), Provenance: model.ProvCached},
	}
	for _, sample := range samples {
		if _, err := s.InsertLimitIfChanged(sample); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

// rowTools lists the TOOL column of every returned row, in order.
func rowTools(rows []Row) []string {
	tools := make([]string, 0, len(rows))
	for _, row := range rows {
		tools = append(tools, row.Cells[0].Text)
	}
	return tools
}

func toolScope(tool model.Tool) Scope {
	scope := Scope{}
	scope.Tool = tool
	return scope
}

// TestLimitsRowsHonorTheToolFilter is the defect: the border title narrows to
// the filtered tool while the body kept listing every tool's limits.
func TestLimitsRowsHonorTheToolFilter(t *testing.T) {
	s := seedLimits(t)
	cases := []struct {
		name  string
		scope Scope
		want  int
		tool  string
	}{
		{name: "unfiltered", scope: Scope{}, want: 5},
		{name: "claude", scope: toolScope(model.ToolClaude), want: 3, tool: "claude"},
		{name: "codex", scope: toolScope(model.ToolCodex), want: 2, tool: "codex"},
	}
	for _, test := range cases {
		rows, err := LimitsView{}.Rows(s.DB(), model.DefaultPricing(), test.scope)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != test.want {
			t.Errorf("%s: got %d rows %v, want %d", test.name, len(rows),
				rowTools(rows), test.want)
		}
		if test.tool == "" {
			continue
		}
		for _, got := range rowTools(rows) {
			if got != test.tool {
				t.Errorf("%s: row for tool %q survived the %s filter",
					test.name, got, test.tool)
			}
		}
	}
}

// TestLimitsFilteredToAToolWithoutSamplesStillReadsNoData keeps the missing
// limit contract under a filter: the expected kinds of the filtered tool still
// appear, and still say "no data" rather than a reassuring 0%.
func TestLimitsFilteredToAToolWithoutSamplesStillReadsNoData(t *testing.T) {
	s := seedStore(t) // no limit samples at all
	rows, err := LimitsView{}.Rows(s.DB(), model.DefaultPricing(),
		toolScope(model.ToolCodex))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows %v, want codex's two expected kinds",
			len(rows), rowTools(rows))
	}
	for _, row := range rows {
		joined := ""
		for _, cell := range row.Cells {
			joined += cell.Text
		}
		if !strings.Contains(joined, "no data") {
			t.Errorf("a missing limit must read 'no data', got %q", joined)
		}
	}
}

// TestLimitsKeepProvenanceAndBindingUnderAFilter pins the markers the filter
// must not cost: the age and provenance, the ⚠ on a cached sample, and the
// "◀ binding" pointer at the limit that actually binds.
func TestLimitsKeepProvenanceAndBindingUnderAFilter(t *testing.T) {
	s := seedLimits(t)
	claude, err := LimitsView{}.Rows(s.DB(), model.DefaultPricing(),
		toolScope(model.ToolClaude))
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(rowSources(claude), " | ")
	if !strings.Contains(joined, "live <1m") {
		t.Errorf("provenance and age must survive the filter, got %q", joined)
	}
	if !strings.Contains(joined, "◀ binding") {
		t.Errorf("the binding marker must survive the filter, got %q", joined)
	}
	codex, err := LimitsView{}.Rows(s.DB(), model.DefaultPricing(),
		toolScope(model.ToolCodex))
	if err != nil {
		t.Fatal(err)
	}
	if joined := strings.Join(rowSources(codex), " | "); !strings.Contains(joined, "⚠") {
		t.Errorf("a cached sample must keep its ⚠ under a filter, got %q", joined)
	}
}

func rowSources(rows []Row) []string {
	sources := make([]string, 0, len(rows))
	for _, row := range rows {
		sources = append(sources, row.Cells[len(row.Cells)-1].Text)
	}
	return sources
}

// TestLimitsIgnoreTheDateRangeAndSayNothingElse: a limit is the newest sample
// per (tool, kind, scope), so a date range cannot narrow it. The view leaves the
// rows alone — and the title has to leave them alone too, reading "all" rather
// than claiming a range the body never applied.
func TestLimitsIgnoreTheDateRangeAndSayNothingElse(t *testing.T) {
	s := seedLimits(t)
	ranged := Scope{}
	ranged.From = time.Now().Add(-time.Hour)
	ranged.To = time.Now()
	rows, err := LimitsView{}.Rows(s.DB(), model.DefaultPricing(), ranged)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 5 {
		t.Errorf("a date range must not narrow limits: got %d rows %v, want 5",
			len(rows), rowTools(rows))
	}
	if got := scopeLabel(ranged); got != "all" {
		t.Errorf("scopeLabel for a range-only scope = %q, want all — the title "+
			"must not claim a narrowing the Limits body never applied", got)
	}
}

var limitsTitlePattern = regexp.MustCompile(`Limits\(([^)]*)\)\[(\d+)\]`)

// frameBodyRows counts the non-blank interior lines of the bordered body, less
// the table's own column-header line.
func frameBodyRows(t *testing.T, lines []string) int {
	t.Helper()
	count := 0
	for _, line := range lines {
		if !strings.HasPrefix(line, "│") {
			continue
		}
		interior := strings.TrimSuffix(strings.TrimPrefix(line, "│"), "│")
		if strings.TrimSpace(interior) != "" {
			count++
		}
	}
	if count == 0 {
		t.Fatalf("no body content in frame:\n%s", strings.Join(lines, "\n"))
	}
	return count - 1
}

// TestLimitsFrameTitleCountEqualsVisibleRows is the invariant the defect broke,
// asserted end to end on the rendered frame: whatever number the border title
// claims, that many limit rows are on screen, under every tool filter.
func TestLimitsFrameTitleCountEqualsVisibleRows(t *testing.T) {
	s := seedLimits(t)
	m := New(s, model.DefaultPricing(), "/tmp/usage.db", LimitsView{}, nil)
	m.width, m.height = 100, 24
	m.reloadCurrent()
	for _, test := range []struct {
		press, scope string
		want         int
	}{
		{press: "1", scope: "all", want: 5},
		{press: "2", scope: "claude", want: 3},
		{press: "3", scope: "codex", want: 2},
	} {
		next, _ := m.Update(key(test.press))
		m = next.(Model)
		lines := frameLines(t, m)
		frame := strings.Join(lines, "\n")
		match := limitsTitlePattern.FindStringSubmatch(frame)
		if match == nil {
			t.Fatalf("<%s>: no Limits(scope)[count] title in frame:\n%s",
				test.press, frame)
		}
		if match[1] != test.scope {
			t.Errorf("<%s>: title scope = %q, want %q", test.press, match[1], test.scope)
		}
		claimed, err := strconv.Atoi(match[2])
		if err != nil {
			t.Fatal(err)
		}
		if claimed != test.want {
			t.Errorf("<%s>: title claims %d rows, want %d", test.press, claimed, test.want)
		}
		if shown := frameBodyRows(t, lines); shown != claimed {
			t.Errorf("<%s>: title claims %d rows, body shows %d:\n%s",
				test.press, claimed, shown, frame)
		}
		if test.scope == "all" {
			continue
		}
		other := "codex"
		if test.scope == "codex" {
			other = "claude"
		}
		if strings.Contains(frame, fmt.Sprintf("%s   ", other)) {
			t.Errorf("<%s>: the body still lists %s rows:\n%s",
				test.press, other, frame)
		}
	}
}
