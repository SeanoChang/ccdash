package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/seanochang/ccdash/internal/agg"
	"github.com/seanochang/ccdash/internal/ingest"
	"github.com/seanochang/ccdash/internal/model"
	"github.com/seanochang/ccdash/internal/render"
	"github.com/seanochang/ccdash/internal/store"
)

var (
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	accentStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	warningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	headingStyle = lipgloss.NewStyle().Bold(true)
)

type legacyModel struct {
	st         *store.Store
	pricing    *model.Pricing
	filter     agg.Filter
	rangeLabel string

	totals   agg.TotalsResult
	days     []agg.DayBucket
	models   []agg.ModelBucket
	projects []agg.ProjectBucket
	limits   []agg.LimitState
	unpriced int

	width, height int
	loaded        bool
	err           error
}

type loadedMsg struct {
	totals   agg.TotalsResult
	days     []agg.DayBucket
	models   []agg.ModelBucket
	projects []agg.ProjectBucket
	limits   []agg.LimitState
	unpriced int
	err      error
}

func newLegacy(st *store.Store, pricing *model.Pricing) legacyModel {
	return legacyModel{st: st, pricing: pricing, rangeLabel: "all"}
}

func (m legacyModel) Init() tea.Cmd { return m.load(false) }

func (m legacyModel) load(reingest bool) tea.Cmd {
	st, pricing, filter := m.st, m.pricing, m.filter
	return func() tea.Msg {
		if st == nil {
			return loadedMsg{}
		}
		if reingest {
			home, err := os.UserHomeDir()
			if err != nil {
				return loadedMsg{err: err}
			}
			if _, err := ingest.Run(st, ingest.DefaultSources(home), pricing, false); err != nil {
				return loadedMsg{err: err}
			}
		}
		var result loadedMsg
		var err error
		if result.totals, err = agg.Totals(st.DB(), pricing, filter); err != nil {
			return loadedMsg{err: err}
		}
		if result.days, err = agg.ByDay(st.DB(), pricing, filter); err != nil {
			return loadedMsg{err: err}
		}
		if result.models, err = agg.ByModel(st.DB(), pricing, filter); err != nil {
			return loadedMsg{err: err}
		}
		if result.projects, err = agg.ByProject(st.DB(), pricing, filter); err != nil {
			return loadedMsg{err: err}
		}
		if result.limits, err = agg.LatestLimits(st.DB()); err != nil {
			return loadedMsg{err: err}
		}
		unpriced, err := st.Unpriced()
		if err != nil {
			return loadedMsg{err: err}
		}
		result.unpriced = len(unpriced)
		return result
	}
}

func (m legacyModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = message.Width, message.Height
		return m, nil
	case loadedMsg:
		m.totals, m.days, m.models = message.totals, message.days, message.models
		m.projects, m.limits, m.unpriced = message.projects, message.limits, message.unpriced
		m.err, m.loaded = message.err, true
		return m, nil
	case tea.KeyMsg:
		switch message.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "1":
			m.filter.Tool = ""
			return m, m.load(false)
		case "2":
			m.filter.Tool = model.ToolClaude
			return m, m.load(false)
		case "3":
			m.filter.Tool = model.ToolCodex
			return m, m.load(false)
		case "d":
			m.setRange(24*time.Hour, "day")
			return m, m.load(false)
		case "w":
			m.setRange(7*24*time.Hour, "week")
			return m, m.load(false)
		case "m":
			m.setRange(30*24*time.Hour, "month")
			return m, m.load(false)
		case "r":
			return m, m.load(true)
		}
	}
	return m, nil
}

func (m *legacyModel) setRange(duration time.Duration, label string) {
	now := time.Now()
	m.filter.From = now.Add(-duration)
	m.filter.To = time.Time{}
	m.rangeLabel = label
}

