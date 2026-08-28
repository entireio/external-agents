package grok

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
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
	// PromptIndex is set by Grok only on genuine user prompts. Synthetic user
	// messages (system reminders, injected context) never carry it, so its
	// presence is the reliable signal for "the human typed this".
	PromptIndex *int `json:"prompt_index"`
	// SyntheticReason is set on Grok-injected user messages, e.g. "system_reminder".
	SyntheticReason string `json:"synthetic_reason"`
	// ToolCallID links a tool_result line back to the tool_use that produced it.
	ToolCallID string `json:"tool_call_id"`
	// Summary carries the human-readable reasoning summary on reasoning lines.
	Summary any `json:"summary"`
	// ModelID is stamped on assistant and reasoning lines.
	ModelID string `json:"model_id"`
}

// lastModelID returns the most recent model stamped on the transcript. Grok
// rejects a restored session whose summary.json has no current_model_id.
func lastModelID(messages []chatHistoryMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if id := strings.TrimSpace(messages[i].ModelID); id != "" {
			return id
		}
	}
	return ""
}

var userQueryPattern = regexp.MustCompile(`(?s)<user_query>\s*(.*?)\s*</user_query>`)

func readChatHistoryMessages(path string) ([]chatHistoryMessage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	messages, err := scanChatHistory(f)
	if err != nil {
		return nil, err
	}
	return messages, nil
}

