package claude

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/seanochang/llm-usage-dashboard/internal/model"
	"github.com/seanochang/llm-usage-dashboard/internal/source"
)

// Two-thirds of transcript lines do not contain usage, and those lines carry
// most message/tool text. This prefilter avoids decoding the expensive lines.
var usageMarker = []byte(`"usage":{`)

type Source struct {
	root string
}

func New(root string) *Source { return &Source{root: root} }

func (s *Source) Name() model.Tool { return model.ToolClaude }

func (s *Source) Discover() ([]source.FileRef, error) {
	var files []source.FileRef
	if err := discoverUnsorted(s.root, true, &files); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return files, nil
}

// Python's glob oracle traverses os.scandir order. os.ReadDir and WalkDir sort
// entries, which can choose a different variant when a request ID appears in
// more than one transcript. File.Readdir preserves filesystem order.
func discoverUnsorted(dir string, root bool, files *[]source.FileRef) error {
	handle, err := os.Open(dir)
	if err != nil {
		if root {
			return err
		}
		return nil
	}
	entries, err := handle.Readdir(-1)
	closeErr := handle.Close()
	if err != nil {
		if root {
			return err
		}
		return nil
	}
	if closeErr != nil && root {
		return closeErr
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if entry.IsDir() {
			if err := discoverUnsorted(path, false, files); err != nil {
				return err
			}
			continue
		}
		if !strings.HasSuffix(path, ".jsonl") {
			continue
		}
		*files = append(*files, source.FileRef{
			Path: path, Size: entry.Size(), Mtime: entry.ModTime().Unix(),
		})
	}
	return nil
}

type entry struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	SessionID string `json:"sessionId"`
	Cwd       string `json:"cwd"`
	RequestID string `json:"requestId"`
	Message   struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage *struct {
			InputTokens              int64 `json:"input_tokens"`
			OutputTokens             int64 `json:"output_tokens"`
			CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
			OutputTokensDetails      struct {
				ThinkingTokens int64 `json:"thinking_tokens"`
			} `json:"output_tokens_details"`
			CacheCreation *struct {
				Ephemeral5m int64 `json:"ephemeral_5m_input_tokens"`
				Ephemeral1h int64 `json:"ephemeral_1h_input_tokens"`
			} `json:"cache_creation"`
		} `json:"usage"`
	} `json:"message"`
}

type agentMetadata struct {
	SpawnDepth int    `json:"spawnDepth"`
	Model      string `json:"model"`
}

// attribution derives stable IDs from paths such as
// .../subagents/workflows/<workflow>/agent-<id>.jsonl.
func attribution(path string) (agent, workflow string) {
	path = filepath.ToSlash(path)
	if !strings.Contains(path, "/subagents/") {
		return "", ""
	}
	agent = strings.TrimSuffix(filepath.Base(path), ".jsonl")
	if before, after, ok := strings.Cut(path, "/workflows/"); ok && before != "" {
		if i := strings.IndexByte(after, '/'); i >= 0 {
			workflow = after[:i]
		}
	}
	return agent, workflow
}

func readAgentMetadata(path string) agentMetadata {
	metaPath := strings.TrimSuffix(path, ".jsonl") + ".meta.json"
	b, err := os.ReadFile(metaPath)
	if err != nil {
		return agentMetadata{}
	}
	var metadata agentMetadata
	_ = json.Unmarshal(b, &metadata)
	return metadata
}

func (s *Source) Parse(file source.FileRef, from int64) (source.Result, error) {
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

	agent, workflow := attribution(file.Path)
	metadata := readAgentMetadata(file.Path)
	result := source.Result{Offset: from}
	seen := make(map[string]struct{})
	reader := bufio.NewReaderSize(fh, 64*1024)
	for {
		raw, readErr := reader.ReadBytes('\n')
		if len(raw) == 0 && errors.Is(readErr, io.EOF) {
			break
		}
		complete := len(raw) > 0 && raw[len(raw)-1] == '\n'
		line := bytes.TrimSuffix(raw, []byte{'\n'})
		line = bytes.TrimSuffix(line, []byte{'\r'})
		// Active transcript writes can expose a partial final JSON object. Keep
		// the cursor at its start so the completed line is retried next run.
		if !complete && !json.Valid(line) {
			if readErr != nil && !errors.Is(readErr, io.EOF) {
				return result, readErr
			}
			break
		}
		result.Offset += int64(len(raw))

		if !bytes.Contains(line, usageMarker) {
			if readErr != nil {
				if errors.Is(readErr, io.EOF) {
					break
				}
				return result, readErr
			}
			continue
		}
		result.Candidates++
		var parsed entry
		if err := json.Unmarshal(line, &parsed); err != nil {
			result.Malformed++
			if readErr != nil {
				if errors.Is(readErr, io.EOF) {
					break
				}
				return result, readErr
			}
			continue
		}
		if parsed.Message.Usage == nil {
			if readErr != nil {
				if errors.Is(readErr, io.EOF) {
					break
				}
				return result, readErr
			}
			continue
		}
		id := parsed.RequestID
		if id == "" {
			id = parsed.Message.ID
		}
		if id == "" {
			result.Malformed++
			if readErr != nil {
				if errors.Is(readErr, io.EOF) {
					break
				}
				return result, readErr
			}
			continue
		}
		if _, duplicate := seen[id]; duplicate {
			result.Duplicates++
			if readErr != nil {
				if errors.Is(readErr, io.EOF) {
					break
				}
				return result, readErr
			}
			continue
		}
		seen[id] = struct{}{}

		ts, err := time.Parse(time.RFC3339Nano, parsed.Timestamp)
		if err != nil {
			ts = time.Unix(file.Mtime, 0)
		}
		usage := parsed.Message.Usage
		write5m, write1h := int64(0), int64(0)
		if usage.CacheCreation != nil {
			write5m = usage.CacheCreation.Ephemeral5m
			write1h = usage.CacheCreation.Ephemeral1h
		}
		if write5m == 0 && write1h == 0 {
			write5m = usage.CacheCreationInputTokens
		}
		modelID := parsed.Message.Model
		if modelID == "" {
			modelID = metadata.Model
		}
		if modelID == "" {
			modelID = "<unknown>"
		}
		result.Records = append(result.Records, model.Record{
			ID: id, Tool: model.ToolClaude, TS: ts,
			Model: model.NormalizeModel(modelID), Project: parsed.Cwd,
			Session: parsed.SessionID, Agent: agent, Workflow: workflow,
			Depth: metadata.SpawnDepth, InputTok: usage.InputTokens,
			OutputTok:    usage.OutputTokens,
			ThinkingTok:  usage.OutputTokensDetails.ThinkingTokens,
			CacheReadTok: usage.CacheReadInputTokens,
			CacheWrite5m: write5m, CacheWrite1h: write1h,
		})
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return result, readErr
		}
	}
	return result, nil
}
