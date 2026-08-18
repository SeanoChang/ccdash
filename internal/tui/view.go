package tui

import (
	"database/sql"

	"github.com/seanochang/ccdash/internal/agg"
	"github.com/seanochang/ccdash/internal/model"
)

type Alignment int

const (
	AlignLeft Alignment = iota
	AlignRight
)

type SortKind int

const (
	SortString SortKind = iota
	SortNumeric
	SortTime
)

type CellKind int

const (
	CellText CellKind = iota
	CellPath
	CellNumber
	CellBar
	CellPercentBar
	CellSparkline
)

// SparkScale controls whether a sparkline preserves magnitude across rows or
// emphasizes the shape of each row independently.
type SparkScale int

const (
	SparkScaleShared SparkScale = iota
	SparkScaleLocal
)

// Unit is the quantity a column measures. It is only consulted when a value has
// to be printed outside a cell — today, the shared maximum in a sparkline
// column's header — so UnitNumber is a safe default for every other column.
type Unit int

const (
	UnitNumber Unit = iota
	UnitMoney
)

// Column describes one column of a resource table.
type Column struct {
	Title           string
	Align           Alignment
	Width           int // 0 means flexible: share the remaining width
	Sort            SortKind
	Kind            CellKind
	Unit            Unit
	SparkScale      SparkScale
	DisableSort     bool
	DefaultSortDesc bool
}

// Cell is one table cell. Text is used for text, paths, and numbers; Value is
// used for numeric sorting and bars; and Series is used for sparklines.
type Cell struct {
	Text   string
	Value  float64
	Series []float64
}

// Row is one table row. Key is a stable identity used to keep the selection
// anchored across refreshes.
type Row struct {
	Key   string
	Cells []Cell
}

// Scope is the current filter plus any drill-down narrowing.
type Scope struct {
	agg.Filter
}

// View is one navigable resource.
type View interface {
	Title() string
	Columns() []Column
	Rows(db *sql.DB, pricing *model.Pricing, scope Scope) ([]Row, error)
	// Drill returns the view entered by pressing enter on row, along with the
	// scope narrowing it implies, and false when the resource is a leaf.
	Drill(row Row, scope Scope) (View, Scope, bool)
}

// DefaultSorter lets a view expose the active sort instead of relying on an
// implicit order returned by its aggregation query.
type DefaultSorter interface {
	DefaultSort() (column int, descending bool)
}

// Paginator is implemented only by views whose result set is too large to hold
// at once. Table type-asserts for it; a view that does not implement it is
// fetched whole via Rows.
type Paginator interface {
	Page(db *sql.DB, pricing *model.Pricing, scope Scope, offset, limit int) (rows []Row, more bool, err error)
	PageSize() int
}

// Unscoped is implemented by a view the scope does not narrow. Its border
// title carries the count with no "(scope)" parenthesis, because a title
// reading Help(claude) would advertise a filter the body never applied — the
// same lie the Limits pane told in 77a23ea, in a different place. Only
// HelpView implements it.
type Unscoped interface {
	UnscopedTitle() bool
}

// Renderer is implemented by views that paint their own body instead of a
// table. App checks for it before falling back to Table. Only PulseView
// implements it.
type Renderer interface {
	Body(db *sql.DB, pricing *model.Pricing, scope Scope, width, height int) ([]string, error)
}
