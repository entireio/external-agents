package pi

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/entireio/external-agents/agents/entire-agent-pi/internal/protocol"
)

// Pi JSONL entry types
const entryTypeMessage = "message"

// maxScannerLine is 1 MB — large enough for Pi JSONL lines that may
// contain thinking blocks or full file contents in tool call arguments.
const maxScannerLine = 1 << 20

func newJSONLScanner(data []byte) *bufio.Scanner {
	s := bufio.NewScanner(bytes.NewReader(data))
	s.Buffer(make([]byte, 0, maxScannerLine), maxScannerLine)
	return s
}

// countLines returns the number of non-empty lines in data.
func countLines(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	n := bytes.Count(data, []byte{'\n'})
	// Count final line if it doesn't end with newline
	if len(data) > 0 && data[len(data)-1] != '\n' {
		n++
	}
	return n
}

// skipLines returns data with the first n lines removed.
func skipLines(data []byte, n int) []byte {
	if n <= 0 {
		return data
	}
	off := 0
	for i := 0; i < n && off < len(data); i++ {
		idx := bytes.IndexByte(data[off:], '\n')
		if idx < 0 {
			return nil // fewer lines than offset
		}
		off += idx + 1
	}
	return data[off:]
}

type sessionEntry struct {
	Type      string `json:"type"`
	Version   int    `json:"version"`
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	Cwd       string `json:"cwd"`
}

type messageEntry struct {
	Type      string  `json:"type"`
	ID        string  `json:"id"`
	Timestamp string  `json:"timestamp"`
	Message   message `json:"message"`
}

type message struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	Timestamp  json.Number     `json:"timestamp"`
	Usage      *tokenUsage     `json:"usage,omitempty"`
	StopReason string          `json:"stopReason,omitempty"`
	ToolCallID string          `json:"toolCallId,omitempty"`
	ToolName   string          `json:"toolName,omitempty"`
	IsError    bool            `json:"isError,omitempty"`
}

type tokenUsage struct {
	Input      int `json:"input"`
	Output     int `json:"output"`
	CacheRead  int `json:"cacheRead"`
	CacheWrite int `json:"cacheWrite"`
}

