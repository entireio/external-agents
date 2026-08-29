package amp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/entireio/external-agents/agents/entire-agent-amp/internal/protocol"
)

const prepareTranscriptTimeout = 30 * time.Second

// threadIDFromTranscriptData recovers the Amp thread id from a stored
// transcript. The JSONL layout has no thread header, so the id comes from the
// messages first (see ThreadMessage.ThreadID), then from any unprepared hook
// payload lines the plugin wrote before the first export.
func threadIDFromTranscriptData(data []byte) (string, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return "", errors.New("empty amp transcript")
	}

	// An unprepared transcript is not an error here: the hook payloads it
	// holds are exactly what carries the thread id we need to export with.
	if messages, err := decodeTranscript(trimmed); err == nil {
		if id := threadIDFromMessages(messages); id != "" {
			return id, nil
		}
	}

	for line := range bytes.SplitSeq(trimmed, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var payload ampHookPayload
		if err := json.Unmarshal(line, &payload); err != nil {
			continue
		}
		if strings.TrimSpace(payload.ThreadID) != "" {
			return strings.TrimSpace(payload.ThreadID), nil
		}
	}

	if trimmed[0] == '{' {
		var payload map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &payload); err == nil && len(payload) == 0 {
			return "", nil
		}
	}

	return "", errors.New("cannot prepare transcript: missing amp thread_id")
}

// threadIDFromMessages returns the first thread id stamped on a message.
func threadIDFromMessages(messages []ThreadMessage) string {
	for _, msg := range messages {
		if id := strings.TrimSpace(msg.ThreadID); id != "" {
			return id
		}
	}
	return ""
}

// startTimeFromMessages returns the first message send time, or now.
func startTimeFromMessages(messages []ThreadMessage) time.Time {
	for _, msg := range messages {
		if msg.Meta == nil {
			continue
		}
		if ts := msg.Meta.SentAt.Time(); !ts.IsZero() {
			return ts
		}
	}
	return time.Now().UTC()
}

func (a *Agent) ReadSession(input *protocol.HookInputJSON) (protocol.AgentSessionJSON, error) {
	var sessionID string
	var sessionRef string
	if input != nil {
		sessionID = input.SessionID
		sessionRef = input.SessionRef
		if sessionRef == "" && sessionID != "" {
			sessionRef = transcriptPath(sessionID)
		}
	}
	if sessionRef == "" {
		return protocol.AgentSessionJSON{}, errors.New("session_ref or session_id is required")
	}

	data, err := os.ReadFile(sessionRef)
	if err != nil {
		return protocol.AgentSessionJSON{}, err
	}
	messages, err := decodeTranscript(data)
	if err != nil {
		return protocol.AgentSessionJSON{}, err
	}
	if len(messages) == 0 {
		if sessionID == "" {
			sessionID = strings.TrimSuffix(filepath.Base(sessionRef), filepath.Ext(sessionRef))
		}
		return protocol.AgentSessionJSON{
			SessionID:     sessionID,
			AgentName:     "amp",
			RepoPath:      protocol.RepoRoot(),
			SessionRef:    sessionRef,
			StartTime:     time.Now().UTC().Format(time.RFC3339),
			NativeData:    data,
			ModifiedFiles: []string{},
			NewFiles:      []string{},
			DeletedFiles:  []string{},
		}, nil
	}

	// The JSONL layout carries no thread header, so recover the id from the
	// hook input, the messages, or the file name (in that order).
	if sessionID == "" {
		sessionID = threadIDFromMessages(messages)
	}
	if sessionID == "" {
		sessionID = strings.TrimSuffix(filepath.Base(sessionRef), filepath.Ext(sessionRef))
	}

	return protocol.AgentSessionJSON{
		SessionID:     sessionID,
		AgentName:     "amp",
		RepoPath:      protocol.RepoRoot(),
		SessionRef:    sessionRef,
		StartTime:     startTimeFromMessages(messages).Format(time.RFC3339),
		NativeData:    data,
		ModifiedFiles: modifiedFilesFromMessages(messages),
		NewFiles:      []string{},
		DeletedFiles:  []string{},
	}, nil
}

func (a *Agent) PrepareTranscript(sessionRef string) error {
	if strings.TrimSpace(sessionRef) == "" {
		return errors.New("session_ref is required")
	}
	data, err := os.ReadFile(sessionRef)
	if err != nil {
		return fmt.Errorf("read transcript for prepare: %w", err)
	}
	threadID, err := threadIDFromTranscriptData(data)
	if err != nil {
		return err
	}
	if threadID == "" {
		return nil
	}
	return a.exportThread(threadID, sessionRef)
}