func (m legacyModel) View() string {
	if m.err != nil {
		return fmt.Sprintf("error: %v\n\npress q to quit\n", m.err)
	}
	if !m.loaded {
		return "loading…\n"
	}
	width := m.width
	if width < 40 {
		width = 80
	}
	var out strings.Builder
	out.WriteString(headingStyle.Render("ccdash"))
	if m.filter.Tool != "" {
		out.WriteString(dimStyle.Render(fmt.Sprintf("  [%s only]", m.filter.Tool)))
	}
	if m.rangeLabel != "" && m.rangeLabel != "all" {
		out.WriteString(dimStyle.Render(fmt.Sprintf("  [%s]", m.rangeLabel)))
	}
	if !m.totals.From.IsZero() {
		out.WriteString(dimStyle.Render(fmt.Sprintf("  %s → %s",
			m.totals.From.Format("2006-01-02"), m.totals.To.Format("01-02"))))
	}
	out.WriteByte('\n')

	if m.totals.Requests == 0 {
		out.WriteString(dimStyle.Render("no usage data yet — run `ccdash ingest`\n"))
	} else {
		out.WriteString(fmt.Sprintf("%s tokens   %s   %d requests   cache read %.1f%%\n",
			accentStyle.Render(formatTokens(m.totals.Tokens)),
			accentStyle.Render(fmt.Sprintf("$%.2f at API rates", m.totals.Cost)),
			m.totals.Requests, inputShare(m.totals.CacheRead, m.totals.Input,
				m.totals.CacheWrite)))
		out.WriteString(m.usagePanels(width))
		out.WriteString(m.projectPanel())
	}
	out.WriteString(m.limitsPanel(width))

	if m.totals.Requests > 0 {
		out.WriteString(dimStyle.Render(fmt.Sprintf("\nmain %.1f%% · subagent %.1f%%",
			percentage(m.totals.MainCost, m.totals.Cost),
			percentage(m.totals.SubCost, m.totals.Cost))))
	}
	if m.unpriced > 0 {
		out.WriteString(warningStyle.Render(fmt.Sprintf("   ⚠ %d unpriced models", m.unpriced)))
	}
	out.WriteByte('\n')
	out.WriteString(dimStyle.Render("[1]all [2]claude [3]codex   [d]ay [w]eek [m]onth   [r]e-ingest [q]uit\n"))
	return out.String()
}

func (m legacyModel) usagePanels(width int) string {
	models := m.modelPanel()
	if width >= 100 && len(m.days) > 0 && models != "" {
		panelWidth := width/2 - 2
		chart := m.chartPanel(panelWidth)
		left := lipgloss.NewStyle().Width(panelWidth).Render(chart)
		right := lipgloss.NewStyle().Width(width/2 - 2).Render(models)
		return "\n" + lipgloss.JoinHorizontal(lipgloss.Top, left, right) + "\n"
	}
	return m.chartPanel(width) + models
}

func (m legacyModel) chartPanel(width int) string {
	if len(m.days) == 0 {
		return ""
	}
	values := make([]float64, 0, len(m.days))
	for _, day := range m.days {
		values = append(values, day.Cost)
	}
	chartWidth := minimum(width-2, 72)
	if chartWidth < 10 {
		chartWidth = 10
	}
	return dimStyle.Render("\ncost / day\n") + accentStyle.Render(render.Braille(values, chartWidth, 4)) + "\n"
}

func (m legacyModel) modelPanel() string {
	if len(m.models) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString(dimStyle.Render("\nby model\n"))
	for i, bucket := range m.models {
		if i >= 6 {
			break
		}
		out.WriteString(fmt.Sprintf("  %-22s %6d req  $%9.2f\n",
			bucket.Model, bucket.Requests, bucket.Cost))
	}
	return out.String()
}

func (m legacyModel) projectPanel() string {
	if len(m.projects) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString(dimStyle.Render("\nby project\n"))
	top := m.projects[0].Cost
	for i, project := range m.projects {
		if i >= 6 {
			break
		}
		fraction := 0.0
		if top > 0 {
			fraction = project.Cost / top
		}
		out.WriteString(fmt.Sprintf("  %-28s %s $%8.2f  %s\n",
			truncateLeft(project.Project, 28), accentStyle.Render(render.Bar(fraction, 16)),
			project.Cost, render.Sparkline(project.Spark)))
	}
	return out.String()
}

type expectedLimit struct {
	tool model.Tool
	kind model.LimitKind
}

