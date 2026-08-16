package model

import "time"

type Tool string

const (
	ToolClaude Tool = "claude"
	ToolCodex  Tool = "codex"
)

// Record is one normalized unit of model work. InputTok excludes cache reads.
type Record struct {
	ID       string
	Tool     Tool
	TS       time.Time
	Model    string
	Project  string
	Session  string
	Agent    string
	Workflow string
	Depth    int

	InputTok     int64
	OutputTok    int64
	ThinkingTok  int64
	CacheReadTok int64
	CacheWrite5m int64
	CacheWrite1h int64

	Anomaly bool
}

type LimitKind string

const (
	KindSession      LimitKind = "session"
	KindWeeklyAll    LimitKind = "weekly_all"
	KindWeeklyScoped LimitKind = "weekly_scoped"
	KindCodex5h      LimitKind = "codex_5h"
	KindCodexWeekly  LimitKind = "codex_weekly"
)

type Provenance string

const (
	ProvLive   Provenance = "live"
	ProvCached Provenance = "cached"
)

type LimitSample struct {
	Tool       Tool
	Kind       LimitKind
	Scope      string
	Percent    float64
	ResetsAt   *time.Time
	IsActive   bool
	ObservedAt time.Time
	Provenance Provenance
}
