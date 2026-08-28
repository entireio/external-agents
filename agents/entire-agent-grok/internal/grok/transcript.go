package grok

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/entireio/external-agents/agents/entire-agent-grok/internal/protocol"
)

var fileModificationTools = map[string]struct{}{
	"write_file":         {},
	"edit":               {},
	"edit_file":          {},
	"replace":            {},
	"save_file":          {},
	"writeFile":          {},
	"WriteFile":          {},
	"Write":              {},
	"search_replace":     {},
	"Edit":               {},
	"MultiEdit":          {},
	"NotebookEdit":       {},
	"notebook_edit":      {},
	"str_replace_editor": {},
	"Shell":              {},
	"Bash":               {},
	"run_shell_command":  {},
	"shell":              {},
}

var commonPathKeys = []string{
	"file_path",
	"filePath",
	"filepath",
	"path",
	"file",
	"notebook_path",
	"absolute_path",
	"relative_path",
	"target_file",
	"filename",
}

func (a *Agent) ReadSession(input *protocol.HookInputJSON) (protocol.AgentSessionJSON, error) {
	sessionID, sessionRef := a.sessionIDAndRef(input)
	data, err := os.ReadFile(sessionRef)
	if errors.Is(err, os.ErrNotExist) {
		data = nil
	} else if err != nil {
		return protocol.AgentSessionJSON{}, err
	}
	modifiedFiles, _, err := a.ExtractModifiedFiles(sessionRef, 0)
	if errors.Is(err, os.ErrNotExist) {
		modifiedFiles = nil
	} else if err != nil {
		return protocol.AgentSessionJSON{}, err
	}
	return protocol.AgentSessionJSON{
		SessionID:     sessionID,
		AgentName:     AgentName,
		RepoPath:      protocol.RepoRoot(),
		SessionRef:    sessionRef,
		StartTime:     time.Now().UTC().Format(time.RFC3339),
		NativeData:    data,
		ModifiedFiles: modifiedFiles,
		NewFiles:      []string{},
		DeletedFiles:  []string{},
	}, nil
}

func (a *Agent) WriteSession(session protocol.AgentSessionJSON) error {
	if strings.TrimSpace(session.SessionRef) == "" {
		return errMissingSessionRef
	}
	sessionDir := filepath.Dir(session.SessionRef)
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(session.SessionRef, session.NativeData, 0o600); err != nil {
		return err
	}
	return writeSessionSummary(sessionDir, session)
}

// writeSessionSummary synthesizes the summary.json that Grok requires to
// recognize a session directory.
//
// Restoring chat_history.jsonl alone is not enough: Grok's session lookup
// ignores a directory with no summary.json, reports "Session not found
// locally", and falls back to the remote registry (404 for a local-only
// session). Worse, `grok --continue` then silently resumes a different, older
// session instead of failing, so a bad restore looks like a good one.
//
// An existing summary.json is left alone so a restore over a live session
// does not clobber Grok's own metadata.
func writeSessionSummary(sessionDir string, session protocol.AgentSessionJSON) error {
	path := filepath.Join(sessionDir, "summary.json")
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	sessionID := strings.TrimSpace(session.SessionID)
	if sessionID == "" {
		sessionID = filepath.Base(sessionDir)
	}
	cwd := strings.TrimSpace(session.RepoPath)
	if cwd == "" {
		cwd = protocol.RepoRoot()
	}
	timestamp := normalizedSummaryTime(session.StartTime)
	count := countTranscriptLines(session.NativeData)

	messages, _ := readChatHistoryMessagesFromBytes(session.NativeData)
	summary := grokSessionSummary{
		Info:              grokSessionSummaryInfo{ID: sessionID, CWD: cwd},
		SessionSummary:    restoredSummaryTitle,
		GeneratedTitle:    restoredSummaryTitle,
		CurrentModelID:    lastModelID(messages),
		CreatedAt:         timestamp,
		UpdatedAt:         timestamp,
		LastActiveAt:      timestamp,
		NumMessages:       count,
		NumChatMessages:   count,
		ChatFormatVersion: 1,
		// Grok records git_root_dir with a trailing separator.
		GitRootDir: strings.TrimSuffix(cwd, "/") + "/",
		GrokHome:   grokHome(),
	}
	data, err := json.Marshal(summary)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// normalizedSummaryTime returns an RFC3339 timestamp Grok will accept.
//
// Entire sends an unset StartTime as a formatted zero time rather than an
// empty string, and Grok rejects a summary carrying a year-1 timestamp with
// "Session does not exist", so treat unparseable and zero values as absent.
func normalizedSummaryTime(value string) string {
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil || parsed.IsZero() || parsed.Year() < 2000 {
		return time.Now().UTC().Format(time.RFC3339)
	}
	return parsed.UTC().Format(time.RFC3339)
}

func countTranscriptLines(data []byte) int {
	count := 0
	for _, line := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(line)) > 0 {
			count++
		}
	}
	return count
}

