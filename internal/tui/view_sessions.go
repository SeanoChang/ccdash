package tui

import (
	"database/sql"

	"github.com/seanochang/ccdash/internal/model"
)

// Temporary stub. Replaced in full by Task 15.
type SessionsView struct{}

func (SessionsView) Title() string     { return "Sessions" }
func (SessionsView) Columns() []Column { return nil }
func (SessionsView) Rows(*sql.DB, *model.Pricing, Scope) ([]Row, error) {
	return nil, nil
}
func (SessionsView) Drill(Row, Scope) (View, Scope, bool) { return nil, Scope{}, false }