func (m legacyModel) limitsPanel(width int) string {
	var out strings.Builder
	out.WriteString(dimStyle.Render("\nlimits\n"))
	states := make(map[string]agg.LimitState)
	for _, state := range m.limits {
		states[limitKey(state.Tool, state.Kind, state.Scope)] = state
	}
	expected := []expectedLimit{
		{model.ToolClaude, model.KindSession},
		{model.ToolClaude, model.KindWeeklyAll},
		{model.ToolCodex, model.KindCodex5h},
		{model.ToolCodex, model.KindCodexWeekly},
	}
	for _, item := range expected {
		key := limitKey(item.tool, item.kind, "")
		if state, ok := states[key]; ok {
			out.WriteString(renderLimit(state, width < 90))
			delete(states, key)
		} else {
			out.WriteString(fmt.Sprintf("  %-7s %-14s — no data\n", item.tool, limitLabel(item.kind, "")))
		}
	}
	for _, state := range m.limits {
		key := limitKey(state.Tool, state.Kind, state.Scope)
		if _, ok := states[key]; !ok {
			continue
		}
		out.WriteString(renderLimit(state, width < 90))
		delete(states, key)
	}
	return out.String()
}

func renderLimit(state agg.LimitState, compact bool) string {
	provenance := fmt.Sprintf("%s %s", state.Provenance, formatAge(state.Age))
	if state.Provenance == model.ProvCached || state.Age >= time.Hour {
		provenance = warningStyle.Render("⚠ " + provenance)
	}
	marker := ""
	if state.IsActive {
		marker = "◀ binding"
	}
	if compact {
		return fmt.Sprintf("  %-7s %-14s %s %5.1f%%  %-9s %s\n",
			state.Tool, limitLabel(state.Kind, state.Scope),
			accentStyle.Render(render.Bar(state.Percent/100, 8)), state.Percent,
			marker, provenance)
	}
	return fmt.Sprintf("  %-7s %-14s %s %5.1f%%  %-16s %-9s %s\n",
		state.Tool, limitLabel(state.Kind, state.Scope),
		accentStyle.Render(render.Bar(state.Percent/100, 10)), state.Percent,
		resetIn(state.ResetsAt), marker, provenance)
}

func limitKey(tool model.Tool, kind model.LimitKind, scope string) string {
	return string(tool) + "\x00" + string(kind) + "\x00" + scope
}

func limitLabel(kind model.LimitKind, scope string) string {
	if scope != "" {
		return scope
	}
	switch kind {
	case model.KindWeeklyAll:
		return "weekly"
	case model.KindCodex5h:
		return "5h"
	case model.KindCodexWeekly:
		return "weekly"
	default:
		return string(kind)
	}
}

func formatTokens(tokens int64) string {
	switch {
	case tokens >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", float64(tokens)/1e9)
	case tokens >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(tokens)/1e6)
	case tokens >= 1_000:
		return fmt.Sprintf("%.1fK", float64(tokens)/1e3)
	default:
		return fmt.Sprintf("%d", tokens)
	}
}

func inputShare(cacheRead, fresh, cacheWrite int64) float64 {
	return percentage(float64(cacheRead), float64(cacheRead+fresh+cacheWrite))
}

func percentage(part, whole float64) float64 {
	if whole == 0 {
		return 0
	}
	return part / whole * 100
}

func resetIn(value *time.Time) string {
	if value == nil {
		return "no reset time"
	}
	duration := time.Until(*value)
	if duration <= 0 {
		return "resetting"
	}
	if duration >= 24*time.Hour {
		return fmt.Sprintf("resets %dd %dh", int(duration.Hours())/24, int(duration.Hours())%24)
	}
	return fmt.Sprintf("resets %dh%02dm", int(duration.Hours()), int(duration.Minutes())%60)
}

func formatAge(age time.Duration) string {
	if age < time.Minute {
		return "<1m"
	}
	if age < time.Hour {
		return fmt.Sprintf("%dm", int(age.Minutes()))
	}
	return fmt.Sprintf("%dh", int(age.Hours()))
}

func truncateLeft(value string, width int) string {
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	if width <= 1 {
		return "…"
	}
	return "…" + string(runes[len(runes)-(width-1):])
}

func minimum(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func Run(st *store.Store, pricing *model.Pricing) error {
	_, err := tea.NewProgram(newLegacy(st, pricing), tea.WithAltScreen()).Run()
	return err
}
