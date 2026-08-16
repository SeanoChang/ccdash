package agg

import (
	"database/sql"
	"sort"
	"strings"
	"time"

	"github.com/seanochang/ccdash/internal/model"
)

type Filter struct {
	From, To time.Time
	Tool     model.Tool
	Project  string
	Model    string
}

func (f Filter) where() (string, []any) {
	conditions := make([]string, 0, 5)
	args := make([]any, 0, 5)
	if !f.From.IsZero() {
		conditions = append(conditions, "ts >= ?")
		args = append(args, f.From.Unix())
	}
	if !f.To.IsZero() {
		conditions = append(conditions, "ts <= ?")
		args = append(args, f.To.Unix())
	}
	if f.Tool != "" {
		conditions = append(conditions, "tool = ?")
		args = append(args, string(f.Tool))
	}
	if f.Project != "" {
		conditions = append(conditions, "project = ?")
		args = append(args, f.Project)
	}
	if f.Model != "" {
		conditions = append(conditions, "model = ?")
		args = append(args, model.NormalizeModel(f.Model))
	}
	if len(conditions) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conditions, " AND "), args
}

const recordColumns = `model,agent,project,ts,in_tok,out_tok,think_tok,cache_read,cache_w5m,cache_w1h`

type storedRow struct {
	record model.Record
}

func scanRows(db *sql.DB, filter Filter) ([]storedRow, error) {
	where, args := filter.where()
	rows, err := db.Query(`SELECT `+recordColumns+` FROM request`+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []storedRow
	for rows.Next() {
		var record model.Record
		var ts int64
		var agent, project sql.NullString
		if err := rows.Scan(
			&record.Model, &agent, &project, &ts, &record.InputTok,
			&record.OutputTok, &record.ThinkingTok, &record.CacheReadTok,
			&record.CacheWrite5m, &record.CacheWrite1h,
		); err != nil {
			return nil, err
		}
		record.TS = time.Unix(ts, 0).UTC()
		record.Agent = agent.String
		record.Project = project.String
		result = append(result, storedRow{record: record})
	}
	return result, rows.Err()
}

type TotalsResult struct {
	Requests                int
	Tokens, Input, Output   int64
	CacheRead, CacheWrite   int64
	MainTokens, SubTokens   int64
	Cost, MainCost, SubCost float64
	From, To                time.Time
}

func Totals(db *sql.DB, pricing *model.Pricing, filter Filter) (TotalsResult, error) {
	rows, err := scanRows(db, filter)
	if err != nil {
		return TotalsResult{}, err
	}
	var totals TotalsResult
	for _, row := range rows {
		record := row.record
		totals.Requests++
		totals.Input += record.InputTok
		totals.Output += record.OutputTok
		totals.CacheRead += record.CacheReadTok
		totals.CacheWrite += record.CacheWrite5m + record.CacheWrite1h
		recordTokens := record.InputTok + record.OutputTok + record.CacheReadTok +
			record.CacheWrite5m + record.CacheWrite1h
		totals.Tokens += recordTokens
		if record.Agent == "" {
			totals.MainTokens += recordTokens
		} else {
			totals.SubTokens += recordTokens
		}
		if cost, ok := pricing.Cost(record); ok {
			totals.Cost += cost
			if record.Agent == "" {
				totals.MainCost += cost
			} else {
				totals.SubCost += cost
			}
		}
		if totals.From.IsZero() || record.TS.Before(totals.From) {
			totals.From = record.TS
		}
		if totals.To.IsZero() || record.TS.After(totals.To) {
			totals.To = record.TS
		}
	}
	return totals, nil
}

type DayBucket struct {
	Day    time.Time
	Tokens int64
	Cost   float64
}

func dayUTC(t time.Time) time.Time {
	year, month, day := t.UTC().Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func ByDay(db *sql.DB, pricing *model.Pricing, filter Filter) ([]DayBucket, error) {
	rows, err := scanRows(db, filter)
	if err != nil {
		return nil, err
	}
	buckets := make(map[time.Time]*DayBucket)
	for _, row := range rows {
		day := dayUTC(row.record.TS)
		bucket := buckets[day]
		if bucket == nil {
			bucket = &DayBucket{Day: day}
			buckets[day] = bucket
		}
		bucket.Tokens += row.record.InputTok + row.record.OutputTok +
			row.record.CacheReadTok + row.record.CacheWrite5m + row.record.CacheWrite1h
		if cost, ok := pricing.Cost(row.record); ok {
			bucket.Cost += cost
		}
	}
	result := make([]DayBucket, 0, len(buckets))
	for _, bucket := range buckets {
		result = append(result, *bucket)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Day.Before(result[j].Day) })
	return result, nil
}

type ModelBucket struct {
	Model        string  `json:"model"`
	Requests     int     `json:"requests"`
	Tokens       int64   `json:"tokens"`
	InputTok     int64   `json:"input_tokens"`
	OutputTok    int64   `json:"output_tokens"`
	ThinkingTok  int64   `json:"thinking_tokens"`
	CacheReadTok int64   `json:"cache_read_tokens"`
	CacheWrite5m int64   `json:"cache_write_5m_tokens"`
	CacheWrite1h int64   `json:"cache_write_1h_tokens"`
	Cost         float64 `json:"cost_at_api_rates"`
}

func ByModel(db *sql.DB, pricing *model.Pricing, filter Filter) ([]ModelBucket, error) {
	rows, err := scanRows(db, filter)
	if err != nil {
		return nil, err
	}
	buckets := make(map[string]*ModelBucket)
	for _, row := range rows {
		name := model.NormalizeModel(row.record.Model)
		bucket := buckets[name]
		if bucket == nil {
			bucket = &ModelBucket{Model: name}
			buckets[name] = bucket
		}
		bucket.Requests++
		bucket.InputTok += row.record.InputTok
		bucket.OutputTok += row.record.OutputTok
		bucket.ThinkingTok += row.record.ThinkingTok
		bucket.CacheReadTok += row.record.CacheReadTok
		bucket.CacheWrite5m += row.record.CacheWrite5m
		bucket.CacheWrite1h += row.record.CacheWrite1h
		bucket.Tokens += row.record.InputTok + row.record.OutputTok +
			row.record.CacheReadTok + row.record.CacheWrite5m + row.record.CacheWrite1h
		if cost, ok := pricing.Cost(row.record); ok {
			bucket.Cost += cost
		}
	}
	result := make([]ModelBucket, 0, len(buckets))
	for _, bucket := range buckets {
		result = append(result, *bucket)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Cost == result[j].Cost {
			if result[i].Requests == result[j].Requests {
				return result[i].Model < result[j].Model
			}
			return result[i].Requests > result[j].Requests
		}
		return result[i].Cost > result[j].Cost
	})
	return result, nil
}

