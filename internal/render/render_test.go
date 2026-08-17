package render

import (
	"math"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestBarWidthAndClamping(t *testing.T) {
	for _, test := range []struct {
		fraction float64
		width    int
	}{
		{0.5, 10}, {1, 4}, {0, 4}, {2, 6}, {-1, 6}, {math.NaN(), 5},
	} {
		if got := utf8.RuneCountInString(Bar(test.fraction, test.width)); got != test.width {
			t.Errorf("Bar(%v,%d) width = %d", test.fraction, test.width, got)
		}
	}
	if got := Bar(1, 4); got != "████" {
		t.Errorf("full bar = %q", got)
	}
	if got := Bar(0, 4); strings.TrimSpace(got) != "" {
		t.Errorf("empty bar = %q", got)
	}
}

func TestBarUsesPartialBlocks(t *testing.T) {
	if got := Bar(0.55, 4); !strings.ContainsAny(got, "▏▎▍▌▋▊▉") {
		t.Errorf("bar = %q, want partial block", got)
	}
}

func TestSparklineLengthAndRange(t *testing.T) {
	line := Sparkline([]float64{1, 5, 3, 9, 0})
	if utf8.RuneCountInString(line) != 5 {
		t.Errorf("length = %d", utf8.RuneCountInString(line))
	}
	for _, r := range line {
		if !strings.ContainsRune("▁▂▃▄▅▆▇█", r) {
			t.Errorf("unexpected rune %q", r)
		}
	}
	if Sparkline(nil) != "" || Sparkline([]float64{3, 3, 3}) != "▁▁▁" {
		t.Error("empty or flat sparkline is incorrect")
	}
}

func TestBrailleDimensions(t *testing.T) {
	output := Braille([]float64{1, 2, 3, 4, 5}, 20, 4)
	lines := strings.Split(output, "\n")
	if len(lines) != 4 {
		t.Fatalf("height = %d", len(lines))
	}
	for i, line := range lines {
		if got := utf8.RuneCountInString(line); got != 20 {
			t.Errorf("line %d width = %d", i, got)
		}
	}
}

func TestBrailleDomainZeroBasedDoesNotFillPlot(t *testing.T) {
	// A flat non-zero series must sit near the top of a [0,max] domain,
	// not fill the whole plot the way a [min,max] domain would.
	flat := []float64{5, 5, 5, 5, 5}
	got := BrailleDomain(flat, 10, 4, 0, 5*1.05)
	lines := strings.Split(got, "\n")
	if len(lines) != 4 {
		t.Fatalf("got %d lines, want 4", len(lines))
	}
	if strings.TrimSpace(lines[3]) != "" {
		t.Errorf("bottom row should be empty for a flat series at max, got %q", lines[3])
	}
	if strings.TrimSpace(lines[0]) == "" {
		t.Errorf("top row should carry the series, got %q", lines[0])
	}
}

func TestBrailleDomainFlatZeroSeriesSitsOnFloor(t *testing.T) {
	got := BrailleDomain([]float64{0, 0, 0}, 8, 4, 0, 1)
	lines := strings.Split(got, "\n")
	if strings.TrimSpace(lines[0]) != "" {
		t.Errorf("top row should be empty for an all-zero series, got %q", lines[0])
	}
	if strings.TrimSpace(lines[3]) == "" {
		t.Error("bottom row should carry an all-zero series")
	}
}

func TestSparklineDomainSharedScaleMakesRowsComparable(t *testing.T) {
	// Two rows with identical values must render identically when they share
	// a domain, which per-row normalization does not guarantee.
	a := SparklineDomain([]float64{1, 2, 3}, 0, 10)
	b := SparklineDomain([]float64{1, 2, 3}, 0, 10)
	if a != b {
		t.Fatalf("identical series under one domain differ: %q vs %q", a, b)
	}
	// A row of small values under a large shared domain must not max out.
	small := SparklineDomain([]float64{1, 1, 1}, 0, 100)
	if strings.ContainsRune(small, '█') {
		t.Errorf("small values under a large domain should not render full blocks: %q", small)
	}
}

func TestBarTrackShowsUnfilledCells(t *testing.T) {
	got := BarTrack(0.5, 10, '·')
	if len([]rune(got)) != 10 {
		t.Fatalf("width = %d, want 10", len([]rune(got)))
	}
	if !strings.ContainsRune(got, '·') {
		t.Errorf("unfilled cells must render the track rune, got %q", got)
	}
	full := BarTrack(1.0, 6, '·')
	if strings.ContainsRune(full, '·') {
		t.Errorf("a full bar has no track cells, got %q", full)
	}
}

func TestBarStillPadsWithSpaces(t *testing.T) {
	// The existing Bar contract must not change.
	got := Bar(0.5, 10)
	if len([]rune(got)) != 10 {
		t.Fatalf("width = %d, want 10", len([]rune(got)))
	}
	if strings.ContainsRune(got, '·') {
		t.Errorf("Bar must keep space padding, got %q", got)
	}
}

func TestTruncatePathBreaksOnSeparator(t *testing.T) {
	cases := []struct {
		in    string
		width int
		want  string
	}{
		{"/home/user/dev/metrics/stocks/twn/data-cloud", 28, "…/stocks/twn/data-cloud"},
		{"/home/user/dev/metrics/crypto/data-cloud", 28, "…/metrics/crypto/data-cloud"},
		{"short/path", 28, "short/path"},
	}
	for _, c := range cases {
		got := TruncatePath(c.in, c.width)
		if got != c.want {
			t.Errorf("TruncatePath(%q,%d) = %q, want %q", c.in, c.width, got, c.want)
		}
		if len([]rune(got)) > c.width {
			t.Errorf("TruncatePath(%q,%d) = %q exceeds width", c.in, c.width, got)
		}
	}
}

func TestTruncatePathFallsBackWhenLastSegmentTooLong(t *testing.T) {
	got := TruncatePath("/a/averyveryverylongfinalsegmentname", 12)
	if len([]rune(got)) != 12 {
		t.Fatalf("got %q (len %d), want exactly 12 runes", got, len([]rune(got)))
	}
	if !strings.HasPrefix(got, "…") {
		t.Errorf("expected an ellipsis prefix, got %q", got)
	}
}