func (a *Agent) WriteSession(session protocol.AgentSessionJSON) error {
	if session.SessionRef == "" {
		return errors.New("session_ref is required")
	}
	if err := os.MkdirAll(filepath.Dir(session.SessionRef), 0o700); err != nil {
		return err
	}
	return atomicWriteFile(session.SessionRef, session.NativeData, 0o600)
}

func (a *Agent) ReadTranscript(sessionRef string) ([]byte, error) {
	data, err := os.ReadFile(sessionRef)
	if err != nil {
		return nil, err
	}
	if _, err := decodeTranscript(data); err != nil {
		return nil, err
	}
	return data, nil
}

func (a *Agent) ChunkTranscript(content []byte, maxSize int) ([][]byte, error) {
	if maxSize <= 0 {
		return nil, fmt.Errorf("max-size must be positive, got %d", maxSize)
	}
	var chunks [][]byte
	for len(content) > 0 {
		end := min(maxSize, len(content))
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
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	messages, err := decodeTranscript(data)
	if err != nil {
		return 0, err
	}
	return len(messages), nil
}

func (a *Agent) ExtractModifiedFiles(path string, offset int) ([]string, int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	messages, err := decodeTranscript(data)
	if err != nil {
		return nil, 0, err
	}
	return modifiedFilesFromMessagesFromOffset(messages, offset), len(messages), nil
}

func (a *Agent) ExtractPrompts(sessionRef string, offset int) ([]string, error) {
	data, err := os.ReadFile(sessionRef)
	if err != nil {
		return nil, err
	}
	messages, err := decodeTranscript(data)
	if err != nil {
		return nil, err
	}
	var prompts []string
	for _, msg := range messagesFromOffset(messages, offset) {
		if msg.Role != ThreadMessageRoleUser {
			continue
		}
		if text := threadMessageText(msg); text != "" {
			prompts = append(prompts, text)
		}
	}
	return prompts, nil
}

func (a *Agent) ExtractSummary(sessionRef string) (string, bool, error) {
	data, err := os.ReadFile(sessionRef)
	if err != nil {
		return "", false, err
	}
	messages, err := decodeTranscript(data)
	if err != nil {
		return "", false, err
	}
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Role != ThreadMessageRoleAssistant {
			continue
		}
		if text := threadMessageText(msg); text != "" {
			return text, true, nil
		}
	}
	return "", false, nil
}

func (a *Agent) CalculateTokens(data []byte, offset int) (protocol.TokenUsageResponse, error) {
	messages, err := decodeTranscript(data)
	if err != nil {
		return protocol.TokenUsageResponse{}, err
	}
	var usage protocol.TokenUsageResponse
	for _, msg := range messagesFromOffset(messages, offset) {
		if msg.Usage == nil {
			continue
		}
		usage.InputTokens += msg.Usage.InputTokens
		usage.OutputTokens += msg.Usage.OutputTokens
		usage.CacheReadTokens += msg.Usage.CacheReadInputTokens
		usage.CacheCreationTokens += msg.Usage.CacheCreationInputTokens
		usage.APICallCount++
	}
	return usage, nil
}

// messagesFromOffset returns the messages at index >= offset. Because the
// on-disk transcript is one message per line, the offset Entire records via
// get-transcript-position (a line index) is also a message index.
func messagesFromOffset(messages []ThreadMessage, offset int) []ThreadMessage {
	if len(messages) == 0 {
		return nil
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= len(messages) {
		return nil
	}
	return messages[offset:]
}

func latestThreadModel(messages []ThreadMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		usage := messages[i].Usage
		if usage != nil && strings.TrimSpace(usage.Model) != "" {
			return strings.TrimSpace(usage.Model)
		}
	}
	return ""
}

func modifiedFilesFromMessages(messages []ThreadMessage) []string {
	return modifiedFilesFromMessagesWithContext(messages, messages)
}

func modifiedFilesFromMessagesFromOffset(messages []ThreadMessage, offset int) []string {
	return modifiedFilesFromMessagesWithContext(messagesFromOffset(messages, offset), messages)
}

