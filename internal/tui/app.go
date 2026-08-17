package tui

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/seanochang/ccdash/internal/agg"
	"github.com/seanochang/ccdash/internal/model"
	"github.com/seanochang/ccdash/internal/store"
)

type stackEntry struct {
	view  View
	scope Scope
	table *Table
}

type Model struct {
	st      *store.Store
	pricing *model.Pricing
	dbPath  string

	stack []stackEntry
	scope Scope // global filter; drill-down narrowing is layered on top

	width, height int
	rangeLabel    string

	totals   agg.TotalsResult
	unpriced int

	lastRefresh time.Time
	refreshErr  error
	inFlight    bool

	mode  inputMode
	input string

	registry   map[string]func() View
	commandErr string
}

type inputMode int

const (
	modeNormal inputMode = iota
	modeCommand
	modeFilter
)

func New(st *store.Store, pricing *model.Pricing, dbPath string, root View,
	registry map[string]func() View) Model {
	m := Model{
		st: st, pricing: pricing, dbPath: dbPath,
		rangeLabel: "all", width: 80, height: 24,
		registry: registry,
	}
	m.stack = []stackEntry{{view: root, scope: m.scope, table: NewTable(root.Columns())}}
	return m
}

func (m Model) current() *stackEntry {
	if len(m.stack) == 0 {
		return nil
	}
	return &m.stack[len(m.stack)-1]
}

func (m Model) db() *sql.DB {
	if m.st == nil {
		return nil
	}
	return m.st.DB()
}

// reloadCurrent refetches the top view's rows into its table.
func (m *Model) reloadCurrent() {
	entry := m.current()
	if entry == nil {
		return
	}
	entry.table.SetSize(m.width, bodyHeight(m.height))
	rows, err := entry.view.Rows(m.db(), m.pricing, entry.scope)
	if err != nil {
		m.refreshErr = err
		return
	}
	m.refreshErr = nil
	entry.table.SetRows(rows)
}

func (m Model) breadcrumb() string {
	parts := make([]string, 0, len(m.stack))
	for _, entry := range m.stack {
		parts = append(parts, "<"+entry.view.Title()+">")
	}
	return strings.Join(parts, " ")
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = message.Width, message.Height
		for i := range m.stack {
			m.stack[i].table.SetSize(m.width, bodyHeight(m.height))
		}
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(message)
	}
	return m, nil
}

func (m Model) handleKey(message tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.mode != modeNormal {
		return m.handlePrompt(message)
	}
	entry := m.current()
	if entry == nil {
		return m, nil
	}
	switch message.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		entry.table.Move(1)
	case "k", "up":
		entry.table.Move(-1)
	case "ctrl+f":
		entry.table.Page(1)
	case "ctrl+b":
		entry.table.Page(-1)
	case "g":
		entry.table.Home()
	case "G":
		entry.table.End()
	case "s":
		entry.table.NextSort()
	case "S":
		entry.table.ReverseSort()
	case "enter":
		return m.drill()
	case "esc":
		return m.pop()
	case "1":
		return m.setTool("")
	case "2":
		return m.setTool(model.ToolClaude)
	case "3":
		return m.setTool(model.ToolCodex)
	case "d":
		return m.setRange(24*time.Hour, "day")
	case "w":
		return m.setRange(7*24*time.Hour, "week")
	case "m":
		return m.setRange(30*24*time.Hour, "month")
	case "a":
		return m.setRange(0, "all")
	case ":":
		m.mode = modeCommand
		m.input = ""
		m.commandErr = ""
	case "/":
		m.mode = modeFilter
		m.input = ""
	}
	return m, nil
}

// handlePrompt consumes every key while a prompt is open, so global bindings
// never fire on characters the user is typing.
func (m Model) handlePrompt(message tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch message.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		m.mode = modeNormal
		m.input = ""
		return m, nil
	case tea.KeyBackspace:
		if runes := []rune(m.input); len(runes) > 0 {
			m.input = string(runes[:len(runes)-1])
		}
		return m, nil
	case tea.KeyEnter:
		return m.submitPrompt()
	case tea.KeyRunes, tea.KeySpace:
		// Bubble Tea reports a lone space as KeySpace with Runes == [' '],
		// so the runes are the whole story; only a synthesised KeySpace
		// carrying no runes needs one appended.
		if len(message.Runes) > 0 {
			m.input += string(message.Runes)
		} else if message.Type == tea.KeySpace {
			m.input += " "
		}
		return m, nil
	}
	return m, nil
}

