package model

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const perMillion = 1_000_000.0

// Rate contains absolute USD-per-million-token prices. A zero field means
// that the component has no published charge, not that the model is unknown.
type Rate struct {
	Input        float64
	CachedInput  float64
	CacheWrite5m float64
	CacheWrite1h float64
	Output       float64
}

type Pricing struct {
	rates map[string]Rate
}

func anth(input, output float64) Rate {
	return Rate{
		Input:        input,
		CachedInput:  input * 0.10,
		CacheWrite5m: input * 1.25,
		CacheWrite1h: input * 2.00,
		Output:       output,
	}
}

func oai(input, cached, write, output float64) Rate {
	return Rate{
		Input:        input,
		CachedInput:  cached,
		CacheWrite5m: write,
		CacheWrite1h: write,
		Output:       output,
	}
}

func DefaultPricing() *Pricing {
	return &Pricing{rates: map[string]Rate{
		"claude-fable-5":    anth(10.00, 50.00),
		"claude-mythos-5":   anth(10.00, 50.00),
		"claude-opus-5":     anth(5.00, 25.00),
		"claude-opus-4-8":   anth(5.00, 25.00),
		"claude-opus-4-7":   anth(5.00, 25.00),
		"claude-opus-4-6":   anth(5.00, 25.00),
		"claude-sonnet-5":   anth(3.00, 15.00),
		"claude-sonnet-4-6": anth(3.00, 15.00),
		"claude-haiku-4-5":  anth(1.00, 5.00),

		"gpt-5.6-sol":   oai(5.00, 0.50, 6.25, 30.00),
		"gpt-5.6-terra": oai(2.00, 0.20, 2.50, 12.00),
		"gpt-5.6-luna":  oai(0.20, 0.02, 0.25, 1.20),
		"gpt-5.5":       oai(5.00, 0.50, 0, 30.00),
		"gpt-5.4":       oai(2.50, 0.25, 0, 15.00),
		"gpt-5.2":       oai(1.75, 0.175, 0, 14.00),
		"gpt-5.1":       oai(1.25, 0.125, 0, 10.00),
		"gpt-5":         oai(1.25, 0.125, 0, 10.00),
		"gpt-5-mini":    oai(0.25, 0.025, 0, 2.00),
		"gpt-5-nano":    oai(0.05, 0.005, 0, 0.40),
	}}
}

func (p *Pricing) HasRate(modelID string) bool {
	if p == nil {
		return false
	}
	_, ok := p.rates[NormalizeModel(modelID)]
	return ok
}

// Cost returns a record's equivalent cost at API list rates. The bool is false
// when the model has no configured rate; the record itself must still be kept.
func (p *Pricing) Cost(r Record) (float64, bool) {
	if p == nil {
		return 0, false
	}
	rate, ok := p.rates[NormalizeModel(r.Model)]
	if !ok {
		return 0, false
	}
	cost := float64(r.InputTok)/perMillion*rate.Input +
		float64(r.OutputTok)/perMillion*rate.Output +
		float64(r.CacheReadTok)/perMillion*rate.CachedInput +
		float64(r.CacheWrite5m)/perMillion*rate.CacheWrite5m +
		float64(r.CacheWrite1h)/perMillion*rate.CacheWrite1h
	return cost, true
}

// LoadPricing reads path and writes the editable default table on first use.
func LoadPricing(path string) (*Pricing, error) {
	b, err := os.ReadFile(path)
	if err == nil {
		return parseTOML(string(b))
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read pricing %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create pricing directory: %w", err)
	}
	contents := defaultTOML()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		return nil, fmt.Errorf("write default pricing %s: %w", path, err)
	}
	return parseTOML(contents)
}

// parseTOML handles the intentionally small TOML dialect emitted below.
func parseTOML(text string) (*Pricing, error) {
	p := &Pricing{rates: make(map[string]Rate)}
	current := ""
	for n, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[models.") && strings.HasSuffix(line, "]") {
			name := strings.TrimSuffix(strings.TrimPrefix(line, "[models."), "]")
			name = strings.Trim(name, `"`)
			if name == "" {
				return nil, fmt.Errorf("pricing line %d: empty model name", n+1)
			}
			current = name
			if _, ok := p.rates[current]; !ok {
				p.rates[current] = Rate{}
			}
			continue
		}
		if current == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if before, _, found := strings.Cut(value, "#"); found {
			value = before
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			return nil, fmt.Errorf("pricing line %d: %w", n+1, err)
		}
		r := p.rates[current]
		switch strings.TrimSpace(key) {
		case "input":
			r.Input = f
		case "cached_input":
			r.CachedInput = f
		case "cache_write":
			r.CacheWrite5m, r.CacheWrite1h = f, f
		case "cache_write_5m":
			r.CacheWrite5m = f
		case "cache_write_1h":
			r.CacheWrite1h = f
		case "output":
			r.Output = f
		default:
			continue
		}
		p.rates[current] = r
	}
	return p, nil
}

func defaultTOML() string {
	var b strings.Builder
	b.WriteString("# llm-usage-dashboard pricing, absolute USD per million tokens.\n")
	b.WriteString("# Anthropic source: claude-api table cached 2026-06-24.\n")
	b.WriteString("# OpenAI source: developers.openai.com/api/docs/pricing, retrieved 2026-08-16.\n")
	b.WriteString("# An omitted component has no charge. Long-context rates are not auto-applied.\n\n")
	defaults := DefaultPricing()
	models := []string{
		"claude-fable-5", "claude-mythos-5", "claude-opus-5", "claude-opus-4-8",
		"claude-opus-4-7", "claude-opus-4-6", "claude-sonnet-5",
		"claude-sonnet-4-6", "claude-haiku-4-5", "gpt-5.6-sol",
		"gpt-5.6-terra", "gpt-5.6-luna", "gpt-5.5", "gpt-5.4", "gpt-5.2",
		"gpt-5.1", "gpt-5", "gpt-5-mini", "gpt-5-nano",
	}
	for _, name := range models {
		r := defaults.rates[name]
		fmt.Fprintf(&b, "[models.%q]\n", name)
		fmt.Fprintf(&b, "input = %g\n", r.Input)
		fmt.Fprintf(&b, "cached_input = %g\n", r.CachedInput)
		if r.CacheWrite5m != 0 && r.CacheWrite5m == r.CacheWrite1h {
			fmt.Fprintf(&b, "cache_write = %g\n", r.CacheWrite5m)
		} else if r.CacheWrite5m != 0 {
			fmt.Fprintf(&b, "cache_write_5m = %g\n", r.CacheWrite5m)
		}
		if r.CacheWrite1h != 0 && r.CacheWrite1h != r.CacheWrite5m {
			fmt.Fprintf(&b, "cache_write_1h = %g\n", r.CacheWrite1h)
		}
		fmt.Fprintf(&b, "output = %g\n\n", r.Output)
	}
	b.WriteString("# These Codex variants have no verified published rates. Uncomment and fill\n")
	b.WriteString("# them in to price them; their tokens are still archived while unpriced.\n")
	for _, name := range []string{
		"gpt-5-codex", "gpt-5.3-codex", "gpt-5.1-codex-max", "gpt-5.1-codex",
		"gpt-5.2-codex", "gpt-5.1-codex-mini", "codex-auto-review",
	} {
		fmt.Fprintf(&b, "# [models.%q]\n# input = 0.0\n# cached_input = 0.0\n# output = 0.0\n\n", name)
	}
	return b.String()
}
