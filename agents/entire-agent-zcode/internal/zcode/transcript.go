package zcode

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

	"github.com/entireio/external-agents/agents/entire-agent-zcode/internal/protocol"
)

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

	// A session_ref that exists on disk but is not a transcript export is a
	// write-session snapshot (Entire stores raw native_data there); echo the
	// bytes back. Transcript refs (.jsonl under the zcode export dir) and
	// missing files fall through to the live transcript paths.
	if input != nil && input.SessionRef != "" && !isTranscriptExportRef(input.SessionRef) {
		if session, ok, err := readStoredSession(input.SessionRef, sessionID); err == nil && ok {
			return session, nil
		}
	}

	// Prefer a fresh export from ZCode's store; fall back to the last
	// prepared transcript file when the store is unavailable (app closed,
	// fixture-only environments).
	messages, startMS, err := a.loadMessages(context.Background(), sessionID, sessionRef)
	if err != nil {
		return protocol.AgentSessionJSON{}, err
	}
	if sessionID == "" {
		sessionID = sessionIDFromTranscriptRef(sessionRef)
	}

	encoded, err := encodeMessagesJSONL(messages)
	if err != nil {
		return protocol.AgentSessionJSON{}, err
	}

	startTime := time.Now().UTC()
	if startMS > 0 {
		startTime = time.UnixMilli(startMS)
	} else if len(messages) > 0 && messages[0].Time > 0 {
		startTime = time.UnixMilli(messages[0].Time)
	}

	return protocol.AgentSessionJSON{
		SessionID:     sessionID,
		AgentName:     "zcode",
		RepoPath:      protocol.RepoRoot(),
		SessionRef:    sessionRef,
		StartTime:     startTime.Format(time.RFC3339),
		NativeData:    encoded,
		ModifiedFiles: modifiedFilesFromMessages(messages),
		NewFiles:      []string{},
		DeletedFiles:  []string{},
	}, nil
}

// isTranscriptExportRef reports whether a session_ref points at a JSONL
// transcript exported by prepare-transcript rather than a session snapshot.
func isTranscriptExportRef(sessionRef string) bool {
	return filepath.Ext(sessionRef) == ".jsonl"
}

// readStoredSession builds an AgentSession from a write-session snapshot at
// path: an AgentSession envelope is echoed with its own fields, any other
// non-empty file is returned with its raw bytes as native_data.
func readStoredSession(path, sessionID string) (protocol.AgentSessionJSON, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return protocol.AgentSessionJSON{}, false, nil
		}
		return protocol.AgentSessionJSON{}, false, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return protocol.AgentSessionJSON{}, false, nil
	}

	var stored protocol.AgentSessionJSON
	if err := json.Unmarshal(data, &stored); err == nil &&
		(stored.SessionID != "" || stored.AgentName != "") {
		if stored.SessionRef == "" {
			stored.SessionRef = path
		}
		return stored, true, nil
	}

	// Raw native_data bytes: derive what we can from the content and file.
	messages, decodeErr := decodeTranscript(data)
	session := protocol.AgentSessionJSON{
		SessionID:  sessionID,
		AgentName:  "zcode",
		RepoPath:   protocol.RepoRoot(),
		SessionRef: path,
		NativeData: data,
	}
	if decodeErr == nil && len(messages) > 0 {
		if sessionID == "" {
			session.SessionID = sessionIDFromTranscriptRef(path)
		}
		if messages[0].Time > 0 {
			session.StartTime = time.UnixMilli(messages[0].Time).UTC().Format(time.RFC3339)
		}
		session.ModifiedFiles = modifiedFilesFromMessages(messages)
	}
	if session.StartTime == "" {
		if info, statErr := os.Stat(path); statErr == nil {
			session.StartTime = info.ModTime().UTC().Format(time.RFC3339)
		} else {
			session.StartTime = time.Now().UTC().Format(time.RFC3339)
		}
	}
	if session.ModifiedFiles == nil {
		session.ModifiedFiles = []string{}
	}
	session.NewFiles = []string{}
	session.DeletedFiles = []string{}
	return session, true, nil
}

// loadMessages exports from the SQLite store when possible, otherwise reads
// the prepared transcript file.
func (a *Agent) loadMessages(ctx context.Context, sessionID, sessionRef string) ([]ExportMessage, int64, error) {
	if sessionID != "" && a.querier() != nil {
		if _, statErr := os.Stat(dbPath()); statErr == nil {
			session, err := a.querySessionRow(ctx, sessionID)
			if err != nil {
				return nil, 0, err
			}
			messages, err := a.exportSession(ctx, sessionID)
			if err != nil {
				return nil, 0, err
			}
			if len(messages) > 0 {
				var startMS int64
				if session != nil {
					startMS = session.TimeCreated
				}
				return messages, startMS, nil
			}
		}
	}
	data, err := os.ReadFile(sessionRef)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, nil
		}
		return nil, 0, err
	}
	messages, err := decodeTranscript(data)
	if err != nil {
		return nil, 0, err
	}
	return messages, 0, nil
}

