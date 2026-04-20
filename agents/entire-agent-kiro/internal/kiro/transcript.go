package kiro

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/entireio/external-agents/agents/entire-agent-kiro/internal/protocol"
)

var runSQLiteCommand = func(args ...string) ([]byte, error) {
	return exec.Command("sqlite3", args...).Output()
}

var kiroFileModificationTools = map[string]struct{}{
	"fs_write": {},
	"fs_edit":  {},
}

const (
	execLogsSubdir    = "414d1636299d2b9e4ce7e17fb11f63e9"
	execIndexFilename = "f62de366d0006e17ea00a01f6624aabf"
)

// execActionToToolName maps Kiro IDE execution log action types to the CLI
// transcript tool names used for file modification tracking.
var execActionToToolName = map[string]string{
	"create": "fs_write",
	"edit":   "fs_edit",
}

type ideSessionIndexEntry struct {
	SessionID   string `json:"sessionId"`
	Title       string `json:"title"`
	DateCreated string `json:"dateCreated"`
}

func (a *Agent) ReadTranscript(sessionRef string) ([]byte, error) {
	return os.ReadFile(sessionRef)
}

func (a *Agent) ChunkTranscript(content []byte, maxSize int) ([][]byte, error) {
	if maxSize <= 0 {
		return nil, errors.New("max-size must be greater than zero")
	}
	if len(content) == 0 {
		return [][]byte{{}}, nil
	}

	var chunks [][]byte
	for start := 0; start < len(content); start += maxSize {
		end := start + maxSize
		if end > len(content) {
			end = len(content)
		}
		chunk := make([]byte, end-start)
		copy(chunk, content[start:end])
		chunks = append(chunks, chunk)
	}
	return chunks, nil
}

func (a *Agent) ReassembleTranscript(chunks [][]byte) ([]byte, error) {
	return bytes.Join(chunks, nil), nil
}

func (a *Agent) querySessionID(cwd string) (string, error) {
	db, err := kiroCLIDataDBPath()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(db); err != nil {
		return "", fmt.Errorf("kiro database not found at %s: %w", db, err)
	}

	query := fmt.Sprintf(
		"SELECT json_extract(value, '$.conversation_id') FROM conversations_v2 WHERE key = '%s' ORDER BY updated_at DESC LIMIT 1",
		escapeSQLString(cwd),
	)

	out, err := runSQLiteCommand("-json", db, query)
	if err != nil {
		return "", fmt.Errorf("sqlite3 query failed: %w", err)
	}

	result := strings.TrimSpace(string(out))
	if result == "" || result == "[]" {
		return "", nil
	}

	var rows []map[string]string
	if err := json.Unmarshal([]byte(result), &rows); err != nil {
		return "", fmt.Errorf("failed to parse sqlite3 output: %w", err)
	}
	if len(rows) == 0 {
		return "", nil
	}
	for _, value := range rows[0] {
		return value, nil
	}
	return "", nil
}

func (a *Agent) ensureCachedTranscript(cwd string, sessionID string, conversationID string) (string, error) {
	db, err := kiroCLIDataDBPath()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(db); err != nil {
		return "", fmt.Errorf("kiro database not found at %s: %w", db, err)
	}

	var query string
	if conversationID != "" {
		query = fmt.Sprintf(
			"SELECT value FROM conversations_v2 WHERE json_extract(value, '$.conversation_id') = '%s' ORDER BY updated_at DESC LIMIT 1",
			escapeSQLString(conversationID),
		)
	} else {
		query = fmt.Sprintf(
			"SELECT value FROM conversations_v2 WHERE key = '%s' ORDER BY updated_at DESC LIMIT 1",
			escapeSQLString(cwd),
		)
	}

	out, err := runSQLiteCommand(db, query)
	if err != nil {
		return "", fmt.Errorf("sqlite3 transcript query failed: %w", err)
	}

	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return "", errors.New("no transcript found")
	}

	cachePath, err := a.cacheTranscriptPath(cwd, sessionID)
	if err != nil {
		return "", err
	}

	data := []byte(raw)
	if filtered, ok := a.trimTranscriptHistory([]byte(raw)); ok {
		data = filtered
	}

	// If the transcript has no history (e.g., offset trimmed everything),
	// return error so captureTranscriptForStop can try other sources (IDE).
	if parsed, parseErr := parseTranscript(data); parseErr == nil && len(parsed.History) == 0 {
		return "", errors.New("CLI transcript has no history entries")
	}

	if err := os.WriteFile(cachePath, data, 0o600); err != nil {
		return "", fmt.Errorf("failed to write cached transcript: %w", err)
	}
	return cachePath, nil
}

