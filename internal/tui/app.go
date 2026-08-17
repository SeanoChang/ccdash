package tui

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/seanochang/ccdash/internal/agg"
	"github.com/seanochang/ccdash/internal/ingest"
	"github.com/seanochang/ccdash/internal/model"
	"github.com/seanochang/ccdash/internal/store"
)

type stackEntry struct {
	view  View
	scope Scope
	table *Table
	// pages and more are only meaningful for a view implementing Paginator:
	// how many pages deep the user has scrolled, and whether the last fetch
	// came back full and so may have a successor.
	pages int
	more  bool
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

	// showHelp replaces the body with the keybinding overlay. Any key dismisses
	// it, so it is never sticky.
	showHelp bool

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
	m.stack = []stackEntry{newEntry(root, m.scope)}
	return m
}

// newEntry builds a stack level. pages starts at 1 so a paginated view opens
// on its first page rather than on nothing.
func newEntry(view View, scope Scope) stackEntry {
	return stackEntry{view: view, scope: scope, table: NewTable(view.Columns()), pages: 1}
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

// fetchRows loads a view's rows. A Paginator is asked for the entire prefix
// the user has scrolled into — pages × PageSize rows from offset zero — so a
// refresh keeps that depth instead of snapping back to the first page. more
// reports whether the fetch came back full and so may have a successor.
func fetchRows(view View, db *sql.DB, pricing *model.Pricing, scope Scope,
	pages int) ([]Row, bool, error) {
	paginator, ok := view.(Paginator)
	if !ok {
		rows, err := view.Rows(db, pricing, scope)
		return rows, false, err
	}
	if pages < 1 {
		pages = 1
	}
	size := paginator.PageSize()
	if size < 1 {
		size = 1
	}
	return paginator.Page(db, pricing, scope, 0, pages*size)
}

// reloadCurrent refetches the top view's rows into its table.
func (m *Model) reloadCurrent() {
	entry := m.current()
	if entry == nil {
		return
	}
	entry.table.SetSize(bodyWidth(m.width), bodyHeight(m.height))
	rows, more, err := fetchRows(entry.view, m.db(), m.pricing, entry.scope, entry.pages)
	if err != nil {
		m.refreshErr = err
		return
	}
	m.refreshErr = nil
	entry.more = more
	entry.table.SetRows(rows)
}

// loadMore extends a paginated view by one page once the selection reaches the
// last loaded row. A view that is not a Paginator, or whose last fetch came
// back short, is left alone.
func (m *Model) loadMore() {
	entry := m.current()
	if entry == nil || !entry.more || !entry.table.AtBottom() {
		return
	}
	if _, ok := entry.view.(Paginator); !ok {
		return
	}
	entry.pages++
	m.reloadCurrent()
}

func (m Model) breadcrumb() string {
	parts := make([]string, 0, len(m.stack))
	for _, entry := range m.stack {
		parts = append(parts, "<"+entry.view.Title()+">")
	}
	return strings.Join(parts, " ")
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.scheduleTick(), m.refresh(false))
}

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = message.Width, message.Height
		for i := range m.stack {
			m.stack[i].table.SetSize(bodyWidth(m.width), bodyHeight(m.height))
		}
		return m, nil
	case tickMsg:
		// Single-flight: a tick arriving mid-refresh is dropped outright, not
		// queued and not rescheduled — the running refresh reschedules the
		// ticker when it lands, so a duplicated chain of ticks dies here
		// instead of compounding.
		if m.inFlight {
			return m, nil
		}
		// Prompts pause the work but not the ticker, so it resumes on close.
		if m.mode != modeNormal {
			return m, m.scheduleTick()
		}
		m.inFlight = true
		return m, m.refresh(true)
	case refreshedMsg:
		m.inFlight = false
		m.lastRefresh = message.at
		m.refreshErr = message.err
		if message.err == nil {
			m.totals = message.totals
			m.unpriced = message.unpriced
			entry := m.current()
			entry.more = message.more
			entry.table.SetRows(message.rows)
		}
		return m, m.scheduleTick()
	case tea.KeyMsg:
		return m.handleKey(message)
	}
	return m, nil
}

func (m Model) handleKey(message tea.KeyMsg) (tea.Model, tea.Cmd) {
	// The help overlay is dismissed by any key, and that key is swallowed: the
	// keystroke that closes the overlay must not also act on the table beneath
	// it. ctrl+c still quits, per spec §5.5's "any" context.
	if m.showHelp {
		if message.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
		m.showHelp = false
		return m, nil
	}
	if m.mode != modeNormal {
		return m.handlePrompt(message)
	}
	entry := m.current()
	if entry == nil {
		return m, nil
	}
	switch message.String() {
	// q is bound only here, in the normal-mode switch: handlePrompt consumes
	// every key, so a q typed into a filter or command stays text.
	case "ctrl+c", "q":
		return m, tea.Quit
	case "j", "down":
		entry.table.Move(1)
		m.loadMore()
	case "k", "up":
		entry.table.Move(-1)
	case "ctrl+f":
		entry.table.Page(1)
		m.loadMore()
	case "ctrl+b":
		entry.table.Page(-1)
	case "g":
		entry.table.Home()
	case "G":
		entry.table.End()
		m.loadMore()
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
	case "r":
		if m.inFlight {
			return m, nil
		}
		m.inFlight = true
		return m, m.refresh(true)
	case ":":
		m.mode = modeCommand
		m.input = ""
		m.commandErr = ""
	case "/":
		m.mode = modeFilter
		m.input = ""
	case "?":
		m.showHelp = true
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
		m.stack = []stackEntry{newEntry(view, m.scope)}
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
	m.stack = append(m.stack, newEntry(next, scope))
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
		Tool:     string(m.scope.Tool),
		Tokens:   formatTokens(m.totals.Tokens),
		Cost:     fmt.Sprintf("$%.2f at API rates", m.totals.Cost),
		Requests: fmt.Sprintf("%d", m.totals.Requests),
		Unpriced: fmt.Sprintf("%d", m.unpriced),
	}
	interior, height := bodyWidth(m.width), bodyHeight(m.height)
	var body []string
	// The overlay is not a resource, so it borrows the border but not the
	// resource title — a row count there would be a count of nothing.
	title := "Help"
	if m.showHelp {
		body = helpBody(interior, height)
	} else {
		body = entry.table.Render()
		if renderer, ok := entry.view.(Renderer); ok {
			if custom, err := renderer.Body(m.db(), m.pricing, entry.scope,
				interior, height); err == nil {
				body = custom
			}
		}
		title = m.bodyTitle(entry)
	}
	return frame(headerBlock(info, m.width, m.height), bodyPanel(title, body, m.width),
		m.footer(), m.width, m.height)
}