func (a *Agent) ReadTranscript(sessionRef string) ([]byte, error) {
	return os.ReadFile(sessionRef)
}

func (a *Agent) ChunkTranscript(content []byte, maxSize int) ([][]byte, error) {
	if maxSize <= 0 {
		return nil, fmt.Errorf("max-size must be positive, got %d", maxSize)
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

func (a *Agent) GetTranscriptPosition(path string) (int, error) {
	if isNativeTranscript(path) {
		position, err := chatHistoryPosition(path)
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return position, err
	}
	records, err := readTranscriptEntries(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return len(records), nil
}

func (a *Agent) ExtractModifiedFiles(path string, offset int) ([]string, int, error) {
	if isNativeTranscript(path) {
		messages, err := readChatHistoryMessages(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil, 0, nil
		}
		if err != nil {
			return nil, 0, err
		}
		if offset < 0 {
			offset = 0
		}
		if offset > len(messages) {
			offset = len(messages)
		}
		return modifiedFilesFromChatHistory(messages, offset), len(messages), nil
	}
	records, err := readTranscriptEntries(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	if offset < 0 {
		offset = 0
	}
	if offset > len(records) {
		offset = len(records)
	}
	files := modifiedFilesFromRecords(records[offset:])
	return files, len(records), nil
}

func (a *Agent) ExtractPrompts(path string, offset int) ([]string, error) {
	if isNativeTranscript(path) {
		messages, err := readChatHistoryMessages(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return promptsFromChatHistory(messages, offset), nil
	}
	records, err := readTranscriptEntries(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if offset < 0 {
		offset = 0
	}
	if offset > len(records) {
		offset = len(records)
	}
	var prompts []string
	for _, record := range records[offset:] {
		if record.Event == "UserPromptSubmit" && strings.TrimSpace(record.Prompt) != "" {
			prompts = append(prompts, record.Prompt)
		}
	}
	return prompts, nil
}

func (a *Agent) ExtractSummary(path string) (string, bool, error) {
	if isNativeTranscript(path) {
		messages, err := readChatHistoryMessages(path)
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		if err != nil {
			return "", false, err
		}
		summary, ok := summaryFromChatHistory(messages)
		return summary, ok, nil
	}
	records, err := readTranscriptEntries(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	for i := len(records) - 1; i >= 0; i-- {
		record := records[i]
		if strings.TrimSpace(record.LastAssistantMessage) != "" {
			return record.LastAssistantMessage, true, nil
		}
		if strings.TrimSpace(record.ErrorDetails) != "" {
			return record.ErrorDetails, true, nil
		}
		if strings.TrimSpace(record.CompactSummary) != "" {
			return record.CompactSummary, true, nil
		}
	}
	return "", false, nil
}

func (a *Agent) sessionIDAndRef(input *protocol.HookInputJSON) (string, string) {
	sessionID := stubSessionID
	sessionRef := ""
	if input != nil {
		if strings.TrimSpace(input.SessionID) != "" {
			sessionID = input.SessionID
		}
		sessionRef = input.SessionRef
	}
	if strings.TrimSpace(sessionRef) == "" {
		sessionRef = a.resolveSessionRef(sessionID, protocol.RepoRoot())
	}
	if strings.TrimSpace(sessionID) == "" {
		sessionID = strings.TrimSuffix(filepath.Base(sessionRef), filepath.Ext(sessionRef))
	}
	return sessionID, sessionRef
}

func (a *Agent) writeSessionMarker(raw grokHookInputRaw, sessionRef string) error {
	marker := protocol.AgentSessionJSON{
		SessionID:  raw.SessionID,
		AgentName:  AgentName,
		RepoPath:   protocol.RepoRoot(),
		SessionRef: sessionRef,
		StartTime:  raw.Timestamp,
	}
	data, err := json.Marshal(marker)
	if err != nil {
		return err
	}
	path := filepath.Join(protocol.RepoRoot(), ".entire", "tmp", safeFilename(raw.SessionID)+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func safeFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return stubSessionID
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		default:
			if unicode.IsLetter(r) || unicode.IsNumber(r) {
				b.WriteRune(r)
			} else {
				b.WriteByte('_')
			}
		}
	}
	out := strings.Trim(b.String(), "._")
	if out == "" {
		return stubSessionID
	}
	return out
}

func readSidecarRecords(path string) ([]sidecarRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var records []sidecarRecord
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var record sidecarRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, scanner.Err()
}

func modifiedFilesFromRecords(records []sidecarRecord) []string {
	seen := map[string]struct{}{}
	for _, record := range records {
		if !isFileModificationTool(record.ToolName) {
			continue
		}
		for _, path := range pathsFromToolInput(record.ToolInput, record.ToolName) {
			normalized := strings.TrimSpace(path)
			if normalized == "" {
				continue
			}
			seen[normalized] = struct{}{}
		}
	}
	files := make([]string, 0, len(seen))
	for file := range seen {
		files = append(files, file)
	}
	sort.Strings(files)
	return files
}

func isFileModificationTool(name string) bool {
	if _, ok := fileModificationTools[name]; ok {
		return true
	}
	lower := strings.ToLower(name)
	return strings.Contains(lower, "write") ||
		strings.Contains(lower, "edit") ||
		strings.Contains(lower, "replace") ||
		strings.Contains(lower, "shell") ||
		strings.Contains(lower, "bash")
}

func pathsFromToolInput(raw json.RawMessage, toolName string) []string {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	var paths []string
	collectPathValues(value, &paths)
	if command, ok := value["command"].(string); ok && isShellTool(toolName) {
		paths = append(paths, pathsFromShellCommand(command)...)
	}
	return paths
}

func collectPathValues(value any, paths *[]string) {
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			if isCommonPathKey(key) {
				*paths = append(*paths, stringValues(item)...)
			}
			collectPathValues(item, paths)
		}
	case []any:
		for _, item := range v {
			collectPathValues(item, paths)
		}
	}
}

func isCommonPathKey(key string) bool {
	for _, candidate := range commonPathKeys {
		if strings.EqualFold(key, candidate) {
			return true
		}
	}
	return false
}

func stringValues(value any) []string {
	switch v := value.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return []string{v}
	case []any:
		var out []string
		for _, item := range v {
			out = append(out, stringValues(item)...)
		}
		return out
	default:
		return nil
	}
}

func isShellTool(toolName string) bool {
	lower := strings.ToLower(toolName)
	return lower == "bash" || lower == "shell" || strings.Contains(lower, "shell")
}

var shellPathPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?:^|\s)(?:>|>>)\s*['"]?([^'"\s;&|]+)`),
	regexp.MustCompile(`(?:^|\s)tee\s+(?:-a\s+)?['"]?([^'"\s;&|]+)`),
	regexp.MustCompile(`(?:^|\s)touch\s+['"]?([^'"\s;&|]+)`),
}

func pathsFromShellCommand(command string) []string {
	var paths []string
	for _, pattern := range shellPathPatterns {
		matches := pattern.FindAllStringSubmatch(command, -1)
		for _, match := range matches {
			if len(match) > 1 {
				paths = append(paths, strings.Trim(match[1], `"'`))
			}
		}
	}
	return paths
}

func copyRawMessage(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	out := make([]byte, len(raw))
	copy(out, raw)
	return out
}