type transcriptOffset struct {
	ConversationID string `json:"conversation_id"`
	Position       int    `json:"position"`
}

func (a *Agent) transcriptOffsetPath() string {
	return filepath.Join(protocol.RepoRoot(), ".entire", "tmp", "kiro-transcript-offset.json")
}

func readTranscriptOffset(path string) transcriptOffset {
	data, err := os.ReadFile(path)
	if err != nil {
		return transcriptOffset{}
	}
	var offset transcriptOffset
	if err := json.Unmarshal(data, &offset); err != nil {
		return transcriptOffset{}
	}
	return offset
}

func writeTranscriptOffset(path string, offset transcriptOffset) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(offset)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// trimTranscriptHistory trims already-checkpointed entries from a cumulative
// kiro transcript using a stored offset. Returns the re-serialized trimmed
// JSON and true if trimming was applied, or (nil, false) if the full
// transcript should be used as-is.
func (a *Agent) trimTranscriptHistory(raw []byte) ([]byte, bool) {
	parsed, err := parseTranscript(raw)
	if err != nil || len(parsed.History) == 0 {
		return nil, false
	}

	offsetPath := a.transcriptOffsetPath()
	prev := readTranscriptOffset(offsetPath)
	totalLen := len(parsed.History)

	trimFrom := 0
	if prev.ConversationID == parsed.ConversationID && prev.Position > 0 && prev.Position <= totalLen {
		trimFrom = prev.Position
	}

	// Only update offset when there are new entries to capture.
	if trimFrom < totalLen {
		_ = writeTranscriptOffset(offsetPath, transcriptOffset{
			ConversationID: parsed.ConversationID,
			Position:       totalLen,
		})
	}

	if trimFrom == 0 || trimFrom >= totalLen {
		// No trimming needed (first capture) or no new entries since last
		// capture. Return full transcript so the caller always has content —
		// an empty transcript would cause the CLI finalize step to fail.
		return nil, false
	}

	parsed.History = parsed.History[trimFrom:]
	filtered, err := json.Marshal(parsed)
	if err != nil {
		return nil, false
	}
	return filtered, true
}

func (a *Agent) ensureIDETranscript(cwd string, sessionID string) (string, error) {
	sessionsDir, err := ideWorkspaceSessionsDir(cwd)
	if err != nil {
		return "", err
	}

	indexPath := filepath.Join(sessionsDir, "sessions.json")
	indexData, err := os.ReadFile(indexPath)
	if err != nil {
		return "", fmt.Errorf("IDE sessions.json not found: %w", err)
	}

	var sessions []ideSessionIndexEntry
	if err := json.Unmarshal(indexData, &sessions); err != nil {
		return "", fmt.Errorf("failed to parse IDE sessions.json: %w", err)
	}
	if len(sessions) == 0 {
		return "", errors.New("no IDE sessions found")
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].DateCreated > sessions[j].DateCreated
	})

	ideSessionID := sessions[0].SessionID
	if !filepath.IsLocal(ideSessionID + ".json") {
		return "", fmt.Errorf("invalid session ID: %q", ideSessionID)
	}

	transcriptPath := filepath.Join(sessionsDir, ideSessionID+".json")
	transcriptData, err := os.ReadFile(transcriptPath)
	if err != nil {
		return "", fmt.Errorf("failed to read IDE transcript %s: %w", transcriptPath, err)
	}

	// Enrich the IDE transcript with actual agent responses and tool calls.
	// The IDE session format only stores "On it." as assistant content.
	//
	// Priority 1: Read Kiro IDE execution logs — these contain the full
	// action trace (tool calls, responses, file modifications).
	// Priority 2: Merge tool calls from post-tool-use hook JSONL file.
	data := transcriptData
	enriched := false
	if chatSessionID := extractIDESessionID(transcriptData); chatSessionID != "" {
		if execLogs, err := findExecutionLogsForSession(chatSessionID); err == nil && len(execLogs) > 0 {
			if result := enrichIDETranscriptWithExecutionLogs(transcriptData, execLogs); result != nil {
				data = result
				enriched = true
			}
		}
	}
	if !enriched {
		if toolCalls := a.readAndClearToolCalls(); len(toolCalls) > 0 {
			if result := enrichIDETranscriptWithToolCalls(transcriptData, toolCalls); result != nil {
				data = result
			}
		}
	}

	// Trim already-checkpointed entries from cumulative IDE transcript.
	if filtered, ok := a.trimTranscriptHistory(data); ok {
		data = filtered
	}

	if parsed, parseErr := parseTranscript(data); parseErr == nil && len(parsed.History) == 0 {
		return "", errors.New("IDE transcript has no history entries after trimming")
	}

	cachePath, err := a.cacheTranscriptPath(cwd, sessionID)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(cachePath, data, 0o600); err != nil {
		return "", fmt.Errorf("failed to write cached IDE transcript: %w", err)
	}
	return cachePath, nil
}

