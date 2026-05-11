package amp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/entireio/external-agents/agents/entire-agent-amp/internal/protocol"
)

const prepareTranscriptTimeout = 30 * time.Second

func parseAmpThread(data []byte) (*Thread, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, errors.New("empty amp thread transcript")
	}
	if trimmed[0] != '{' {
		return nil, errors.New("amp transcript is not prepared: run prepare-transcript first")
	}
	var thread Thread
	if err := json.Unmarshal(trimmed, &thread); err != nil {
		return nil, fmt.Errorf("parse amp thread transcript: %w", err)
	}
	if thread.ID == "" {
		return nil, errors.New("prepared amp thread transcript missing id")
	}
	return &thread, nil
}

func readAmpThread(path string) (*Thread, []byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	thread, err := parseAmpThread(data)
	if err != nil {
		return nil, data, err
	}
	return thread, data, nil
}

func threadIDFromTranscriptData(data []byte) (string, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return "", errors.New("empty amp transcript")
	}

	if thread, err := parseAmpThread(trimmed); err == nil {
		return thread.ID, nil
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

	return "", errors.New("cannot prepare transcript: missing amp thread_id")
}

func (a *Agent) ReadSession(input *protocol.HookInputJSON) (protocol.AgentSessionJSON, error) {
	sessionRef := input.SessionRef
	if sessionRef == "" && input.SessionID != "" {
		sessionRef = transcriptPath(input.SessionID)
	}
	if sessionRef == "" {
		return protocol.AgentSessionJSON{}, errors.New("session_ref or session_id is required")
	}

	thread, data, err := readAmpThread(sessionRef)
	if err != nil {
		return protocol.AgentSessionJSON{}, err
	}

	startTime := thread.Created.Time()
	if startTime.IsZero() && !thread.UpdatedAt.IsZero() {
		startTime = thread.UpdatedAt.UTC()
	}
	if startTime.IsZero() {
		startTime = time.Now().UTC()
	}

	return protocol.AgentSessionJSON{
		SessionID:     thread.ID,
		AgentName:     "amp",
		RepoPath:      protocol.RepoRoot(),
		SessionRef:    sessionRef,
		StartTime:     startTime.Format(time.RFC3339),
		NativeData:    data,
		ModifiedFiles: modifiedFilesFromThread(thread),
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
	runner := a.CommandRunner
	if runner == nil {
		runner = &DefaultCommandRunner{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), prepareTranscriptTimeout)
	defer cancel()
	_, err = runner.ExportThread(ctx, threadID, sessionRef)
	return err
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
	data, err := os.ReadFile(sessionRef)
	if err != nil {
		return nil, err
	}
	if _, err := parseAmpThread(data); err != nil {
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
	thread, err := parseAmpThread(data)
	if err != nil {
		return 0, err
	}
	return len(thread.Messages), nil
}

func (a *Agent) ExtractModifiedFiles(path string, offset int) ([]string, int, error) {
	thread, _, err := readAmpThread(path)
	if err != nil {
		return nil, 0, err
	}
	return modifiedFilesFromMessages(threadMessagesFromOffset(thread, offset)), len(thread.Messages), nil
}

func (a *Agent) ExtractPrompts(sessionRef string, offset int) ([]string, error) {
	thread, _, err := readAmpThread(sessionRef)
	if err != nil {
		return nil, err
	}
	var prompts []string
	for _, msg := range threadMessagesFromOffset(thread, offset) {
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
	thread, _, err := readAmpThread(sessionRef)
	if err != nil {
		return "", false, err
	}
	for i := len(thread.Messages) - 1; i >= 0; i-- {
		msg := thread.Messages[i]
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
	thread, err := parseAmpThread(data)
	if err != nil {
		return protocol.TokenUsageResponse{}, err
	}
	var usage protocol.TokenUsageResponse
	for _, msg := range threadMessagesFromOffset(thread, offset) {
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

func threadMessagesFromOffset(thread *Thread, offset int) []ThreadMessage {
	if thread == nil || len(thread.Messages) == 0 {
		return nil
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= len(thread.Messages) {
		return nil
	}
	return thread.Messages[offset:]
}

func latestThreadModel(thread *Thread) string {
	if thread == nil {
		return ""
	}
	for i := len(thread.Messages) - 1; i >= 0; i-- {
		usage := thread.Messages[i].Usage
		if usage != nil && strings.TrimSpace(usage.Model) != "" {
			return strings.TrimSpace(usage.Model)
		}
	}
	return ""
}

func modifiedFilesFromThread(thread *Thread) []string {
	if thread == nil {
		return nil
	}
	return modifiedFilesFromMessages(thread.Messages)
}

func modifiedFilesFromMessages(messages []ThreadMessage) []string {
	seen := map[string]bool{}
	for _, msg := range messages {
		for _, file := range modifiedFilesFromMessage(msg) {
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

func modifiedFilesFromMessage(msg ThreadMessage) []string {
	var files []string
	for _, block := range msg.Content {
		files = append(files, filesFromContentBlock(block)...)
	}
	for _, input := range msg.OriginalToolUseInput {
		files = append(files, filesFromToolInput(input)...)
	}
	return files
}

func filesFromContentBlock(block ThreadContentBlock) []string {
	var files []string
	files = append(files, filesFromToolInput(block.Input)...)
	if block.Run != nil {
		files = append(files, block.Run.TrackFiles...)
		var result ThreadCommonToolResult
		if len(block.Run.Result) > 0 && json.Unmarshal(block.Run.Result, &result) == nil {
			if strings.TrimSpace(result.AbsolutePath) != "" {
				files = append(files, strings.TrimSpace(result.AbsolutePath))
			}
		}
	}
	return cleanFiles(files)
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
		file = strings.TrimSpace(file)
		if file != "" {
			out = append(out, file)
		}
	}
	return out
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