// scanChatHistory parses a chat_history.jsonl stream leniently.
//
// Entire scopes a checkpoint by slicing the transcript from a mid-session
// offset, which can cut the first line in half, and it reads the file while
// Grok is still appending to it, which can leave a half-flushed final line.
// Neither case is corruption, so an unparseable line is skipped rather than
// failing the whole parse. Erroring here surfaces to the user as the summary
// falling back to raw bytes and reporting "no content to summarize".
func scanChatHistory(r io.Reader) ([]chatHistoryMessage, error) {
	var messages []chatHistoryMessage
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var message chatHistoryMessage
		if err := json.Unmarshal([]byte(line), &message); err != nil {
			continue
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
	scoped := messages[offset:]
	marked := hasPromptIndex(scoped)
	var prompts []string
	for _, message := range scoped {
		if !isPromptCandidate(message, marked) {
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

// hasPromptIndex reports whether Grok tagged any user line with prompt_index.
// Versions that emit the tag let us identify real prompts exactly; for any that
// do not, callers fall back to the <user_query> heuristic.
func hasPromptIndex(messages []chatHistoryMessage) bool {
	for _, message := range messages {
		if message.Type == "user" && message.PromptIndex != nil {
			return true
		}
	}
	return false
}

func isPromptCandidate(message chatHistoryMessage, marked bool) bool {
	if message.Type != "user" || message.SyntheticReason != "" {
		return false
	}
	if marked {
		return message.PromptIndex != nil
	}
	return true
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

// unwrapUserQuery strips the <user_query> wrapper Grok puts around the prompt it
// hands to UserPromptSubmit hooks. Passing it through verbatim leaves the markup
// in checkpoint titles, and renders the checkpoint Intent as literally
// "<user_query>". Text without the wrapper is returned trimmed.
func unwrapUserQuery(text string) string {
	if match := userQueryPattern.FindStringSubmatch(text); len(match) > 1 {
		return strings.TrimSpace(match[1])
	}
	return strings.TrimSpace(text)
}

func extractUserQuery(text string) string {
	if match := userQueryPattern.FindStringSubmatch(text); len(match) > 1 {
		return strings.TrimSpace(match[1])
	}
	// Grok injects context into the user role. Without prompt_index to go on,
	// drop the wrappers it uses so they don't surface as user prompts.
	for _, marker := range []string{"<user_info>", "<rules>", "<system-reminder>"} {
		if strings.Contains(text, marker) {
			return ""
		}
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
	marked := hasPromptIndex(messages)
	records := make([]sidecarRecord, 0, len(messages))
	for _, message := range messages {
		record := sidecarRecord{Agent: AgentName}
		switch message.Type {
		case "user":
			if !isPromptCandidate(message, marked) {
				continue
			}
			record.Event = "UserPromptSubmit"
			for _, text := range messageTextParts(message.Content) {
				if prompt := extractUserQuery(text); prompt != "" {
					record.Prompt = prompt
					break
				}
			}
			if record.Prompt == "" {
				continue
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
	marked := hasPromptIndex(messages)
	results := toolResultsByCallID(messages)
	consumed := make(map[string]bool, len(results))
	var buf bytes.Buffer
	for _, message := range messages {
		switch message.Type {
		case "tool_result":
			// A checkpoint slice can start after the assistant line that issued
			// the call, orphaning its result. Emit it standalone so the tool
			// output still reaches the summarizer.
			result, ok := results[message.ToolCallID]
			if !ok || consumed[message.ToolCallID] {
				continue
			}
			consumed[message.ToolCallID] = true
			if err := writeCompactLine(&buf, compactLine{
				V: 1, Agent: AgentName, CLIVersion: compactCLIVersion, Type: "assistant",
				ID: message.ToolCallID,
				Content: []any{compactToolUseBlock{
					Type: "tool_use", ID: message.ToolCallID, Name: "unknown", Result: result,
				}},
			}); err != nil {
				return nil, err
			}
		case "user":
			if !isPromptCandidate(message, marked) {
				continue
			}
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
		case "reasoning":
			// Keep the plaintext reasoning summary. encrypted_content is
			// deliberately dropped: it is opaque to a summarizer and is the
			// field redaction mangles.
			text := reasoningSummaryText(message.Summary)
			if text == "" {
				continue
			}
			if err := writeCompactLine(&buf, compactLine{
				V: 1, Agent: AgentName, CLIVersion: compactCLIVersion, Type: "assistant",
				Content: []compactAssistantTextBlock{{Type: "thinking", Text: text}},
			}); err != nil {
				return nil, err
			}
		case "assistant":
			for _, call := range message.ToolCalls {
				block := compactToolUseBlock{
					Type: "tool_use", ID: call.ID, Name: call.Name,
					Input: decodeRawObject(json.RawMessage(call.Arguments)),
				}
				if result, ok := results[call.ID]; ok {
					block.Result = result
					consumed[call.ID] = true
				}
				if err := writeCompactLine(&buf, compactLine{
					V: 1, Agent: AgentName, CLIVersion: compactCLIVersion, Type: "assistant",
					ID: call.ID, Content: []any{block},
				}); err != nil {
					return nil, err
				}
			}
			if text, ok := assistantText(message); ok && text != "" {
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
	return scanChatHistory(bytes.NewReader(data))
}

// toolResultsByCallID indexes tool_result lines so each tool_use block can carry
// its output. Grok writes results as separate lines keyed by tool_call_id; without
// this the compacted transcript shows every call with no result.
func toolResultsByCallID(messages []chatHistoryMessage) map[string]*compactToolResult {
	results := make(map[string]*compactToolResult)
	for _, message := range messages {
		if message.Type != "tool_result" || message.ToolCallID == "" {
			continue
		}
		parts := messageTextParts(message.Content)
		if len(parts) == 0 {
			if text, ok := message.Content.(string); ok && strings.TrimSpace(text) != "" {
				parts = []string{text}
			}
		}
		results[message.ToolCallID] = &compactToolResult{
			Output: strings.Join(parts, "\n"),
			Status: "success",
		}
	}
	return results
}

// reasoningSummaryText pulls plain text out of a reasoning line's summary, which
// Grok writes as a list of {type: summary_text, text: ...} blocks.
func reasoningSummaryText(summary any) string {
	switch value := summary.(type) {
	case string:
		return strings.TrimSpace(value)
	case []any:
		var parts []string
		for _, item := range value {
			block, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if text, ok := block["text"].(string); ok && strings.TrimSpace(text) != "" {
				parts = append(parts, strings.TrimSpace(text))
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}
