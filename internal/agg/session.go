package agg

import (
	"database/sql"
	"sort"
	"time"

	"github.com/seanochang/ccdash/internal/model"
)

type SessionBucket struct {
	Session  string
	Tool     model.Tool
	Project  string
	Started  time.Time
	Ended    time.Time
	Requests int
	Tokens   int64
	Cost     float64
	Unpriced int
}

// BySession groups requests into the sessions that produced them. A session's
// project and tool are taken from its first request; sessions do not migrate
// between projects in practice.
func BySession(db *sql.DB, pricing *model.Pricing, filter Filter) ([]SessionBucket, error) {
	rows, err := scanDetail(db, filter, "ts ASC", 0, 0)
	if err != nil {
		return nil, err
	}
	buckets := make(map[string]*SessionBucket)
	order := make([]string, 0, len(rows))
	for _, row := range rows {
		bucket := buckets[row.Session]
		if bucket == nil {
			bucket = &SessionBucket{
				Session: row.Session,
				Tool:    row.Tool,
				Project: row.Project,
				Started: row.TS,
			}
			buckets[row.Session] = bucket
			order = append(order, row.Session)
		}
		bucket.Requests++
		bucket.Ended = row.TS
		bucket.Tokens += row.InputTok + row.OutputTok + row.CacheReadTok +
			row.CacheWrite5m + row.CacheWrite1h
		if cost, ok := pricing.Cost(row.Record); ok {
			bucket.Cost += cost
		} else {
			bucket.Unpriced++
		}
	}
	result := make([]SessionBucket, 0, len(buckets))
	for _, key := range order {
		result = append(result, *buckets[key])
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Ended.Equal(result[j].Ended) {
			return result[i].Session < result[j].Session
		}
		return result[i].Ended.After(result[j].Ended)
	})
	return result, nil
}