func modifiedFilesFromMessagesWithContext(messages []ThreadMessage, contextMessages []ThreadMessage) []string {
	seen := map[string]bool{}
	toolNamesByID := toolNamesByIDFromMessages(contextMessages)
	for _, msg := range messages {
		for _, file := range modifiedFilesFromMessage(msg, toolNamesByID) {
			if file != "" {
				seen[file] = true
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

func toolNamesByIDFromMessages(messages []ThreadMessage) map[string]string {
	toolNamesByID := map[string]string{}
	for _, msg := range messages {
		for _, block := range msg.Content {
			if strings.TrimSpace(block.ID) != "" && strings.TrimSpace(block.Name) != "" {
				toolNamesByID[strings.TrimSpace(block.ID)] = strings.TrimSpace(block.Name)
			}
		}
	}
	return toolNamesByID
}

func modifiedFilesFromMessage(msg ThreadMessage, toolNamesByID map[string]string) []string {
	var files []string
	for _, block := range msg.Content {
		files = append(files, filesFromContentBlockForTool(block, toolNameForContentBlock(block, toolNamesByID))...)
	}
	for idOrName, input := range msg.OriginalToolUseInput {
		if isMutatingToolName(idOrName) || isMutatingToolName(toolNamesByID[strings.TrimSpace(idOrName)]) {
			files = append(files, filesFromToolInput(input)...)
		}
	}
	return files
}

func filesFromContentBlockForTool(block ThreadContentBlock, toolName string) []string {
	var files []string
	mutatingTool := isMutatingToolName(toolName)
	if mutatingTool {
		files = append(files, filesFromToolInput(block.Input)...)
	}
	if block.Run != nil {
		if mutatingTool {
			files = append(files, block.Run.TrackFiles...)
		}
		var result ThreadCommonToolResult
		if len(block.Run.Result) > 0 && json.Unmarshal(block.Run.Result, &result) == nil {
			if mutatingTool && strings.TrimSpace(result.AbsolutePath) != "" {
				files = append(files, strings.TrimSpace(result.AbsolutePath))
			}
		}
	}
	return cleanFiles(files)
}

func toolNameForContentBlock(block ThreadContentBlock, toolNamesByID map[string]string) string {
	if strings.TrimSpace(block.Name) != "" {
		return strings.TrimSpace(block.Name)
	}
	if toolNamesByID == nil || strings.TrimSpace(block.ToolUseID) == "" {
		return ""
	}
	return toolNamesByID[strings.TrimSpace(block.ToolUseID)]
}

func isMutatingToolName(name string) bool {
	switch normalizeToolName(name) {
	case "create", "create_file",
		"delete", "delete_file",
		"edit", "edit_file",
		"move", "move_file",
		"multi_edit",
		"patch",
		"rename", "rename_file",
		"replace",
		"str_replace_based_edit_tool",
		"str_replace_editor",
		"write", "write_file":
		return true
	default:
		return false
	}
}

func normalizeToolName(name string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(name)), "-", "_")
}

func filesFromToolInput(input ThreadToolInput) []string {
	var files []string
	for _, key := range []string{"path", "filePath", "filepath", "file", "absolutePath"} {
		files = append(files, stringValues(input[key])...)
	}
	for _, key := range []string{"paths", "files"} {
		files = append(files, stringValues(input[key])...)
	}
	return cleanFiles(files)
}

func stringValues(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return []string{s}
	}
	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr
	}
	return nil
}

func cleanFiles(files []string) []string {
	out := files[:0]
	for _, file := range files {
		file = cleanFile(file)
		if file != "" {
			out = append(out, file)
		}
	}
	return out
}

func cleanFile(file string) string {
	file = strings.TrimSpace(file)
	if file == "" {
		return ""
	}
	if u, err := url.Parse(file); err == nil && u.Scheme == "file" {
		file = u.Path
		if unescaped, err := url.PathUnescape(file); err == nil {
			file = unescaped
		}
	}
	return file
}

func threadMessageText(msg ThreadMessage) string {
	var parts []string
	for _, block := range msg.Content {
		if block.Type == ThreadContentText && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func threadMessageIDString(id ThreadMessageID) string {
	return string(id)
}

func threadMessageTimestamp(msg ThreadMessage) string {
	if msg.Meta == nil {
		return ""
	}
	if ts := msg.Meta.SentAt.Time(); !ts.IsZero() {
		return ts.Format(time.RFC3339)
	}
	return ""
}