// enrichIDETranscriptWithToolCalls converts an IDE transcript to CLI format
// and injects the captured tool calls into the last assistant entry. Returns
// the re-serialized CLI-format JSON, or nil if conversion fails.
func enrichIDETranscriptWithToolCalls(ideData []byte, toolCalls []kiroToolCall) []byte {
	converted := tryParseIDETranscript(ideData)
	if converted == nil || len(converted.History) == 0 {
		return nil
	}

	// Assign all tool calls to the last assistant entry, since post-tool-use
	// hooks fire between user-prompt-submit and stop for the current turn.
	toolUse := kiroToolUseContent{
		ToolUse: kiroToolUsePayload{ToolUses: toolCalls},
	}
	toolUseJSON, err := json.Marshal(toolUse)
	if err != nil {
		return nil
	}

	lastIdx := len(converted.History) - 1
	converted.History[lastIdx].Assistant = toolUseJSON

	result, err := json.Marshal(converted)
	if err != nil {
		return nil
	}
	return result
}

func (a *Agent) captureTranscriptForStop(cwd string, sessionID string, conversationID string) string {
	// Try IDE workspace sessions first — the stop hook is fired by the IDE,
	// so IDE data is the most accurate source. CLI DB is a fallback for
	// kiro-cli (non-IDE) sessions where no IDE workspace data exists.
	if sessionRef, err := a.ensureIDETranscript(cwd, sessionID); err == nil && sessionRef != "" {
		return sessionRef
	}
	if sessionRef, err := a.ensureCachedTranscript(cwd, sessionID, conversationID); err == nil && sessionRef != "" {
		return sessionRef
	}
	return a.createPlaceholderTranscript(cwd, sessionID)
}

func (a *Agent) createPlaceholderTranscript(cwd string, sessionID string) string {
	cachePath, err := a.cacheTranscriptPath(cwd, sessionID)
	if err != nil {
		return ""
	}
	if err := os.WriteFile(cachePath, []byte("{}"), 0o600); err != nil {
		return ""
	}
	return cachePath
}

func (a *Agent) cacheTranscriptPath(cwd string, sessionID string) (string, error) {
	repoRoot := protocol.RepoRoot()
	if repoRoot == "" {
		repoRoot = cwd
	}
	sessionDir, err := a.GetSessionDir(repoRoot)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		return "", fmt.Errorf("failed to create cache dir: %w", err)
	}
	return a.ResolveSessionFile(sessionDir, sessionID), nil
}

func kiroDataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support"), nil
	default:
		return filepath.Join(home, ".local", "share"), nil
	}
}

func kiroCLIDataDBPath() (string, error) {
	dataDir, err := kiroDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "kiro-cli", "data.sqlite3"), nil
}

// kiroExtensionStorageDir returns the platform-specific base directory for
// Kiro IDE extension data (workspace sessions, execution logs, etc.).
func kiroExtensionStorageDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Kiro", "User", "globalStorage", "kiro.kiroagent"), nil
	default:
		return filepath.Join(home, ".config", "Kiro", "User", "globalStorage", "kiro.kiroagent"), nil
	}
}

func ideWorkspaceSessionsDir(cwd string) (string, error) {
	baseDir, err := kiroExtensionStorageDir()
	if err != nil {
		return "", err
	}
	// Kiro IDE uses standard base64 with '=' padding replaced by '_'.
	encoded := strings.ReplaceAll(base64.StdEncoding.EncodeToString([]byte(cwd)), "=", "_")
	return filepath.Join(baseDir, "workspace-sessions", encoded), nil
}