type ProjectBucket struct {
	Project string
	Cost    float64
	Spark   []float64
}

const sparkPoints = 14

func ByProject(db *sql.DB, pricing *model.Pricing, filter Filter) ([]ProjectBucket, error) {
	rows, err := scanRows(db, filter)
	if err != nil {
		return nil, err
	}
	type accumulation struct {
		cost float64
		days map[time.Time]float64
	}
	buckets := make(map[string]*accumulation)
	var latestDay time.Time
	for _, row := range rows {
		day := dayUTC(row.record.TS)
		if latestDay.IsZero() || day.After(latestDay) {
			latestDay = day
		}
		bucket := buckets[row.record.Project]
		if bucket == nil {
			bucket = &accumulation{days: make(map[time.Time]float64)}
			buckets[row.record.Project] = bucket
		}
		if cost, ok := pricing.Cost(row.record); ok {
			bucket.cost += cost
			bucket.days[day] += cost
		}
	}
	result := make([]ProjectBucket, 0, len(buckets))
	start := latestDay.AddDate(0, 0, -(sparkPoints - 1))
	for name, bucket := range buckets {
		spark := make([]float64, sparkPoints)
		if !latestDay.IsZero() {
			for i := range sparkPoints {
				spark[i] = bucket.days[start.AddDate(0, 0, i)]
			}
		}
		result = append(result, ProjectBucket{Project: name, Cost: bucket.cost, Spark: spark})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Cost == result[j].Cost {
			return result[i].Project < result[j].Project
		}
		return result[i].Cost > result[j].Cost
	})
	return result, nil
}

type LimitState struct {
	model.LimitSample
	Age time.Duration
}

func LatestLimits(db *sql.DB) ([]LimitState, error) {
	rows, err := db.Query(`
      SELECT l.tool,l.kind,l.scope,l.percent,l.resets_at,l.is_active,
             l.last_seen,l.provenance
      FROM limit_sample l
      JOIN (
        SELECT tool,kind,scope,MAX(observed_at) AS newest
        FROM limit_sample GROUP BY tool,kind,scope
      ) latest
        ON l.tool=latest.tool AND l.kind=latest.kind AND l.scope=latest.scope
       AND l.observed_at=latest.newest
      ORDER BY l.tool,l.kind,l.scope`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	now := time.Now()
	var result []LimitState
	for rows.Next() {
		var (
			sample                 model.LimitSample
			tool, kind, provenance string
			reset                  sql.NullInt64
			active                 int
			lastSeen               int64
		)
		if err := rows.Scan(&tool, &kind, &sample.Scope, &sample.Percent, &reset,
			&active, &lastSeen, &provenance); err != nil {
			return nil, err
		}
		sample.Tool = model.Tool(tool)
		sample.Kind = model.LimitKind(kind)
		sample.Provenance = model.Provenance(provenance)
		sample.IsActive = active == 1
		sample.ObservedAt = time.Unix(lastSeen, 0)
		if reset.Valid {
			value := time.Unix(reset.Int64, 0)
			sample.ResetsAt = &value
		}
		age := now.Sub(sample.ObservedAt)
		if age < 0 {
			age = 0
		}
		result = append(result, LimitState{LimitSample: sample, Age: age})
	}
	return result, rows.Err()
}
