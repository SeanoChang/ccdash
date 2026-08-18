package agg

import (
	"database/sql"
	"sort"
	"strings"
	"time"

	"github.com/SeanoChang/ccdash/internal/model"
)

type Filter struct {
	From, To time.Time
	Tool     model.Tool
	Project  string
	Model    string
	Session  string
	Agent    string
	Workflow string
}

func (f Filter) where() (string, []any) {
	conditions := make([]string, 0, 8)
	args := make([]any, 0, 8)
	if !f.From.IsZero() {
		conditions = append(conditions, "ts >= ?")
		args = append(args, f.From.Unix())
	}
	if !f.To.IsZero() {
		// Exclusive: a calendar window's To is the next window's From, so an
		// inclusive bound would count a midnight request in both.
		conditions = append(conditions, "ts < ?")
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
	if f.Session != "" {
		conditions = append(conditions, "session = ?")
		args = append(args, f.Session)
	}
	if f.Agent != "" {
		conditions = append(conditions, "agent = ?")
		args = append(args, f.Agent)
	}
	if f.Workflow != "" {
		conditions = append(conditions, "workflow = ?")
		args = append(args, f.Workflow)
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
		record.TS = time.Unix(ts, 0).Local()
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
	// Unpriced counts requests in this bucket whose model has no rate. Cost
	// omits them, so a view must render an em dash rather than $0.00.
	Unpriced int
}

// dayIn truncates to midnight in loc. It is split from dayLocal so tests can
// pin a zone without depending on the machine's.
func dayIn(t time.Time, loc *time.Location) time.Time {
	year, month, day := t.In(loc).Date()
	return time.Date(year, month, day, 0, 0, 0, 0, loc)
}

// dayLocal buckets by the user's wall clock. Bucketing in UTC filed an
// evening session in a western zone under the following day, in the Days
// view, the Pulse axis and every Started column at once.
func dayLocal(t time.Time) time.Time { return dayIn(t, time.Local) }

// Resolution is the width of one chart bucket. A window narrower than a day
// has nothing to plot against day buckets, so the caller picks from the span
// it resolved rather than from a constant.
type Resolution int

const (
	ResDay Resolution = iota
	ResHour
)

// bucketOf truncates t to the start of its bucket in the local zone.
func bucketOf(t time.Time, res Resolution) time.Time {
	if res == ResHour {
		local := t.Local()
		year, month, day := local.Date()
		return time.Date(year, month, day, local.Hour(), 0, 0, 0, time.Local)
	}
	return dayLocal(t)
}

func ByBucket(db *sql.DB, pricing *model.Pricing, filter Filter,
	res Resolution) ([]DayBucket, error) {
	rows, err := scanRows(db, filter)
	if err != nil {
		return nil, err
	}
	buckets := make(map[time.Time]*DayBucket)
	for _, row := range rows {
		day := bucketOf(row.record.TS, res)
		bucket := buckets[day]
		if bucket == nil {
			bucket = &DayBucket{Day: day}
			buckets[day] = bucket
		}
		bucket.Tokens += row.record.InputTok + row.record.OutputTok +
			row.record.CacheReadTok + row.record.CacheWrite5m + row.record.CacheWrite1h
		if cost, ok := pricing.Cost(row.record); ok {
			bucket.Cost += cost
		} else {
			bucket.Unpriced++
		}
	}
	result := make([]DayBucket, 0, len(buckets))
	for _, bucket := range buckets {
		result = append(result, *bucket)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Day.Before(result[j].Day) })
	return result, nil
}

// ByDay is ByBucket at day resolution. ByProject's sparkline and the Days
// view are both day-shaped, so they keep this signature.
func ByDay(db *sql.DB, pricing *model.Pricing, filter Filter) ([]DayBucket, error) {
	return ByBucket(db, pricing, filter, ResDay)
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
	// Unpriced counts requests in this bucket whose model has no rate. Cost
	// omits them, so a view must render an em dash rather than $0.00.
	Unpriced int `json:"unpriced_requests"`
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
		} else {
			bucket.Unpriced++
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
	// Unpriced counts requests in this bucket whose model has no rate. Cost
	// omits them, so a view must render an em dash rather than $0.00.
	Unpriced int
}

const sparkPoints = 14

func ByProject(db *sql.DB, pricing *model.Pricing, filter Filter) ([]ProjectBucket, error) {
	rows, err := scanRows(db, filter)
	if err != nil {
		return nil, err
	}
	type accumulation struct {
		cost     float64
		unpriced int
		days     map[time.Time]float64
	}
	buckets := make(map[string]*accumulation)
	var latestDay time.Time
	for _, row := range rows {
		day := dayLocal(row.record.TS)
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
		} else {
			bucket.unpriced++
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
		result = append(result, ProjectBucket{
			Project:  name,
			Cost:     bucket.cost,
			Spark:    spark,
			Unpriced: bucket.unpriced,
		})
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

const detailColumns = `id,tool,model,project,session,agent,workflow,depth,ts,` +
	`in_tok,out_tok,think_tok,cache_read,cache_w5m,cache_w1h,anomaly`

// detailRow carries the identity columns that the aggregate scanners omit.
type detailRow struct {
	model.Record
	ID       string
	Tool     model.Tool
	Session  string
	Workflow string
	Depth    int
	Anomaly  bool
}

// scanDetail reads full request rows. order must be a trusted literal such as
// "ts DESC"; it is never built from user input. limit <= 0 means no limit.
func scanDetail(db *sql.DB, filter Filter, order string, limit, offset int) ([]detailRow, error) {
	where, args := filter.where()
	query := `SELECT ` + detailColumns + ` FROM request` + where
	if order != "" {
		query += ` ORDER BY ` + order
	}
	if limit > 0 {
		query += ` LIMIT ? OFFSET ?`
		args = append(args, limit, offset)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []detailRow
	for rows.Next() {
		var (
			row                               detailRow
			tool                              string
			project, session, agent, workflow sql.NullString
			ts                                int64
			anomaly                           int
		)
		if err := rows.Scan(&row.ID, &tool, &row.Model, &project, &session,
			&agent, &workflow, &row.Depth, &ts,
			&row.InputTok, &row.OutputTok, &row.ThinkingTok,
			&row.CacheReadTok, &row.CacheWrite5m, &row.CacheWrite1h,
			&anomaly); err != nil {
			return nil, err
		}
		row.Tool = model.Tool(tool)
		row.Record.Tool = row.Tool
		row.Record.TS = time.Unix(ts, 0).Local()
		row.TS = row.Record.TS
		row.Project = project.String
		row.Record.Project = project.String
		row.Session = session.String
		row.Record.Session = session.String
		row.Agent = agent.String
		row.Record.Agent = agent.String
		row.Workflow = workflow.String
		row.Record.Workflow = workflow.String
		row.Anomaly = anomaly == 1
		result = append(result, row)
	}
	return result, rows.Err()
}