func escapeSQLString(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

func (a *Agent) GetTranscriptPosition(path string) (int, error) {
	transcript, err := readAndParseTranscript(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return len(transcript.History), nil
}

func (a *Agent) ExtractModifiedFiles(path string, offset int) ([]string, int, error) {
	transcript, err := readAndParseTranscript(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}

	totalEntries := len(transcript.History)
	if offset >= totalEntries {
		return nil, totalEntries, nil
	}

	return extractModifiedFilesFromHistory(transcript.History[offset:]), totalEntries, nil
}

func (a *Agent) ExtractPrompts(path string, offset int) ([]string, error) {
	transcript, err := readAndParseTranscript(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var prompts []string
	for i := offset; i < len(transcript.History); i++ {
		if prompt := extractUserPrompt(transcript.History[i].User.Content); prompt != "" {
			prompts = append(prompts, prompt)
		}
	}
	return prompts, nil
}

func (a *Agent) ExtractSummary(path string) (string, bool, error) {
	transcript, err := readAndParseTranscript(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}

	summary := extractLastAssistantResponse(transcript.History)
	return summary, summary != "", nil
}

func readAndParseTranscript(path string) (*kiroTranscript, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseTranscript(data)
}

func parseTranscript(data []byte) (*kiroTranscript, error) {
	if len(data) == 0 {
		return &kiroTranscript{}, nil
	}

	var transcript kiroTranscript
	if err := json.Unmarshal(data, &transcript); err != nil {
		return nil, fmt.Errorf("failed to parse kiro transcript: %w", err)
	}
	if isCLITranscript(&transcript) {
		return &transcript, nil
	}

	if converted := tryParseIDETranscript(data); converted != nil {
		return converted, nil
	}

	return &transcript, nil
}

func isCLITranscript(transcript *kiroTranscript) bool {
	if len(transcript.History) == 0 {
		return false
	}
	return len(transcript.History[0].User.Content) > 0 || len(transcript.History[0].Assistant) > 0
}

func tryParseIDETranscript(data []byte) *kiroTranscript {
	var ide kiroIDETranscript
	if err := json.Unmarshal(data, &ide); err != nil {
		return nil
	}
	if len(ide.History) == 0 || ide.History[0].Message.Role == "" {
		return nil
	}
	return convertIDETranscript(&ide)
}

func convertIDETranscript(ide *kiroIDETranscript) *kiroTranscript {
	transcript := &kiroTranscript{}

	var pendingUser *kiroIDEHistoryEntry
	for i := range ide.History {
		entry := &ide.History[i]
		switch entry.Message.Role {
		case "user":
			if pendingUser != nil {
				transcript.History = append(transcript.History, ideEntryToPaired(pendingUser, nil))
			}
			pendingUser = entry
		case "assistant":
			transcript.History = append(transcript.History, ideEntryToPaired(pendingUser, entry))
			pendingUser = nil
		}
	}

	if pendingUser != nil {
		transcript.History = append(transcript.History, ideEntryToPaired(pendingUser, nil))
	}

	return transcript
}

func ideEntryToPaired(user, assistant *kiroIDEHistoryEntry) kiroHistoryEntry {
	entry := kiroHistoryEntry{}

	if user != nil {
		prompt := extractIDEText(user.Message.Content, true)
		if prompt != "" {
			content, err := json.Marshal(kiroPromptContent{
				Prompt: struct {
					Prompt string `json:"prompt"`
				}{Prompt: prompt},
			})
			if err == nil {
				entry.User.Content = content
			}
		} else {
			entry.User.Content = user.Message.Content
		}
	}

	if assistant != nil {
		text := extractIDEText(assistant.Message.Content, false)
		if text != "" {
			content, err := json.Marshal(kiroResponseContent{
				Response: kiroResponsePayload{Content: text},
			})
			if err == nil {
				entry.Assistant = content
			}
		} else {
			entry.Assistant = assistant.Message.Content
		}
	}

	return entry
}

// extractIDEText extracts text from a json.RawMessage that may be either a
// plain string or an array of content blocks. When blocksFirst is true, it
// tries to parse as blocks before falling back to a plain string (user
// messages); otherwise it tries plain string first (assistant messages).
func extractIDEText(content json.RawMessage, blocksFirst bool) string {
	if len(content) == 0 {
		return ""
	}

	tryBlocks := func() string {
		var blocks []kiroIDEContentBlock
		if err := json.Unmarshal(content, &blocks); err == nil && len(blocks) > 0 {
			for _, block := range blocks {
				if block.Type == "text" && block.Text != "" {
					return block.Text
				}
			}
		}
		return ""
	}

	tryString := func() string {
		var text string
		if err := json.Unmarshal(content, &text); err == nil {
			return text
		}
		return ""
	}

	if blocksFirst {
		if s := tryBlocks(); s != "" {
			return s
		}
		return tryString()
	}
	if s := tryString(); s != "" {
		return s
	}
	return tryBlocks()
}

func extractUserPrompt(content json.RawMessage) string {
	if len(content) == 0 {
		return ""
	}

	var promptContent kiroPromptContent
	if err := json.Unmarshal(content, &promptContent); err == nil && promptContent.Prompt.Prompt != "" {
		return promptContent.Prompt.Prompt
	}
	return ""
}

func extractModifiedFilesFromHistory(entries []kiroHistoryEntry) []string {
	seen := make(map[string]bool)
	var files []string

	for i := range entries {
		for _, path := range extractFilesFromAssistant(entries[i].Assistant) {
			if path == "" || seen[path] {
				continue
			}
			seen[path] = true
			files = append(files, path)
		}
	}

	return files
}

func extractFilesFromAssistant(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}

	var toolUseContent kiroToolUseContent
	if err := json.Unmarshal(raw, &toolUseContent); err != nil || len(toolUseContent.ToolUse.ToolUses) == 0 {
		return nil
	}

	var paths []string
	for _, call := range toolUseContent.ToolUse.ToolUses {
		if !isFileModificationTool(call.Name) {
			continue
		}
		if path := extractFilePath(call.Args); path != "" {
			paths = append(paths, path)
		}
	}
	return paths
}

func isFileModificationTool(name string) bool {
	_, ok := kiroFileModificationTools[name]
	return ok
}

func extractFilePath(args json.RawMessage) string {
	if len(args) == 0 {
		return ""
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(args, &fields); err != nil {
		return ""
	}

	for _, key := range []string{"path", "file_path", "filename", "file"} {
		raw, ok := fields[key]
		if !ok {
			continue
		}
		var path string
		if err := json.Unmarshal(raw, &path); err == nil && path != "" {
			return path
		}
	}
	return ""
}

func extractLastAssistantResponse(entries []kiroHistoryEntry) string {
	for i := len(entries) - 1; i >= 0; i-- {
		if len(entries[i].Assistant) == 0 {
			continue
		}

		var responseContent kiroResponseContent
		if err := json.Unmarshal(entries[i].Assistant, &responseContent); err == nil && responseContent.Response.Content != "" {
			return responseContent.Response.Content
		}
	}
	return ""
}

// --- Execution log enrichment ---

// extractIDESessionID parses the sessionId from an IDE session JSON file.
func extractIDESessionID(data []byte) string {
	var meta kiroIDESessionMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return ""
	}
	return meta.SessionID
}

// extractIDEExecutionIDs parses executionId values from assistant entries
// in an IDE session JSON file.
func extractIDEExecutionIDs(data []byte) []string {
	var meta kiroIDESessionMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil
	}
	var ids []string
	for _, entry := range meta.History {
		if entry.ExecutionID != "" {
			ids = append(ids, entry.ExecutionID)
		}
	}
	return ids
}

// findExecutionLogsForSession scans Kiro IDE workspace directories to find
// execution log files matching the given chat session ID. Returns a map of
// executionId → *kiroExecutionLog.
func findExecutionLogsForSession(chatSessionID string) (map[string]*kiroExecutionLog, error) {
	baseDir, err := kiroExtensionStorageDir()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() || len(entry.Name()) != 32 {
			continue
		}
		logsDir := filepath.Join(baseDir, entry.Name(), execLogsSubdir)
		if _, err := os.Stat(logsDir); err != nil {
			continue
		}
		logs := scanExecutionLogsDir(logsDir, chatSessionID)
		if len(logs) > 0 {
			return logs, nil
		}
	}

	return map[string]*kiroExecutionLog{}, nil
}