// bodyTitle builds the border title for one stack level. The table already
// holds both counts the title needs: everything loaded, and everything the
// filter left visible.
func (m Model) bodyTitle(entry *stackEntry) string {
	_, rendered := entry.view.(Renderer)
	return bodyTitle(entry.view.Title(), scopeLabel(entry.scope),
		entry.table.VisibleCount(), entry.table.TotalCount(),
		entry.table.Filter() != "", entry.more, rendered)
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
	right := m.styledRefreshAge() + "   [enter] drill  [s]ort  [/]filter  [:]cmd  [?]help"
	if m.showHelp {
		right = m.styledRefreshAge() + "   any key dismisses"
	}
	if m.commandErr != "" {
		right = m.styledRefreshAge() + "   " + styleWarning.Render(m.commandErr)
	}
	// A failed refresh keeps the age beside it, and reddens it: the error says
	// what broke, the age says how stale the data on screen has become. Losing
	// the age here would hide exactly the thing the error makes urgent.
	if m.refreshErr != nil {
		right = styleDanger.Render(m.refreshAge()) + "   " +
			styleDanger.Render("refresh failed: "+m.refreshErr.Error())
	}
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 1 {
		return padLine(" "+left, m.width)
	}
	return " " + left + strings.Repeat(" ", gap) + right + " "
}

// refreshInterval matches k9s's default. Not configurable in this phase.
const refreshInterval = 2 * time.Second

type tickMsg struct{}

type refreshedMsg struct {
	at       time.Time
	totals   agg.TotalsResult
	rows     []Row
	more     bool
	unpriced int
	err      error
}

func (m Model) scheduleTick() tea.Cmd {
	return tea.Tick(refreshInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

// refresh runs an incremental ingest and refetches the current view off the UI
// thread. reingest is false for a plain data refetch.
func (m Model) refresh(reingest bool) tea.Cmd {
	st, pricing, entry := m.st, m.pricing, m.current()
	if entry == nil {
		return nil
	}
	view, scope, pages := entry.view, entry.scope, entry.pages
	global := m.scope
	return func() tea.Msg {
		now := time.Now()
		if st == nil {
			return refreshedMsg{at: now}
		}
		if reingest {
			home, err := os.UserHomeDir()
			if err != nil {
				return refreshedMsg{at: now, err: err}
			}
			if _, err := ingest.Run(st, ingest.DefaultSources(home), pricing, false); err != nil {
				return refreshedMsg{at: now, err: err}
			}
		}
		totals, err := agg.Totals(st.DB(), pricing, global.Filter)
		if err != nil {
			return refreshedMsg{at: now, err: err}
		}
		rows, more, err := fetchRows(view, st.DB(), pricing, scope, pages)
		if err != nil {
			return refreshedMsg{at: now, err: err}
		}
		unpriced, err := agg.UnpricedList(st.DB(), pricing, global.Filter)
		if err != nil {
			return refreshedMsg{at: now, err: err}
		}
		return refreshedMsg{
			at: now, totals: totals, rows: rows, more: more, unpriced: len(unpriced),
		}
	}
}

// Past these thresholds the age is no longer background information: it is the
// only visible sign that the 2s ticker has stopped landing (spec §4.3).
const (
	refreshStale = 30 * time.Second
	refreshDead  = 5 * time.Minute
)

// styledRefreshAge colours the age amber past 30s and red past 5 minutes, so a
// wedged ticker is visible rather than silent. A refresh that has never landed
// is amber too — it is not a healthy state, but there is no elapsed time yet to
// call it dead.
func (m Model) styledRefreshAge() string {
	text := m.refreshAge()
	if m.lastRefresh.IsZero() {
		return styleWarning.Render(text)
	}
	switch age := time.Since(m.lastRefresh); {
	case age > refreshDead:
		return styleDanger.Render(text)
	case age > refreshStale:
		return styleWarning.Render(text)
	default:
		return text
	}
}

func (m Model) refreshAge() string {
	if m.lastRefresh.IsZero() {
		return "never"
	}
	age := time.Since(m.lastRefresh)
	switch {
	case age < time.Minute:
		return fmt.Sprintf("%ds ago", int(age.Seconds()))
	case age < time.Hour:
		return fmt.Sprintf("%dm ago", int(age.Minutes()))
	default:
		return fmt.Sprintf("%dh ago", int(age.Hours()))
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

// Run starts the TUI. The landing view is Projects, per spec §11.2.
func Run(st *store.Store, pricing *model.Pricing, dbPath string) error {
	m := New(st, pricing, dbPath, ProjectsView{}, DefaultRegistry())
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}
