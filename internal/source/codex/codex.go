package codex

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/seanochang/llm-usage-dashboard/internal/model"
	"github.com/seanochang/llm-usage-dashboard/internal/source"
)

var tokenMarker = []byte(`"token_count"`)

type Source struct {
	root string
}

func New(root string) *Source { return &Source{root: root} }

func (s *Source) Name() model.Tool { return model.ToolCodex }

func (s *Source) Discover() ([]source.FileRef, error) {
	if _, err := os.Stat(s.root); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var files []source.FileRef
	err := filepath.WalkDir(s.root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		files = append(files, source.FileRef{
			Path: path, Size: info.Size(), Mtime: info.ModTime().Unix(),
		})
		return nil
	})
	return files, err
}

type usage struct {
	InputTokens     int64 `json:"input_tokens"`
	CachedInput     int64 `json:"cached_input_tokens"`
	CacheWriteInput int64 `json:"cache_write_input_tokens"`
	OutputTokens    int64 `json:"output_tokens"`
	ReasoningOutput int64 `json:"reasoning_output_tokens"`
	TotalTokens     int64 `json:"total_tokens"`
}

type window struct {
	UsedPercent   float64 `json:"used_percent"`
	WindowMinutes int     `json:"window_minutes"`
	ResetsAt      int64   `json:"resets_at"`
}

type line struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Payload   struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Cwd  string `json:"cwd"`

		Model string `json:"model"`

		Info *struct {
			TotalTokenUsage *usage `json:"total_token_usage"`
		} `json:"info"`
		RateLimits *struct {
			Primary   *window `json:"primary"`
			Secondary *window `json:"secondary"`
		} `json:"rate_limits"`
	} `json:"payload"`
}

func kindFor(minutes int) (model.LimitKind, bool) {
	switch {
	case minutes >= 240 && minutes <= 360:
		return model.KindCodex5h, true
	case minutes >= 9000 && minutes <= 11000:
		return model.KindCodexWeekly, true
	default:
		return "", false
	}
}

func delta(previous, current int64) (int64, bool) {
	if current < previous {
		return current, true
	}
	return current - previous, false
}

// Parse reconstructs metadata and cumulative-counter state from the prefix
// before a cursor, then emits only events at or after the cursor. Seeking
// directly to from would incorrectly treat the next cumulative total as a
// complete delta and would lose session/model identity.
func (s *Source) Parse(file source.FileRef, from int64) (source.Result, error) {
	fh, err := os.Open(file.Path)
	if err != nil {
		return source.Result{}, err
	}
	defer fh.Close()

	result := source.Result{}
	reader := bufio.NewReaderSize(fh, 64*1024)
	var sessionID, cwd, modelID string
	var previous usage
	lineIndex := 0
	offset := int64(0)
	for {
		lineStart := offset
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
		offset += int64(len(raw))
		result.Offset = offset
		lineIndex++

		var parsed line
		if err := json.Unmarshal(body, &parsed); err != nil {
			if lineStart >= from {
				result.Malformed++
			}
			if readErr != nil {
				if errors.Is(readErr, io.EOF) {
					break
				}
				return result, readErr
			}
			continue
		}
		switch parsed.Type {
		case "session_meta":
			sessionID, cwd = parsed.Payload.ID, parsed.Payload.Cwd
		case "turn_context":
			if parsed.Payload.Model != "" {
				modelID = model.NormalizeModel(parsed.Payload.Model)
			}
			if parsed.Payload.Cwd != "" {
				cwd = parsed.Payload.Cwd
			}
		}

		if !bytes.Contains(body, tokenMarker) || parsed.Payload.Type != "token_count" {
			if readErr != nil {
				if errors.Is(readErr, io.EOF) {
					break
				}
				return result, readErr
			}
			continue
		}
		ts, err := time.Parse(time.RFC3339Nano, parsed.Timestamp)
		if err != nil {
			ts = time.Unix(file.Mtime, 0)
		}
		emit := lineStart >= from
		if emit {
			result.Candidates++
		}

		if emit && parsed.Payload.RateLimits != nil {
			for _, limit := range []*window{
				parsed.Payload.RateLimits.Primary,
				parsed.Payload.RateLimits.Secondary,
			} {
				if limit == nil {
					continue
				}
				kind, ok := kindFor(limit.WindowMinutes)
				if !ok {
					continue
				}
				var resetsAt *time.Time
				if limit.ResetsAt > 0 {
					reset := time.Unix(limit.ResetsAt, 0)
					resetsAt = &reset
				}
				result.Limits = append(result.Limits, model.LimitSample{
					Tool: model.ToolCodex, Kind: kind, Percent: limit.UsedPercent,
					ResetsAt: resetsAt, ObservedAt: ts, Provenance: model.ProvLive,
				})
			}
		}

		if parsed.Payload.Info != nil && parsed.Payload.Info.TotalTokenUsage != nil {
			current := *parsed.Payload.Info.TotalTokenUsage
			dInput, aInput := delta(previous.InputTokens, current.InputTokens)
			dCached, aCached := delta(previous.CachedInput, current.CachedInput)
			dWrite, aWrite := delta(previous.CacheWriteInput, current.CacheWriteInput)
			dOutput, aOutput := delta(previous.OutputTokens, current.OutputTokens)
			dReason, aReason := delta(previous.ReasoningOutput, current.ReasoningOutput)
			_, aTotal := delta(previous.TotalTokens, current.TotalTokens)
			previous = current

			changed := dInput != 0 || dCached != 0 || dWrite != 0 || dOutput != 0 || dReason != 0
			anomaly := aInput || aCached || aWrite || aOutput || aReason || aTotal
			if emit && !changed && !anomaly {
				result.Duplicates++
			}
			if emit && (changed || anomaly) {
				fresh := dInput - dCached
				badSemantics := fresh < 0
				if badSemantics {
					fresh = 0
				}
				recordModel := modelID
				if recordModel == "" {
					recordModel = "<unknown>"
				}
				result.Records = append(result.Records, model.Record{
					ID:   fmt.Sprintf("%s:%d", sessionID, lineIndex),
					Tool: model.ToolCodex, TS: ts, Model: recordModel,
					Project: cwd, Session: sessionID, InputTok: fresh,
					OutputTok: dOutput, ThinkingTok: dReason,
					CacheReadTok: dCached, CacheWrite5m: dWrite,
					Anomaly: anomaly || badSemantics,
				})
			}
		}

		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return result, readErr
		}
	}
	return result, nil
}