// PrepareTranscript materializes the JSONL export for a session. sessionRef
// is the transcript path (<session_dir>/zcode/<id>.jsonl). Never clobbers an
// existing transcript with an empty export — a store that is momentarily
// unreadable must not destroy the last good transcript.
func (a *Agent) PrepareTranscript(sessionRef string) error {
	if strings.TrimSpace(sessionRef) == "" {
		return errors.New("session_ref is required")
	}
	if a.querier() == nil {
		return nil
	}
	sessionID := sessionIDFromTranscriptRef(sessionRef)
	if _, statErr := os.Stat(dbPath()); statErr == nil {
		return a.exportTo(sessionID, sessionRef)
	}
	return nil
}

func (a *Agent) exportTo(sessionID, sessionRef string) error {
	messages, err := a.exportSession(context.Background(), sessionID)
	if err != nil {
		return fmt.Errorf("export session %s: %w", sessionID, err)
	}
	if len(messages) == 0 {
		return nil
	}
	encoded, err := encodeMessagesJSONL(messages)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(sessionRef), 0o750); err != nil {
		return err
	}
	return atomicWriteFile(sessionRef, encoded, 0o600)
}

// WriteSession persists Entire's session snapshot next to the transcript.
// ZCode's own store is never written to — restoring a live session happens
// through ZCode's UI; this snapshot exists for Entire's bookkeeping.
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

// readMessages loads and decodes a transcript file, tolerating a missing
// file as empty (Entire records position 0 for new sessions).
func readMessages(path string) ([]ExportMessage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return decodeTranscript(data)
}

func (a *Agent) GetTranscriptPosition(path string) (int, error) {
	messages, err := readMessages(path)
	if err != nil {
		return 0, err
	}
	return len(messages), nil
}

func (a *Agent) ExtractModifiedFiles(path string, offset int) ([]string, int, error) {
	messages, err := readMessages(path)
	if err != nil {
		return nil, 0, err
	}
	return modifiedFilesFromMessages(messagesFromOffset(messages, offset)), len(messages), nil
}

func (a *Agent) ExtractPrompts(sessionRef string, offset int) ([]string, error) {
	messages, err := readMessages(sessionRef)
	if err != nil {
		return nil, err
	}
	var prompts []string
	for _, msg := range messagesFromOffset(messages, offset) {
		if msg.Role != "user" || !isUserPrompt(msg) || msg.Text == "" {
			continue
		}
		prompts = append(prompts, msg.Text)
	}
	return prompts, nil
}

func (a *Agent) ExtractSummary(sessionRef string) (string, bool, error) {
	messages, err := readMessages(sessionRef)
	if err != nil {
		return "", false, err
	}
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Role != "assistant" || msg.Text == "" {
			continue
		}
		return msg.Text, true, nil
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
		if msg.Role != "assistant" || msg.Tokens == nil {
			continue
		}
		usage.InputTokens += msg.Tokens.Input
		usage.OutputTokens += msg.Tokens.Output
		usage.CacheReadTokens += msg.Tokens.CacheRead
		usage.CacheCreationTokens += msg.Tokens.CacheWrite
		usage.APICallCount++
	}
	return usage, nil
}

// isUserPrompt distinguishes real user prompts from synthetic user-role
// messages (system reminders, goal continuations). Anything without a
// semantics kind recorded falls back to role alone.
func isUserPrompt(msg ExportMessage) bool {
	if msg.Kind == "" || msg.Kind == "user_prompt" {
		return true
	}
	return false
}

// messagesFromOffset returns the messages at index >= offset; the on-disk
// transcript is one message per line, so the line offset Entire records via
// get-transcript-position is also a message index.
func messagesFromOffset(messages []ExportMessage, offset int) []ExportMessage {
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

// toolInputFileKeys lists the JSON object keys to scan on a tool call's
// input for file paths (single- and multi-path forms).
var toolInputFileKeys = []string{"file_path", "filePath", "filepath", "path", "absolutePath", "paths", "files"}

func modifiedFilesFromMessages(messages []ExportMessage) []string {
	seen := map[string]bool{}
	for _, msg := range messages {
		for _, tool := range msg.Tools {
			if !isMutatingToolName(tool.Tool) {
				continue
			}
			for _, file := range filePathsFromJSON(tool.Input, toolInputFileKeys) {
				if file != "" {
					seen[file] = true
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

func filePathsFromJSON(raw []byte, keys []string) []string {
	if len(raw) == 0 {
		return nil
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil
	}
	var files []string
	for _, key := range keys {
		switch value := obj[key].(type) {
		case string:
			if strings.TrimSpace(value) != "" {
				files = append(files, value)
			}
		case []any:
			for _, item := range value {
				if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
					files = append(files, s)
				}
			}
		}
	}
	return files
}

// isMutatingToolName covers ZCode's write-capable tool names (Write, Edit,
// ApplyPatch alias) plus generic fallbacks.
func isMutatingToolName(name string) bool {
	switch strings.ReplaceAll(strings.ToLower(strings.TrimSpace(name)), "-", "_") {
	case "write", "edit", "multiedit", "multi_edit", "apply_patch", "patch",
		"create", "create_file", "delete", "delete_file", "move", "move_file",
		"rename", "rename_file":
		return true
	}
	return false
}
