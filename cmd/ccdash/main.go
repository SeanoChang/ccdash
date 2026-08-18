package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/SeanoChang/ccdash/internal/agg"
	"github.com/SeanoChang/ccdash/internal/ingest"
	"github.com/SeanoChang/ccdash/internal/model"
	"github.com/SeanoChang/ccdash/internal/store"
	"github.com/SeanoChang/ccdash/internal/tui"
)

const version = "0.1.0"

type cliOptions struct {
	command string
	dbPath  string
	full    bool
	json    bool
	help    bool
}

func dataDir() string {
	if value := os.Getenv("XDG_DATA_HOME"); value != "" {
		return filepath.Join(value, "ccdash")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "ccdash")
}

func configDir() string {
	if value := os.Getenv("XDG_CONFIG_HOME"); value != "" {
		return filepath.Join(value, "ccdash")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "ccdash")
}

func main() {
	os.Exit(runCLI(os.Args[1:], os.Stdout, os.Stderr))
}

func runCLI(args []string, stdout, stderr io.Writer) int {
	options, err := parseArgs(args)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		fmt.Fprint(stderr, usage())
		return 2
	}
	if options.help {
		fmt.Fprint(stdout, usage())
		return 0
	}
	switch options.command {
	case "version":
		fmt.Fprintln(stdout, version)
		return 0
	case "setup-statusline":
		if err := setupStatusline(); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		return 0
	case "ingest":
		if err := runIngest(options, stdout); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		return 0
	case "limits":
		if err := printLimits(options.dbPath, stdout); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		return 0
	case "":
		if err := runIngest(options, stdout); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		st, pricing, err := openApp(options.dbPath)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		defer st.Close()
		if err := tui.Run(st, pricing, databasePath(options.dbPath)); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", options.command)
		fmt.Fprint(stderr, usage())
		return 2
	}
}

func parseArgs(args []string) (cliOptions, error) {
	var options cliOptions
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-h" || arg == "--help":
			options.help = true
		case arg == "--db":
			i++
			if i >= len(args) || args[i] == "" || strings.HasPrefix(args[i], "-") {
				return options, errors.New("--db requires a path")
			}
			options.dbPath = args[i]
		case strings.HasPrefix(arg, "--db="):
			options.dbPath = strings.TrimPrefix(arg, "--db=")
			if options.dbPath == "" {
				return options, errors.New("--db requires a path")
			}
		case arg == "--full":
			options.full = true
		case arg == "--json":
			options.json = true
		case strings.HasPrefix(arg, "-"):
			return options, fmt.Errorf("unknown option %q", arg)
		default:
			if options.command != "" {
				return options, fmt.Errorf("unexpected argument %q", arg)
			}
			options.command = arg
		}
	}
	if (options.full || options.json) && options.command == "" {
		options.command = "ingest"
	}
	if (options.full || options.json) && options.command != "ingest" {
		return options, fmt.Errorf("--full and --json are only valid with ingest")
	}
	return options, nil
}

func usage() string {
	return `Usage:
  ccdash [--db PATH]                 ingest, then open Overview
  ccdash [--db PATH] ingest [--full] [--json]
  ccdash [--db PATH] limits
  ccdash setup-statusline
  ccdash version
`
}

func databasePath(override string) string {
	if override != "" {
		return override
	}
	return filepath.Join(dataDir(), "usage.db")
}

func openApp(dbOverride string) (*store.Store, *model.Pricing, error) {
	path := databasePath(dbOverride)
	st, err := store.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("database %s: %w (if it is corrupt, back it up and remove it with `rm -- %s`)",
			path, err, shellQuote(path))
	}
	pricing, err := model.LoadPricing(filepath.Join(configDir(), "pricing.toml"))
	if err != nil {
		st.Close()
		return nil, nil, err
	}
	return st, pricing, nil
}

type jsonTotals struct {
	Requests       int     `json:"requests"`
	Tokens         int64   `json:"tokens"`
	Input          int64   `json:"input_tokens"`
	Output         int64   `json:"output_tokens"`
	CacheRead      int64   `json:"cache_read_tokens"`
	CacheWrite     int64   `json:"cache_write_tokens"`
	MainTokens     int64   `json:"main_tokens"`
	SubagentTokens int64   `json:"subagent_tokens"`
	Cost           float64 `json:"cost_at_api_rates"`
	MainCost       float64 `json:"main_cost_at_api_rates"`
	SubagentCost   float64 `json:"subagent_cost_at_api_rates"`
}

type ingestJSON struct {
	Stats ingest.Stats            `json:"ingest"`
	All   jsonSnapshot            `json:"all"`
	Tools map[string]jsonSnapshot `json:"tools"`
}

type jsonSnapshot struct {
	Totals   jsonTotals        `json:"totals"`
	Models   []agg.ModelBucket `json:"models"`
	Unpriced map[string]int    `json:"unpriced"`
}

