package model

import (
	"math"
	"os"
	"strings"
	"testing"
)

func approx(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestCostCacheTiers(t *testing.T) {
	p := DefaultPricing()
	r := Record{Model: "claude-opus-5", InputTok: 1_000_000, OutputTok: 1_000_000,
		CacheReadTok: 1_000_000, CacheWrite5m: 1_000_000, CacheWrite1h: 1_000_000}
	got, ok := p.Cost(r)
	if !ok {
		t.Fatal("expected priced")
	}
	approx(t, got, 5.00+25.00+0.50+6.25+10.00)
}

func TestCostDatedModelID(t *testing.T) {
	got, ok := DefaultPricing().Cost(Record{Model: "claude-haiku-4-5-20251001", OutputTok: 1_000_000})
	if !ok {
		t.Fatal("dated model ID should price as its base ID")
	}
	approx(t, got, 5.00)
}

func TestCostUnpriced(t *testing.T) {
	for _, name := range []string{"gpt-5-codex", "<synthetic>", "totally-unknown"} {
		if _, ok := DefaultPricing().Cost(Record{Model: name, OutputTok: 1000}); ok {
			t.Errorf("%q must be unpriced, not guessed", name)
		}
	}
}

func TestCostOmittedRateMeansNoCharge(t *testing.T) {
	got, ok := DefaultPricing().Cost(Record{Model: "gpt-5.5", CacheWrite5m: 1_000_000})
	if !ok {
		t.Fatal("expected priced")
	}
	approx(t, got, 0)
}

func TestLoadPricingCreatesDefault(t *testing.T) {
	path := t.TempDir() + "/pricing.toml"
	p, err := LoadPricing(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.Cost(Record{Model: "claude-opus-5", OutputTok: 1_000_000}); !ok {
		t.Fatal("default pricing should price claude-opus-5")
	}
	got, ok := p.Cost(Record{Model: "gpt-5.6-sol", CacheWrite1h: 1_000_000})
	if !ok {
		t.Fatal("default file should price gpt-5.6-sol")
	}
	approx(t, got, 6.25)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(contents), "[models.\"gpt-5-codex\"]\ninput") {
		t.Fatal("unverified Codex rates must remain commented out")
	}
	if _, err := LoadPricing(path); err != nil {
		t.Fatalf("reload failed: %v", err)
	}
}

func TestParsePricingAllowsInlineComments(t *testing.T) {
	p, err := parseTOML("[models.\"custom\"]\ninput = 2.5 # USD/MTok\noutput = 4\n")
	if err != nil {
		t.Fatal(err)
	}
	got, ok := p.Cost(Record{Model: "custom", InputTok: 1_000_000, OutputTok: 1_000_000})
	if !ok {
		t.Fatal("custom model should be priced")
	}
	approx(t, got, 6.5)
}