// scanExecutionLogsDir reads all execution log files in a directory and returns
// those matching the given chat session ID.
func scanExecutionLogsDir(dir string, chatSessionID string) map[string]*kiroExecutionLog {
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	// Two-pass approach: first identify matching files using a lightweight
	// struct (avoids deserializing the large Actions array), then fully
	// parse only the matches.
	var matchingPaths []string
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		path := filepath.Join(dir, f.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var header struct {
			ChatSessionID string `json:"chatSessionId"`
		}
		if json.Unmarshal(data, &header) == nil && header.ChatSessionID == chatSessionID {
			matchingPaths = append(matchingPaths, path)
		}
	}

	if len(matchingPaths) == 0 {
		return nil
	}

	result := make(map[string]*kiroExecutionLog, len(matchingPaths))
	for _, path := range matchingPaths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var log kiroExecutionLog
		if err := json.Unmarshal(data, &log); err != nil {
			continue
		}
		result[log.ExecutionID] = &log
	}
	return result
}

// convertExecutionActionsToHistoryEntries converts Kiro IDE execution log
// actions into CLI transcript history entries. Tool-modifying actions (create,
// edit) become ToolUse entries; say actions become Response entries.
func convertExecutionActionsToHistoryEntries(actions []kiroExecutionAction) []kiroHistoryEntry {
	var toolCalls []kiroToolCall
	var entries []kiroHistoryEntry

	for _, action := range actions {
		if toolName, ok := execActionToToolName[action.ActionType]; ok {
			filePath := extractFilePath(action.Input)
			if filePath != "" {
				args, err := json.Marshal(map[string]string{"path": filePath})
				if err != nil {
					continue
				}
				toolCalls = append(toolCalls, kiroToolCall{
					Name: toolName,
					Args: args,
				})
			}
		}
		if action.ActionType == "say" {
			var out struct {
				Message string `json:"message"`
			}
			if err := json.Unmarshal(action.Output, &out); err == nil && out.Message != "" {
				// Flush any pending tool calls first
				if len(toolCalls) > 0 {
					toolUseJSON, err := json.Marshal(kiroToolUseContent{
						ToolUse: kiroToolUsePayload{ToolUses: toolCalls},
					})
					if err == nil {
						entries = append(entries, kiroHistoryEntry{Assistant: toolUseJSON})
					}
					toolCalls = nil
				}
				responseJSON, err := json.Marshal(kiroResponseContent{
					Response: kiroResponsePayload{Content: out.Message},
				})
				if err == nil {
					entries = append(entries, kiroHistoryEntry{Assistant: responseJSON})
				}
			}
		}
	}

	// Flush remaining tool calls without a trailing say
	if len(toolCalls) > 0 {
		toolUseJSON, err := json.Marshal(kiroToolUseContent{
			ToolUse: kiroToolUsePayload{ToolUses: toolCalls},
		})
		if err == nil {
			entries = append(entries, kiroHistoryEntry{Assistant: toolUseJSON})
		}
	}

	return entries
}

