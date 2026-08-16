package ingest

import (
	"path/filepath"
	"runtime"
	"sort"
	"sync"

	"github.com/seanochang/ccdash/internal/model"
	"github.com/seanochang/ccdash/internal/source"
	"github.com/seanochang/ccdash/internal/source/claude"
	"github.com/seanochang/ccdash/internal/source/codex"
	"github.com/seanochang/ccdash/internal/source/limits"
	"github.com/seanochang/ccdash/internal/store"
)

type Stats struct {
	Files       int                  `json:"files"`
	Skipped     int                  `json:"skipped_files"`
	Failed      int                  `json:"failed_files"`
	Scanned     int                  `json:"records_scanned"`
	Inserted    int                  `json:"records_inserted"`
	Limits      int                  `json:"limit_samples_inserted"`
	Unpriced    int                  `json:"unpriced_records_inserted"`
	Malformed   int                  `json:"malformed_lines"`
	Anomalies   int                  `json:"anomalies"`
	Candidates  int                  `json:"usage_events"`
	Duplicates  int                  `json:"duplicate_events"`
	ByTool      map[string]ToolStats `json:"by_tool"`
	FailedPaths []string             `json:"failed_paths,omitempty"`
}

type ToolStats struct {
	Files      int `json:"files"`
	Skipped    int `json:"skipped_files"`
	Failed     int `json:"failed_files"`
	Scanned    int `json:"records_scanned"`
	Inserted   int `json:"records_inserted"`
	Malformed  int `json:"malformed_lines"`
	Candidates int `json:"usage_events"`
	Duplicates int `json:"duplicate_events"`
}

func DefaultSources(home string) []source.Source {
	return []source.Source{
		claude.New(filepath.Join(home, ".claude", "projects")),
		codex.New(filepath.Join(home, ".codex", "sessions")),
		limits.NewClaudeJSON(filepath.Join(home, ".claude.json")),
		limits.NewStatusline(filepath.Join(home, ".local", "share", "ccdash", "statusline.jsonl")),
	}
}

type parseJob struct {
	file source.FileRef
	from int64
	res  source.Result
	err  error
}

// Run discovers, parses, and archives all changed sources. Parsing concurrency
// is capped at the machine's CPU count to avoid saturating the disk queue.
func Run(st *store.Store, sources []source.Source, pricing *model.Pricing, full bool) (Stats, error) {
	stats := Stats{ByTool: make(map[string]ToolStats)}
	seenThisRun := make(map[string]struct{})
	for _, adapter := range sources {
		toolName := string(adapter.Name())
		files, err := adapter.Discover()
		if err != nil {
			return stats, err
		}
		jobs := make([]parseJob, 0, len(files))
		for _, file := range files {
			from := int64(0)
			if !full {
				oldSize, oldMtime, oldOffset, ok := st.Cursor(file.Path)
				if ok {
					if oldSize == file.Size && oldMtime == file.Mtime && oldOffset >= file.Size {
						stats.Skipped++
						toolStats := stats.ByTool[toolName]
						toolStats.Skipped++
						stats.ByTool[toolName] = toolStats
						continue
					}
					// Only a strict size increase is an append. Same-size mtime
					// changes and shrinkage are rewrites and require a full parse.
					if file.Size > oldSize && oldOffset <= file.Size {
						from = oldOffset
					} else if oldSize == file.Size && oldMtime == file.Mtime && oldOffset < file.Size {
						// A partial final line was intentionally left behind.
						from = oldOffset
					}
				}
			}
			jobs = append(jobs, parseJob{file: file, from: from})
		}

		workers := runtime.NumCPU()
		if workers < 1 {
			workers = 1
		}
		if workers > len(jobs) {
			workers = len(jobs)
		}
		if workers > 0 {
			queue := make(chan int)
			var group sync.WaitGroup
			for range workers {
				group.Add(1)
				go func() {
					defer group.Done()
					for i := range queue {
						jobs[i].res, jobs[i].err = adapter.Parse(jobs[i].file, jobs[i].from)
					}
				}()
			}
			for i := range jobs {
				queue <- i
			}
			close(queue)
			group.Wait()
		}

		for _, job := range jobs {
			stats.Files++
			toolStats := stats.ByTool[toolName]
			toolStats.Files++
			if job.err != nil {
				stats.Failed++
				toolStats.Failed++
				stats.ByTool[toolName] = toolStats
				stats.FailedPaths = append(stats.FailedPaths, job.file.Path)
				continue
			}
			stats.Scanned += len(job.res.Records)
			stats.Malformed += job.res.Malformed
			stats.Candidates += job.res.Candidates
			stats.Duplicates += job.res.Duplicates
			toolStats.Scanned += len(job.res.Records)
			toolStats.Malformed += job.res.Malformed
			toolStats.Candidates += job.res.Candidates
			toolStats.Duplicates += job.res.Duplicates
			for _, record := range job.res.Records {
				if _, duplicate := seenThisRun[record.ID]; duplicate {
					stats.Duplicates++
					toolStats.Duplicates++
				} else {
					seenThisRun[record.ID] = struct{}{}
				}
			}
			inserted, err := st.UpsertRecordsDetailed(job.res.Records)
			if err != nil {
				return stats, err
			}
			stats.Inserted += len(inserted)
			toolStats.Inserted += len(inserted)
			stats.ByTool[toolName] = toolStats
			for _, record := range inserted {
				if record.Anomaly {
					stats.Anomalies++
				}
				if !pricing.HasRate(record.Model) {
					if err := st.NoteUnpriced(model.NormalizeModel(record.Model), record.TS); err != nil {
						return stats, err
					}
					stats.Unpriced++
				}
			}
			for _, limit := range job.res.Limits {
				wasInserted, err := st.InsertLimitIfChanged(limit)
				if err != nil {
					return stats, err
				}
				if wasInserted {
					stats.Limits++
				}
			}
			if err := st.SetCursor(
				job.file.Path, adapter.Name(), job.file.Size, job.file.Mtime, job.res.Offset,
			); err != nil {
				return stats, err
			}
		}
	}
	if err := st.ReconcileUnpriced(pricing); err != nil {
		return stats, err
	}
	sort.Strings(stats.FailedPaths)
	return stats, nil
}
