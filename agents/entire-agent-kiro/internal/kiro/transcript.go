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

type ideSessionIndexEntry struct {
	SessionID   string `json:"sessionId"`
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
		return [][]byte{[]byte{}}, nil
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

func (a *Agent) ensureCachedTranscript(cwd string, sessionID string) (string, error) {
	db, err := kiroCLIDataDBPath()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(db); err != nil {
		return "", fmt.Errorf("kiro database not found at %s: %w", db, err)
	}

	query := fmt.Sprintf(
		"SELECT value FROM conversations_v2 WHERE key = '%s' ORDER BY updated_at DESC LIMIT 1",
		escapeSQLString(cwd),
	)

	out, err := runSQLiteCommand(db, query)
	if err != nil {
		return "", fmt.Errorf("sqlite3 transcript query failed: %w", err)
	}

	transcript := strings.TrimSpace(string(out))
	if transcript == "" {
		return "", errors.New("no transcript found")
	}

	cachePath, err := a.cacheTranscriptPath(cwd, sessionID)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(cachePath, []byte(transcript), 0o600); err != nil {
		return "", fmt.Errorf("failed to write cached transcript: %w", err)
	}
	return cachePath, nil
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

	transcriptPath := filepath.Join(sessionsDir, sessions[0].SessionID+".json")
	transcriptData, err := os.ReadFile(transcriptPath)
	if err != nil {
		return "", fmt.Errorf("failed to read IDE transcript %s: %w", transcriptPath, err)
	}

	cachePath, err := a.cacheTranscriptPath(cwd, sessionID)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(cachePath, transcriptData, 0o600); err != nil {
		return "", fmt.Errorf("failed to write cached IDE transcript: %w", err)
	}
	return cachePath, nil
}

func (a *Agent) captureTranscriptForStop(cwd string, sessionID string) string {
	if sessionRef, err := a.ensureCachedTranscript(cwd, sessionID); err == nil && sessionRef != "" {
		return sessionRef
	}
	if sessionRef, err := a.ensureIDETranscript(cwd, sessionID); err == nil && sessionRef != "" {
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
	if err := os.MkdirAll(sessionDir, 0o750); err != nil {
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

func ideWorkspaceSessionsDir(cwd string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	encoded := base64.StdEncoding.EncodeToString([]byte(cwd))
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Kiro", "User", "globalStorage", "kiro.kiroagent", "workspace-sessions", encoded), nil
	default:
		return filepath.Join(home, ".config", "Kiro", "User", "globalStorage", "kiro.kiroagent", "workspace-sessions", encoded), nil
	}
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

	for _, key := range []string{"path", "file_path", "filename"} {
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