// enrichIDETranscriptWithExecutionLogs converts an IDE transcript to CLI
// format and replaces "On it." assistant entries with actual data from
// execution logs. Returns the re-serialized CLI-format JSON, or nil if
// conversion fails.
func enrichIDETranscriptWithExecutionLogs(ideData []byte, execLogs map[string]*kiroExecutionLog) []byte {
	var meta kiroIDESessionMeta
	if err := json.Unmarshal(ideData, &meta); err != nil {
		return nil
	}
	if len(meta.History) == 0 {
		return nil
	}

	// Convert to CLI format, enriching assistant entries from execution logs.
	// kiroIDEHistoryMeta contains both the Message (for pairing) and the
	// ExecutionID (for matching against execution logs).
	transcript := &kiroTranscript{}
	var pendingUser *kiroIDEHistoryEntry
	for i := range meta.History {
		// Adapt meta entry to IDE entry (they share the same Message field)
		ideEntry := &kiroIDEHistoryEntry{Message: meta.History[i].Message}
		switch ideEntry.Message.Role {
		case "user":
			if pendingUser != nil {
				transcript.History = append(transcript.History, ideEntryToPaired(pendingUser, nil))
			}
			pendingUser = ideEntry
		case "assistant":
			if execID := meta.History[i].ExecutionID; execID != "" {
				if execLog, ok := execLogs[execID]; ok {
					userEntry := ideEntryToPaired(pendingUser, nil)
					assistantEntries := convertExecutionActionsToHistoryEntries(execLog.Actions)
					if len(assistantEntries) > 0 {
						userEntry.Assistant = assistantEntries[0].Assistant
						transcript.History = append(transcript.History, userEntry)
						transcript.History = append(transcript.History, assistantEntries[1:]...)
						pendingUser = nil
						continue
					}
				}
			}
			transcript.History = append(transcript.History, ideEntryToPaired(pendingUser, ideEntry))
			pendingUser = nil
		}
	}
	if pendingUser != nil {
		transcript.History = append(transcript.History, ideEntryToPaired(pendingUser, nil))
	}

	result, err := json.Marshal(transcript)
	if err != nil {
		return nil
	}
	return result
}
