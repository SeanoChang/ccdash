package limits

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"time"

	"github.com/SeanoChang/ccdash/internal/model"
	"github.com/SeanoChang/ccdash/internal/source"
)

func parseTime(value string) *time.Time {
	if value == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil
	}
	return &parsed
}

type ClaudeJSON struct {
	path string
}

func NewClaudeJSON(path string) *ClaudeJSON { return &ClaudeJSON{path: path} }

func (c *ClaudeJSON) Name() model.Tool { return model.ToolClaude }

func (c *ClaudeJSON) Discover() ([]source.FileRef, error) {
	info, err := os.Stat(c.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return []source.FileRef{{Path: c.path, Size: info.Size(), Mtime: info.ModTime().Unix()}}, nil
}

type claudeJSONDocument struct {
	Cached struct {
		FetchedAtMs int64 `json:"fetchedAtMs"`
		Utilization struct {
			Limits []struct {
				Kind     string  `json:"kind"`
				Percent  float64 `json:"percent"`
				ResetsAt string  `json:"resets_at"`
				IsActive bool    `json:"is_active"`
				Scope    *struct {
					Model *struct {
						DisplayName string `json:"display_name"`
					} `json:"model"`
				} `json:"scope"`
			} `json:"limits"`
		} `json:"utilization"`
	} `json:"cachedUsageUtilization"`
}

func (c *ClaudeJSON) Parse(file source.FileRef, _ int64) (source.Result, error) {
	b, err := os.ReadFile(file.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return source.Result{}, nil
		}
		return source.Result{}, err
	}
	result := source.Result{Offset: int64(len(b))}
	var document claudeJSONDocument
	if err := json.Unmarshal(b, &document); err != nil {
		result.Malformed = 1
		return result, nil
	}
	observed := time.UnixMilli(document.Cached.FetchedAtMs)
	if document.Cached.FetchedAtMs == 0 {
		observed = time.Unix(file.Mtime, 0)
	}
	for _, raw := range document.Cached.Utilization.Limits {
		kind := model.LimitKind(raw.Kind)
		switch kind {
		case model.KindSession, model.KindWeeklyAll, model.KindWeeklyScoped:
		default:
			continue
		}
		scope := ""
		if raw.Scope != nil && raw.Scope.Model != nil {
			scope = raw.Scope.Model.DisplayName
		}
		result.Limits = append(result.Limits, model.LimitSample{
			Tool: model.ToolClaude, Kind: kind, Scope: scope,
			Percent: raw.Percent, ResetsAt: parseTime(raw.ResetsAt),
			IsActive: raw.IsActive, ObservedAt: observed,
			Provenance: model.ProvCached,
		})
	}
	return result, nil
}

type Statusline struct {
	path string
}

func NewStatusline(path string) *Statusline { return &Statusline{path: path} }

func (s *Statusline) Name() model.Tool { return model.ToolClaude }

func (s *Statusline) Discover() ([]source.FileRef, error) {
	info, err := os.Stat(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return []source.FileRef{{Path: s.path, Size: info.Size(), Mtime: info.ModTime().Unix()}}, nil
}

type statuslinePayload struct {
	RateLimits struct {
		FiveHour *struct {
			UsedPercentage float64 `json:"used_percentage"`
			ResetsAt       string  `json:"resets_at"`
		} `json:"five_hour"`
		SevenDay *struct {
			UsedPercentage float64 `json:"used_percentage"`
			ResetsAt       string  `json:"resets_at"`
		} `json:"seven_day"`
	} `json:"rate_limits"`
}

func (s *Statusline) Parse(file source.FileRef, from int64) (source.Result, error) {
	lineCount, err := countCompleteLines(file.Path, from)
	if err != nil {
		if os.IsNotExist(err) {
			return source.Result{}, nil
		}
		return source.Result{}, err
	}
	fh, err := os.Open(file.Path)
	if err != nil {
		return source.Result{}, err
	}
	defer fh.Close()
	if from > 0 {
		if _, err := fh.Seek(from, io.SeekStart); err != nil {
			return source.Result{}, err
		}
	}

	result := source.Result{Offset: from}
	observed := time.Unix(file.Mtime, 0)
	if lineCount > 1 {
		observed = observed.Add(-time.Duration(lineCount-1) * time.Second)
	}
	lineNumber := 0
	reader := bufio.NewReaderSize(fh, 64*1024)
	for {
		raw, readErr := reader.ReadBytes('\n')
		if len(raw) == 0 && errors.Is(readErr, io.EOF) {
			break
		}
		complete := len(raw) > 0 && raw[len(raw)-1] == '\n'
		body := bytes.TrimSuffix(raw, []byte{'\n'})
		body = bytes.TrimSuffix(body, []byte{'\r'})
		if !complete && !json.Valid(body) {
			break
		}
		result.Offset += int64(len(raw))
		var payload statuslinePayload
		if err := json.Unmarshal(body, &payload); err != nil {
			result.Malformed++
		} else {
			at := observed.Add(time.Duration(lineNumber) * time.Second)
			if payload.RateLimits.FiveHour != nil {
				result.Limits = append(result.Limits, model.LimitSample{
					Tool: model.ToolClaude, Kind: model.KindSession,
					Percent:    payload.RateLimits.FiveHour.UsedPercentage,
					ResetsAt:   parseTime(payload.RateLimits.FiveHour.ResetsAt),
					ObservedAt: at, Provenance: model.ProvLive,
				})
			}
			if payload.RateLimits.SevenDay != nil {
				result.Limits = append(result.Limits, model.LimitSample{
					Tool: model.ToolClaude, Kind: model.KindWeeklyAll,
					Percent:    payload.RateLimits.SevenDay.UsedPercentage,
					ResetsAt:   parseTime(payload.RateLimits.SevenDay.ResetsAt),
					ObservedAt: at, Provenance: model.ProvLive,
				})
			}
		}
		lineNumber++
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return result, readErr
		}
	}
	return result, nil
}

func countCompleteLines(path string, from int64) (int, error) {
	fh, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer fh.Close()
	if from > 0 {
		if _, err := fh.Seek(from, io.SeekStart); err != nil {
			return 0, err
		}
	}
	reader := bufio.NewReaderSize(fh, 64*1024)
	count := 0
	for {
		raw, readErr := reader.ReadBytes('\n')
		if len(raw) == 0 && errors.Is(readErr, io.EOF) {
			return count, nil
		}
		if len(raw) > 0 && (raw[len(raw)-1] == '\n' || json.Valid(raw)) {
			count++
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return count, nil
			}
			return 0, readErr
		}
	}
}