type contentItem struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Name      string          `json:"name,omitempty"`
	ID        string          `json:"id,omitempty"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// resolveActiveBranch parses JSONL data and returns the set of entry IDs on
// the active conversation branch. Pi transcripts form a tree (entries have id
// and parentId); the active branch is the path from the root to the last
// message entry in the file.
// Returns nil if the transcript has no tree structure or cannot be resolved.
func resolveActiveBranch(data []byte) map[string]bool {
	type node struct {
		Type     string  `json:"type"`
		ID       string  `json:"id"`
		ParentID *string `json:"parentId"`
	}

	var lastMessageID string
	hasTree := false
	parentOf := make(map[string]string)

	scanner := newJSONLScanner(data)
	for scanner.Scan() {
		var n node
		if err := json.Unmarshal(scanner.Bytes(), &n); err != nil || n.ID == "" {
			continue
		}
		if n.ParentID != nil {
			parentOf[n.ID] = *n.ParentID
			if *n.ParentID != "" {
				hasTree = true
			}
		}
		if n.Type == entryTypeMessage {
			lastMessageID = n.ID
		}
	}

	// No tree references — all entries are on the active branch.
	if !hasTree || lastMessageID == "" {
		return nil
	}

	active := make(map[string]bool)
	for cur := lastMessageID; cur != ""; {
		if active[cur] {
			break // cycle protection
		}
		active[cur] = true
		parent, ok := parentOf[cur]
		if !ok {
			break
		}
		cur = parent
	}

	return active
}

func (a *Agent) ReadSession(input *protocol.HookInputJSON) (protocol.AgentSessionJSON, error) {
	sessionRef := input.SessionRef
	if sessionRef == "" {
		return protocol.AgentSessionJSON{}, errors.New("session_ref is required")
	}

	data, err := os.ReadFile(sessionRef)
	if err != nil {
		return protocol.AgentSessionJSON{}, fmt.Errorf("read session file: %w", err)
	}

	sessionID := input.SessionID
	if sessionID == "" {
		sessionID = extractSessionIDFromPath(sessionRef)
	}

	var startTime string
	scanner := newJSONLScanner(data)
	for scanner.Scan() {
		var entry sessionEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		if entry.Type == "session" {
			startTime = entry.Timestamp
			if sessionID == "" {
				sessionID = entry.ID
			}
			break
		}
	}

	return protocol.AgentSessionJSON{
		SessionID:     sessionID,
		AgentName:     "pi",
		RepoPath:      protocol.RepoRoot(),
		SessionRef:    sessionRef,
		StartTime:     startTime,
		NativeData:    data,
		ModifiedFiles: []string{},
		NewFiles:      []string{},
		DeletedFiles:  []string{},
	}, nil
}

func (a *Agent) WriteSession(session protocol.AgentSessionJSON) error {
	if session.SessionRef == "" {
		return errors.New("session_ref is required")
	}
	if err := os.MkdirAll(filepath.Dir(session.SessionRef), 0o700); err != nil {
		return err
	}
	return os.WriteFile(session.SessionRef, session.NativeData, 0o600)
}

func (a *Agent) ReadTranscript(sessionRef string) ([]byte, error) {
	return os.ReadFile(sessionRef)
}

func (a *Agent) ChunkTranscript(content []byte, maxSize int) ([][]byte, error) {
	if maxSize <= 0 {
		return nil, fmt.Errorf("max-size must be positive, got %d", maxSize)
	}
	var chunks [][]byte
	for len(content) > 0 {
		end := maxSize
		if end > len(content) {
			end = len(content)
		}
		chunks = append(chunks, content[:end])
		content = content[end:]
	}
	return chunks, nil
}

func (a *Agent) ReassembleTranscript(chunks [][]byte) ([]byte, error) {
	var data []byte
	for _, chunk := range chunks {
		data = append(data, chunk...)
	}
	return data, nil
}

// GetTranscriptPosition returns the number of lines in the JSONL transcript.
// The CLI uses this value as the offset for ExtractModifiedFiles, so units
// must match: both use line count (consistent with Claude Code).
func (a *Agent) GetTranscriptPosition(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	return countLines(data), nil
}

func (a *Agent) ExtractModifiedFiles(path string, offset int) ([]string, int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}

	totalLines := countLines(data)
	active := resolveActiveBranch(data)

	content := skipLines(data, offset)

	seen := make(map[string]bool)
	var files []string

	scanner := newJSONLScanner(content)
	for scanner.Scan() {
		var entry messageEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		if entry.Type != entryTypeMessage {
			continue
		}
		if active != nil && !active[entry.ID] {
			continue
		}
		if entry.Message.Role != "assistant" {
			continue
		}

		var items []contentItem
		if err := json.Unmarshal(entry.Message.Content, &items); err != nil {
			continue
		}

		for _, item := range items {
			if item.Type != "toolCall" {
				continue
			}
			if item.Name != "write" && item.Name != "edit" {
				continue
			}
			var args struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal(item.Arguments, &args); err != nil {
				continue
			}
			if args.Path != "" && !seen[args.Path] {
				seen[args.Path] = true
				files = append(files, args.Path)
			}
		}
	}

	return files, totalLines, nil
}

func (a *Agent) ExtractPrompts(sessionRef string, offset int) ([]string, error) {
	data, err := os.ReadFile(sessionRef)
	if err != nil {
		return nil, err
	}

	active := resolveActiveBranch(data)

	content := skipLines(data, offset)

	var prompts []string
	scanner := newJSONLScanner(content)
	for scanner.Scan() {
		var entry messageEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		if entry.Type != entryTypeMessage || entry.Message.Role != "user" {
			continue
		}
		if active != nil && !active[entry.ID] {
			continue
		}

		var items []contentItem
		if err := json.Unmarshal(entry.Message.Content, &items); err != nil {
			continue
		}

		for _, item := range items {
			if item.Type == contentTypeText && item.Text != "" {
				prompts = append(prompts, item.Text)
			}
		}
	}

	return prompts, nil
}

func (a *Agent) ExtractSummary(sessionRef string) (string, bool, error) {
	data, err := os.ReadFile(sessionRef)
	if err != nil {
		return "", false, err
	}

	active := resolveActiveBranch(data)

	var lastAssistantText string
	scanner := newJSONLScanner(data)
	for scanner.Scan() {
		var entry messageEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		if entry.Type != entryTypeMessage || entry.Message.Role != "assistant" {
			continue
		}
		if active != nil && !active[entry.ID] {
			continue
		}

		var items []contentItem
		if err := json.Unmarshal(entry.Message.Content, &items); err != nil {
			continue
		}

		for _, item := range items {
			if item.Type == contentTypeText && item.Text != "" {
				lastAssistantText = item.Text
			}
		}
	}

	if lastAssistantText != "" {
		return lastAssistantText, true, nil
	}
	return "", false, nil
}

func (a *Agent) CalculateTokens(data []byte, offset int) (protocol.TokenUsageResponse, error) {
	active := resolveActiveBranch(data)

	content := skipLines(data, offset)

	var result protocol.TokenUsageResponse
	scanner := newJSONLScanner(content)
	for scanner.Scan() {
		var entry messageEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		if entry.Type != entryTypeMessage || entry.Message.Role != "assistant" || entry.Message.Usage == nil {
			continue
		}
		if active != nil && !active[entry.ID] {
			continue
		}

		result.InputTokens += entry.Message.Usage.Input
		result.OutputTokens += entry.Message.Usage.Output
		result.CacheReadTokens += entry.Message.Usage.CacheRead
		result.CacheCreationTokens += entry.Message.Usage.CacheWrite
		result.APICallCount++
	}

	return result, nil
}
