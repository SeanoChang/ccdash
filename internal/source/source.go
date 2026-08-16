package source

import "github.com/seanochang/llm-usage-dashboard/internal/model"

type FileRef struct {
	Path  string
	Size  int64
	Mtime int64
}

type Result struct {
	Records    []model.Record
	Limits     []model.LimitSample
	Offset     int64
	Malformed  int
	Candidates int
	Duplicates int
}

// Source confines all on-disk format knowledge to an adapter. Everything
// downstream receives normalized records and limit samples.
type Source interface {
	Name() model.Tool
	Discover() ([]FileRef, error)
	Parse(FileRef, int64) (Result, error)
}
