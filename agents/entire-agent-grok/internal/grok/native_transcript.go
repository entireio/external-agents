package grok

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"sort"
	"strings"
)

type chatHistoryMessage struct {
	Type      string `json:"type"`
	Content   any    `json:"content"`
	ToolCalls []struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"tool_calls"`
}

var userQueryPattern = regexp.MustCompile(`(?s)<user_query>\s*(.*?)\s*</user_query>`)

func readChatHistoryMessages(path string) ([]chatHistoryMessage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var messages []chatHistoryMessage
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var message chatHistoryMessage
		if err := json.Unmarshal([]byte(line), &message); err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, scanner.Err()
}

func chatHistoryPosition(path string) (int, error) {
	messages, err := readChatHistoryMessages(path)
	if err != nil {
		return 0, err
	}
	return len(messages), nil
}

func promptsFromChatHistory(messages []chatHistoryMessage, offset int) []string {
	if offset < 0 {
		offset = 0
	}
	if offset > len(messages) {
		offset = len(messages)
	}
	var prompts []string
	for _, message := range messages[offset:] {
		if message.Type != "user" {
			continue
		}
		for _, text := range messageTextParts(message.Content) {
			if prompt := extractUserQuery(text); prompt != "" {
				prompts = append(prompts, prompt)
			}
		}
	}
	return prompts
}

func summaryFromChatHistory(messages []chatHistoryMessage) (string, bool) {
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		if message.Type != "assistant" {
			continue
		}
		if text, ok := assistantText(message); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text), true
		}
	}
	return "", false
}

func modifiedFilesFromChatHistory(messages []chatHistoryMessage, offset int) []string {
	if offset < 0 {
		offset = 0
	}
	if offset > len(messages) {
		offset = len(messages)
	}
	seen := map[string]struct{}{}
	for _, message := range messages[offset:] {
		if message.Type != "assistant" {
			continue
		}
		for _, call := range message.ToolCalls {
			if !isFileModificationTool(call.Name) {
				continue
			}
			for _, path := range pathsFromToolArguments(call.Name, call.Arguments) {
				if path = strings.TrimSpace(path); path != "" {
					seen[path] = struct{}{}
				}
			}
		}
	}
	files := make([]string, 0, len(seen))
	for file := range seen {
		files = append(files, file)
	}
	sort.Strings(files)
	return files
}

func messageTextParts(content any) []string {
	switch value := content.(type) {
	case string:
		if strings.TrimSpace(value) == "" {
			return nil
		}
		return []string{value}
	case []any:
		var parts []string
		for _, item := range value {
			if block, ok := item.(map[string]any); ok {
				if text, ok := block["text"].(string); ok && strings.TrimSpace(text) != "" {
					parts = append(parts, text)
				}
			}
		}
		return parts
	default:
		return nil
	}
}

func extractUserQuery(text string) string {
	if match := userQueryPattern.FindStringSubmatch(text); len(match) > 1 {
		return strings.TrimSpace(match[1])
	}
	if strings.Contains(text, "<user_info>") || strings.Contains(text, "<rules>") {
		return ""
	}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	return trimmed
}

func assistantText(message chatHistoryMessage) (string, bool) {
	switch value := message.Content.(type) {
	case string:
		return strings.TrimSpace(value), value != ""
	default:
		parts := messageTextParts(message.Content)
		if len(parts) == 0 {
			return "", false
		}
		return strings.TrimSpace(parts[0]), true
	}
}

func pathsFromToolArguments(toolName, raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var value map[string]any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return nil
	}
	var paths []string
	collectPathValues(value, &paths)
	if command, ok := value["command"].(string); ok && isShellTool(toolName) {
		paths = append(paths, pathsFromShellCommand(command)...)
	}
	return paths
}

func isNativeTranscript(path string) bool {
	return strings.HasSuffix(path, nativeTranscriptFile)
}

func readTranscriptEntries(path string) ([]sidecarRecord, error) {
	if !isNativeTranscript(path) {
		return readSidecarRecords(path)
	}
	messages, err := readChatHistoryMessages(path)
	if err != nil {
		return nil, err
	}
	records := make([]sidecarRecord, 0, len(messages))
	for _, message := range messages {
		record := sidecarRecord{Agent: AgentName}
		switch message.Type {
		case "user":
			record.Event = "UserPromptSubmit"
			for _, text := range messageTextParts(message.Content) {
				if prompt := extractUserQuery(text); prompt != "" {
					record.Prompt = prompt
					break
				}
			}
		case "assistant":
			if text, ok := assistantText(message); ok {
				record.Event = "Stop"
				record.LastAssistantMessage = text
			}
			for _, call := range message.ToolCalls {
				toolRecord := sidecarRecord{
					Agent:     AgentName,
					Event:     "PostToolUse",
					ToolName:  call.Name,
					ToolUseID: call.ID,
				}
				if len(strings.TrimSpace(call.Arguments)) > 0 {
					toolRecord.ToolInput = json.RawMessage(call.Arguments)
				}
				records = append(records, toolRecord)
			}
			if record.Event != "" {
				records = append(records, record)
			}
			continue
		default:
			continue
		}
		if record.Event != "" {
			records = append(records, record)
		}
	}
	return records, nil
}

func compactChatHistoryBytes(data []byte) ([]byte, error) {
	messages, err := readChatHistoryMessagesFromBytes(data)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	for _, message := range messages {
		switch message.Type {
		case "user":
			for _, text := range messageTextParts(message.Content) {
				if prompt := extractUserQuery(text); prompt != "" {
					if err := writeCompactLine(&buf, compactLine{
						V: 1, Agent: AgentName, CLIVersion: compactCLIVersion, Type: "user",
						Content: []compactUserTextBlock{{Text: prompt}},
					}); err != nil {
						return nil, err
					}
				}
			}
		case "assistant":
			for _, call := range message.ToolCalls {
				block := compactToolUseBlock{
					Type: "tool_use", ID: call.ID, Name: call.Name,
					Input: decodeRawObject(json.RawMessage(call.Arguments)),
				}
				if err := writeCompactLine(&buf, compactLine{
					V: 1, Agent: AgentName, CLIVersion: compactCLIVersion, Type: "assistant",
					ID: call.ID, Content: []any{block},
				}); err != nil {
					return nil, err
				}
			}
			if text, ok := assistantText(message); ok {
				if err := writeCompactLine(&buf, compactLine{
					V: 1, Agent: AgentName, CLIVersion: compactCLIVersion, Type: "assistant",
					Content: []compactAssistantTextBlock{{Type: "text", Text: text}},
				}); err != nil {
					return nil, err
				}
			}
		}
	}
	if buf.Len() == 0 {
		return nil, errors.New("compact transcript produced no output")
	}
	return buf.Bytes(), nil
}

func readChatHistoryMessagesFromBytes(data []byte) ([]chatHistoryMessage, error) {
	var messages []chatHistoryMessage
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var message chatHistoryMessage
		if err := json.Unmarshal([]byte(line), &message); err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, scanner.Err()
}