package agg

import (
	"database/sql"
	"sort"
	"time"

	"github.com/seanochang/ccdash/internal/model"
)

type AgentBucket struct {
	Agent    string
	Workflow string
	Depth    int
	Requests int
	Tokens   int64
	Cost     float64
	// Unpriced counts requests in this bucket whose model has no rate. Cost
	// omits them, so a view must render an em dash rather than $0.00.
	Unpriced int
}

type WorkflowBucket struct {
	Workflow string
	Agents   int
	Requests int
	Tokens   int64
	Cost     float64
	Started  time.Time
	// Unpriced counts requests in this bucket whose model has no rate. Cost
	// omits them, so a view must render an em dash rather than $0.00.
	Unpriced int
}

func recordTokens(row detailRow) int64 {
	return row.InputTok + row.OutputTok + row.CacheReadTok +
		row.CacheWrite5m + row.CacheWrite1h
}

// ByAgent rolls up subagent activity. Main-loop requests carry an empty agent
// and are excluded: they are not subagents, and folding them into one nameless
// bucket would dwarf every real agent.
func ByAgent(db *sql.DB, pricing *model.Pricing, filter Filter) ([]AgentBucket, error) {
	rows, err := scanDetail(db, filter, "ts ASC", 0, 0)
	if err != nil {
		return nil, err
	}
	buckets := make(map[string]*AgentBucket)
	for _, row := range rows {
		if row.Agent == "" {
			continue
		}
		bucket := buckets[row.Agent]
		if bucket == nil {
			bucket = &AgentBucket{
				Agent: row.Agent, Workflow: row.Workflow, Depth: row.Depth,
			}
			buckets[row.Agent] = bucket
		}
		bucket.Requests++
		bucket.Tokens += recordTokens(row)
		if cost, ok := pricing.Cost(row.Record); ok {
			bucket.Cost += cost
		} else {
			bucket.Unpriced++
		}
	}
	result := make([]AgentBucket, 0, len(buckets))
	for _, bucket := range buckets {
		result = append(result, *bucket)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Cost == result[j].Cost {
			return result[i].Agent < result[j].Agent
		}
		return result[i].Cost > result[j].Cost
	})
	return result, nil
}

// ByWorkflow rolls up whole workflows. Agents counts distinct agent IDs seen
// under the workflow.
func ByWorkflow(db *sql.DB, pricing *model.Pricing, filter Filter) ([]WorkflowBucket, error) {
	rows, err := scanDetail(db, filter, "ts ASC", 0, 0)
	if err != nil {
		return nil, err
	}
	buckets := make(map[string]*WorkflowBucket)
	seenAgents := make(map[string]map[string]bool)
	for _, row := range rows {
		if row.Workflow == "" {
			continue
		}
		bucket := buckets[row.Workflow]
		if bucket == nil {
			bucket = &WorkflowBucket{Workflow: row.Workflow, Started: row.TS}
			buckets[row.Workflow] = bucket
			seenAgents[row.Workflow] = make(map[string]bool)
		}
		bucket.Requests++
		bucket.Tokens += recordTokens(row)
		if cost, ok := pricing.Cost(row.Record); ok {
			bucket.Cost += cost
		} else {
			bucket.Unpriced++
		}
		if row.Agent != "" && !seenAgents[row.Workflow][row.Agent] {
			seenAgents[row.Workflow][row.Agent] = true
			bucket.Agents++
		}
	}
	result := make([]WorkflowBucket, 0, len(buckets))
	for _, bucket := range buckets {
		result = append(result, *bucket)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Cost == result[j].Cost {
			return result[i].Workflow < result[j].Workflow
		}
		return result[i].Cost > result[j].Cost
	})
	return result, nil
}
