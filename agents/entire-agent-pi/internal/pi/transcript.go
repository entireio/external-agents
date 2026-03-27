package pi

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/entireio/external-agents/agents/entire-agent-pi/internal/protocol"
)

// Pi JSONL entry types
type sessionEntry struct {
	Type      string `json:"type"`
	Version   int    `json:"version"`
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	Cwd       string `json:"cwd"`
}

type messageEntry struct {
	Type    string  `json:"type"`
	ID      string  `json:"id"`
	Message message `json:"message"`
}

type message struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	Timestamp  json.Number     `json:"timestamp"`
	Usage      *tokenUsage     `json:"usage,omitempty"`
	StopReason string          `json:"stopReason,omitempty"`
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
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
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

func (a *Agent) GetTranscriptPosition(path string) (int, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	return int(info.Size()), nil
}

func (a *Agent) ExtractModifiedFiles(path string, offset int) ([]string, int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}

	content := data
	if offset > 0 && offset <= len(data) {
		content = data[offset:]
	} else if offset > len(data) {
		content = nil
	}

	seen := make(map[string]bool)
	var files []string

	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	for scanner.Scan() {
		var entry messageEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		if entry.Type != "message" {
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

	return files, len(data), nil
}

func (a *Agent) ExtractPrompts(sessionRef string, offset int) ([]string, error) {
	data, err := os.ReadFile(sessionRef)
	if err != nil {
		return nil, err
	}

	content := data
	if offset > 0 && offset <= len(data) {
		content = data[offset:]
	}

	var prompts []string
	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	for scanner.Scan() {
		var entry messageEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		if entry.Type != "message" || entry.Message.Role != "user" {
			continue
		}

		var items []contentItem
		if err := json.Unmarshal(entry.Message.Content, &items); err != nil {
			continue
		}

		for _, item := range items {
			if item.Type == "text" && item.Text != "" {
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

	var lastAssistantText string
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		var entry messageEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		if entry.Type != "message" || entry.Message.Role != "assistant" {
			continue
		}

		var items []contentItem
		if err := json.Unmarshal(entry.Message.Content, &items); err != nil {
			continue
		}

		for _, item := range items {
			if item.Type == "text" && item.Text != "" {
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
	content := data
	if offset > 0 && offset < len(data) {
		content = data[offset:]
	}

	var result protocol.TokenUsageResponse
	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	for scanner.Scan() {
		var entry messageEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		if entry.Type != "message" || entry.Message.Role != "assistant" || entry.Message.Usage == nil {
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