func (m Model) submitPrompt() (tea.Model, tea.Cmd) {
	input, mode := m.input, m.mode
	m.mode, m.input = modeNormal, ""
	switch mode {
	case modeFilter:
		m.current().table.SetFilter(input)
		return m, nil
	case modeCommand:
		if strings.EqualFold(strings.TrimSpace(input), "q") ||
			strings.EqualFold(strings.TrimSpace(input), "quit") {
			return m, tea.Quit
		}
		view, ok := resolveCommand(input, m.registry)
		if !ok {
			m.commandErr = "unknown command: " + input
			return m, nil
		}
		m.commandErr = ""
		// A command replaces the whole stack: it is a jump, not a drill.
		m.stack = []stackEntry{{
			view: view, scope: m.scope, table: NewTable(view.Columns()),
		}}
		m.reloadCurrent()
		return m, nil
	}
	return m, nil
}

func (m Model) drill() (tea.Model, tea.Cmd) {
	entry := m.current()
	row, ok := entry.table.Selected()
	if !ok {
		return m, nil
	}
	next, scope, ok := entry.view.Drill(row, entry.scope)
	if !ok {
		return m, nil
	}
	m.stack = append(m.stack, stackEntry{
		view: next, scope: scope, table: NewTable(next.Columns()),
	})
	m.reloadCurrent()
	return m, nil
}

// pop returns to the parent view. At the root it does nothing, so a reflexive
// esc can never drop the user out of the application.
func (m Model) pop() (tea.Model, tea.Cmd) {
	if len(m.stack) <= 1 {
		return m, nil
	}
	m.stack = m.stack[:len(m.stack)-1]
	m.reloadCurrent()
	return m, nil
}

// setTool and setRange change the global scope, which is then re-applied to
// every level of the stack so a drilled view stays consistent with the header.
func (m Model) setTool(tool model.Tool) (tea.Model, tea.Cmd) {
	m.scope.Tool = tool
	m.applyScope()
	return m, nil
}

func (m Model) setRange(window time.Duration, label string) (tea.Model, tea.Cmd) {
	m.rangeLabel = label
	if window == 0 {
		m.scope.From = time.Time{}
	} else {
		m.scope.From = time.Now().Add(-window)
	}
	m.scope.To = time.Time{}
	m.applyScope()
	return m, nil
}

func (m *Model) applyScope() {
	for i := range m.stack {
		m.stack[i].scope.From = m.scope.From
		m.stack[i].scope.To = m.scope.To
		m.stack[i].scope.Tool = m.scope.Tool
	}
	m.reloadCurrent()
}

func (m Model) View() string {
	entry := m.current()
	if entry == nil {
		return strings.Repeat(" ", m.width)
	}
	info := headerInfo{
		DBPath:   m.dbPath,
		Range:    m.rangeText(),
		Tokens:   formatTokens(m.totals.Tokens),
		Cost:     fmt.Sprintf("$%.2f at API rates", m.totals.Cost),
		Requests: fmt.Sprintf("%d", m.totals.Requests),
		Unpriced: fmt.Sprintf("%d", m.unpriced),
	}
	return frame(headerBlock(info, m.width), entry.table.Render(),
		m.footer(), m.width, m.height)
}

func (m Model) rangeText() string {
	text := m.rangeLabel
	if !m.totals.From.IsZero() {
		text += fmt.Sprintf("  %s → %s",
			m.totals.From.Format("2006-01-02"), m.totals.To.Format("2006-01-02"))
	}
	return text
}

func (m Model) footer() string {
	if m.mode == modeCommand {
		return padLine(stylePrompt.Render(" :"+m.input+"█"), m.width)
	}
	if m.mode == modeFilter {
		return padLine(stylePrompt.Render(" /"+m.input+"█"), m.width)
	}
	left := m.breadcrumb()
	right := "[enter] drill  [s]ort  [/]filter  [:]cmd  [?]help"
	if m.commandErr != "" {
		right = styleWarning.Render(m.commandErr)
	}
	if m.refreshErr != nil {
		right = styleDanger.Render("refresh failed: " + m.refreshErr.Error())
	}
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 1 {
		return padLine(" "+left, m.width)
	}
	return " " + left + strings.Repeat(" ", gap) + right + " "
}