func runIngest(options cliOptions, output io.Writer) error {
	st, pricing, err := openApp(options.dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	stats, err := ingest.Run(st, ingest.DefaultSources(home), pricing, options.full)
	if err != nil {
		return err
	}
	totals, err := agg.Totals(st.DB(), pricing, agg.Filter{})
	if err != nil {
		return err
	}
	models, err := agg.ByModel(st.DB(), pricing, agg.Filter{})
	if err != nil {
		return err
	}
	if models == nil {
		models = []agg.ModelBucket{}
	}
	unpriced, err := st.Unpriced()
	if err != nil {
		return err
	}
	if options.json {
		claudeSnapshot, err := buildSnapshot(st, pricing, agg.Filter{Tool: model.ToolClaude})
		if err != nil {
			return err
		}
		codexSnapshot, err := buildSnapshot(st, pricing, agg.Filter{Tool: model.ToolCodex})
		if err != nil {
			return err
		}
		payload := ingestJSON{
			Stats: stats,
			All: jsonSnapshot{
				Totals: totalsForJSON(totals), Models: models, Unpriced: unpriced,
			},
			Tools: map[string]jsonSnapshot{
				string(model.ToolClaude): claudeSnapshot,
				string(model.ToolCodex):  codexSnapshot,
			},
		}
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(payload)
	}

	fmt.Fprintf(output, "files %d · skipped %d · records %d · inserted %d · limit samples %d",
		stats.Files, stats.Skipped, stats.Scanned, stats.Inserted, stats.Limits)
	if stats.Malformed > 0 || stats.Failed > 0 || stats.Anomalies > 0 {
		fmt.Fprintf(output, " · malformed %d · failed %d · anomalies %d",
			stats.Malformed, stats.Failed, stats.Anomalies)
	}
	fmt.Fprintln(output)
	fmt.Fprintf(output, "%d requests · %.1fM tokens · $%.2f at API rates\n",
		totals.Requests, float64(totals.Tokens)/1e6, totals.Cost)
	if len(unpriced) > 0 {
		names := make([]string, 0, len(unpriced))
		for name := range unpriced {
			names = append(names, name)
		}
		sort.Strings(names)
		fmt.Fprintf(output, "unpriced models (%d):", len(names))
		for _, name := range names {
			fmt.Fprintf(output, " %s×%d", name, unpriced[name])
		}
		fmt.Fprintln(output)
	}
	for _, path := range stats.FailedPaths {
		fmt.Fprintf(output, "warning: could not read %s\n", path)
	}
	return nil
}

func buildSnapshot(st *store.Store, pricing *model.Pricing, filter agg.Filter) (jsonSnapshot, error) {
	totals, err := agg.Totals(st.DB(), pricing, filter)
	if err != nil {
		return jsonSnapshot{}, err
	}
	models, err := agg.ByModel(st.DB(), pricing, filter)
	if err != nil {
		return jsonSnapshot{}, err
	}
	if models == nil {
		models = []agg.ModelBucket{}
	}
	unpriced := make(map[string]int)
	for _, bucket := range models {
		if !pricing.HasRate(bucket.Model) {
			unpriced[bucket.Model] += bucket.Requests
		}
	}
	return jsonSnapshot{Totals: totalsForJSON(totals), Models: models, Unpriced: unpriced}, nil
}

func totalsForJSON(totals agg.TotalsResult) jsonTotals {
	return jsonTotals{
		Requests: totals.Requests, Tokens: totals.Tokens,
		Input: totals.Input, Output: totals.Output,
		CacheRead: totals.CacheRead, CacheWrite: totals.CacheWrite,
		MainTokens: totals.MainTokens, SubagentTokens: totals.SubTokens,
		Cost: totals.Cost, MainCost: totals.MainCost, SubagentCost: totals.SubCost,
	}
}

func printLimits(dbOverride string, output io.Writer) error {
	st, _, err := openApp(dbOverride)
	if err != nil {
		return err
	}
	defer st.Close()
	states, err := agg.LatestLimits(st.DB())
	if err != nil {
		return err
	}
	if len(states) == 0 {
		fmt.Fprintln(output, "no limit data — run `ccdash setup-statusline` for live Claude limits")
		return nil
	}
	for _, state := range states {
		label := state.Scope
		if label == "" {
			label = string(state.Kind)
		}
		provenance := fmt.Sprintf("%s, %s old", state.Provenance, formatDuration(state.Age))
		if state.Provenance == model.ProvCached || state.Age >= time.Hour {
			provenance = "⚠ " + provenance
		}
		fmt.Fprintf(output, "%-7s %-14s %5.1f%%  %s (%s)\n",
			state.Tool, label, state.Percent, cliResetIn(state.ResetsAt),
			provenance)
	}
	return nil
}

func cliResetIn(value *time.Time) string {
	if value == nil {
		return "no reset time"
	}
	duration := time.Until(*value)
	if duration <= 0 {
		return "resetting"
	}
	return fmt.Sprintf("resets in %dh%02dm", int(duration.Hours()), int(duration.Minutes())%60)
}

func formatDuration(duration time.Duration) string {
	if duration < time.Minute {
		return "<1m"
	}
	if duration < time.Hour {
		return fmt.Sprintf("%dm", int(duration.Minutes()))
	}
	return fmt.Sprintf("%dh", int(duration.Hours()))
}
