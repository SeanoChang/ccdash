package agg

import (
	"database/sql"
	"sort"
	"time"

	"github.com/SeanoChang/ccdash/internal/model"
)

type RequestRow struct {
	ID         string
	TS         time.Time
	Tool       model.Tool
	Model      string
	Project    string
	Session    string
	Agent      string
	Input      int64
	Output     int64
	Thinking   int64
	CacheRead  int64
	CacheWrite int64
	Cost       float64
	Priced     bool
	Anomaly    bool
}

// Requests lists individual requests newest first. limit <= 0 returns every
// matching row; the TUI always passes a limit.
func Requests(db *sql.DB, pricing *model.Pricing, filter Filter, limit, offset int) ([]RequestRow, error) {
	rows, err := scanDetail(db, filter, "ts DESC", limit, offset)
	if err != nil {
		return nil, err
	}
	result := make([]RequestRow, 0, len(rows))
	for _, row := range rows {
		out := RequestRow{
			ID: row.ID, TS: row.TS, Tool: row.Tool,
			Model: model.NormalizeModel(row.Model), Project: row.Project,
			Session: row.Session, Agent: row.Agent,
			Input: row.InputTok, Output: row.OutputTok, Thinking: row.ThinkingTok,
			CacheRead:  row.CacheReadTok,
			CacheWrite: row.CacheWrite5m + row.CacheWrite1h,
			Anomaly:    row.Anomaly,
		}
		out.Cost, out.Priced = pricing.Cost(row.Record)
		result = append(result, out)
	}
	return result, nil
}

type UnpricedRow struct {
	Model     string
	Requests  int
	Tokens    int64
	FirstSeen time.Time
	LastSeen  time.Time
}

// UnpricedList reports models the live rate table cannot price, derived from
// request rows rather than from ingest-time bookkeeping. Editing the rate table
// therefore empties this view with no re-ingest.
func UnpricedList(db *sql.DB, pricing *model.Pricing, filter Filter) ([]UnpricedRow, error) {
	rows, err := scanDetail(db, filter, "ts ASC", 0, 0)
	if err != nil {
		return nil, err
	}
	buckets := make(map[string]*UnpricedRow)
	for _, row := range rows {
		if _, ok := pricing.Cost(row.Record); ok {
			continue
		}
		name := model.NormalizeModel(row.Model)
		bucket := buckets[name]
		if bucket == nil {
			bucket = &UnpricedRow{Model: name, FirstSeen: row.TS}
			buckets[name] = bucket
		}
		bucket.Requests++
		bucket.Tokens += recordTokens(row)
		bucket.LastSeen = row.TS
	}
	result := make([]UnpricedRow, 0, len(buckets))
	for _, bucket := range buckets {
		result = append(result, *bucket)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Requests == result[j].Requests {
			return result[i].Model < result[j].Model
		}
		return result[i].Requests > result[j].Requests
	})
	return result, nil
}
