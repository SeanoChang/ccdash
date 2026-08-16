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
