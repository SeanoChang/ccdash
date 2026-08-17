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
	CellNumber
	CellBar
	CellSparkline
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
	Title string
	Align Alignment
	Width int // 0 means flexible: share the remaining width
	Sort  SortKind
	Kind  CellKind
	Unit  Unit
}

// Cell is one table cell. Text is used for CellText and CellNumber, Value for
// sorting and for the CellBar fill, and Series for CellSparkline. Sparklines
// are rendered by Table rather than by the view, because their domain is
// shared across every row.
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

// Paginator is implemented only by views whose result set is too large to hold
// at once. Table type-asserts for it; a view that does not implement it is
// fetched whole via Rows.
type Paginator interface {
	Page(db *sql.DB, pricing *model.Pricing, scope Scope, offset, limit int) (rows []Row, more bool, err error)
	PageSize() int
}

// Renderer is implemented by views that paint their own body instead of a
// table. App checks for it before falling back to Table. Only PulseView
// implements it.
type Renderer interface {
	Body(db *sql.DB, pricing *model.Pricing, scope Scope, width, height int) ([]string, error)
}
