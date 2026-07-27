package omp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
)

const maxSessionLine = 10 * 1024 * 1024

type sessionEntry struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	ParentID  *string         `json:"parentId"`
	Timestamp string          `json:"timestamp"`
	Model     string          `json:"model"`
	Message   *sessionMessage `json:"message"`
}

type sessionMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	Model      string          `json:"model"`
	Usage      *sessionUsage   `json:"usage"`
	ToolCallID string          `json:"toolCallId"`
	ToolName   string          `json:"toolName"`
	IsError    bool            `json:"isError"`
}

type sessionUsage struct {
	Input  int `json:"input"`
	Output int `json:"output"`
}

type contentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type parsedEntry struct {
	Line  int
	Entry sessionEntry
}

type parsedSession struct {
	Header  sessionHeader
	Entries []parsedEntry
	Active  []parsedEntry
	Lines   int
}

func parseFullSession(data []byte) (*parsedSession, error) {
	header, err := parseSessionHeader(data)
	if err != nil {
		return nil, err
	}
	return parseSessionEntries(data, header)
}

func parseSessionEntries(data []byte, header sessionHeader) (*parsedSession, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), maxSessionLine+1)
	result := &parsedSession{Header: header}
	byID := make(map[string]parsedEntry)
	var last *parsedEntry
	for scanner.Scan() {
		result.Lines++
		if len(scanner.Bytes()) > maxSessionLine {
			return nil, fmt.Errorf("scan session: line %d exceeds %d bytes", result.Lines, maxSessionLine)
		}
		var kind struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(scanner.Bytes(), &kind) != nil {
			continue
		}
		if kind.Type == "title" || kind.Type == "session" {
			continue
		}
		var entry sessionEntry
		if json.Unmarshal(scanner.Bytes(), &entry) != nil || entry.ID == "" || entry.Type == "" {
			continue
		}
		record := parsedEntry{Line: result.Lines, Entry: entry}
		result.Entries = append(result.Entries, record)
		byID[entry.ID] = record
		recordCopy := record
		last = &recordCopy
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan session: %w", err)
	}

	seen := make(map[string]bool)
	for last != nil && !seen[last.Entry.ID] {
		seen[last.Entry.ID] = true
		result.Active = append(result.Active, *last)
		if last.Entry.ParentID == nil || *last.Entry.ParentID == "" {
			break
		}
		parent, ok := byID[*last.Entry.ParentID]
		if !ok {
			break
		}
		parentCopy := parent
		last = &parentCopy
	}
	for left, right := 0, len(result.Active)-1; left < right; left, right = left+1, right-1 {
		result.Active[left], result.Active[right] = result.Active[right], result.Active[left]
	}
	return result, nil
}

func parseSessionSlice(data []byte) (*parsedSession, error) {
	line, _, ok := nextJSONLLine(data)
	if ok {
		var kind struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(line, &kind) == nil && (kind.Type == "title" || kind.Type == "session") {
			return parseFullSession(data)
		}
	}
	return parseSessionEntries(data, sessionHeader{})
}
