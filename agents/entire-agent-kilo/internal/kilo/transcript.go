package kilo

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

	"github.com/entireio/external-agents/agents/entire-agent-kilo/internal/protocol"
)

const prepareTranscriptTimeout = 30 * time.Second

func parseKiloSession(data []byte) (*Session, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return &Session{}, nil
	}
	if trimmed[0] != '{' {
		return nil, errors.New("kilo session is not prepared: run prepare-transcript first")
	}
	var session Session
	if err := json.Unmarshal(trimmed, &session); err != nil {
		return nil, fmt.Errorf("parse kilo session: %w", err)
	}
	return &session, nil
}

func sessionIDFromTranscriptData(data []byte) (string, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return "", errors.New("empty kilo session")
	}
	session, err := parseKiloSession(trimmed)
	if err == nil && session.ID != "" {
		return session.ID, nil
	}
	return "", errors.New("cannot prepare transcript: missing kilo session id")
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
	session, err := parseKiloSession(data)
	if err != nil {
		return protocol.AgentSessionJSON{}, err
	}
	if session.ID == "" {
		if sessionID == "" {
			sessionID = strings.TrimSuffix(filepath.Base(sessionRef), filepath.Ext(sessionRef))
		}
		return protocol.AgentSessionJSON{
			SessionID:     sessionID,
			AgentName:     "kilo",
			RepoPath:      protocol.RepoRoot(),
			SessionRef:    sessionRef,
			StartTime:     time.Now().UTC().Format(time.RFC3339),
			NativeData:    data,
			ModifiedFiles: []string{},
			NewFiles:      []string{},
			DeletedFiles:  []string{},
		}, nil
	}

	startTime := sessionStartTime(session)

	return protocol.AgentSessionJSON{
		SessionID:     session.ID,
		AgentName:     "kilo",
		RepoPath:      protocol.RepoRoot(),
		SessionRef:    sessionRef,
		StartTime:     startTime.Format(time.RFC3339),
		NativeData:    data,
		ModifiedFiles: modifiedFilesFromSession(session),
		NewFiles:      []string{},
		DeletedFiles:  []string{},
	}, nil
}

func sessionStartTime(session *Session) time.Time {
	if session != nil && session.Time != nil {
		if t := session.Time.Created.Time(); !t.IsZero() {
			return t
		}
		if t := session.Time.Updated.Time(); !t.IsZero() {
			return t
		}
	}
	return time.Now().UTC()
}

func (a *Agent) PrepareTranscript(sessionRef string) error {
	if strings.TrimSpace(sessionRef) == "" {
		return errors.New("session_ref is required")
	}
	data, err := os.ReadFile(sessionRef)
	if err != nil {
		return fmt.Errorf("read transcript for prepare: %w", err)
	}
	sessionID, err := sessionIDFromTranscriptData(data)
	if err != nil {
		return err
	}
	if sessionID == "" {
		return nil
	}
	return a.fetchSession(sessionID, sessionRef)
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
	if _, err := parseKiloSession(data); err != nil {
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
	session, err := parseKiloSession(data)
	if err != nil {
		return 0, err
	}
	return len(session.Messages), nil
}

func (a *Agent) ExtractModifiedFiles(path string, offset int) ([]string, int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	session, err := parseKiloSession(data)
	if err != nil {
		return nil, 0, err
	}
	return modifiedFilesFromSessionOffset(session, offset), len(session.Messages), nil
}

func (a *Agent) ExtractPrompts(sessionRef string, offset int) ([]string, error) {
	data, err := os.ReadFile(sessionRef)
	if err != nil {
		return nil, err
	}
	session, err := parseKiloSession(data)
	if err != nil {
		return nil, err
	}
	var prompts []string
	for _, msg := range sessionMessagesFromOffset(session, offset) {
		if msg.Info.Role != MessageRoleUser {
			continue
		}
		if text := messageText(msg); text != "" {
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
	session, err := parseKiloSession(data)
	if err != nil {
		return "", false, err
	}
	for i := len(session.Messages) - 1; i >= 0; i-- {
		msg := session.Messages[i]
		if msg.Info.Role != MessageRoleAssistant {
			continue
		}
		if text := messageText(msg); text != "" {
			return text, true, nil
		}
	}
	return "", false, nil
}

func (a *Agent) CalculateTokens(data []byte, offset int) (protocol.TokenUsageResponse, error) {
	session, err := parseKiloSession(data)
	if err != nil {
		return protocol.TokenUsageResponse{}, err
	}
	var usage protocol.TokenUsageResponse
	for _, msg := range sessionMessagesFromOffset(session, offset) {
		if msg.Info.Role != MessageRoleAssistant || msg.Info.Tokens == nil {
			continue
		}
		tokens := msg.Info.Tokens
		usage.InputTokens += tokens.Input
		usage.OutputTokens += tokens.Output
		if tokens.Cache != nil {
			usage.CacheReadTokens += tokens.Cache.Read
			usage.CacheCreationTokens += tokens.Cache.Write
		}
		usage.APICallCount++
	}
	return usage, nil
}

func sessionMessagesFromOffset(session *Session, offset int) []SessionMessage {
	if session == nil || len(session.Messages) == 0 {
		return nil
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= len(session.Messages) {
		return nil
	}
	return session.Messages[offset:]
}

func latestSessionModel(session *Session) string {
	if session == nil {
		return ""
	}
	for i := len(session.Messages) - 1; i >= 0; i-- {
		info := session.Messages[i].Info
		if info.Role != MessageRoleAssistant {
			continue
		}
		if model := strings.TrimSpace(info.ModelID); model != "" {
			return model
		}
	}
	return ""
}

func modifiedFilesFromSession(session *Session) []string {
	if session == nil {
		return nil
	}
	return modifiedFilesFromMessages(session.Messages)
}

func modifiedFilesFromSessionOffset(session *Session, offset int) []string {
	if session == nil {
		return nil
	}
	return modifiedFilesFromMessages(sessionMessagesFromOffset(session, offset))
}

func modifiedFilesFromMessages(messages []SessionMessage) []string {
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

// toolInputFileKeys lists the JSON object keys to scan on a tool call's
// `state.input` for file paths. Includes single-path and multi-path forms.
var toolInputFileKeys = []string{"path", "filePath", "filepath", "file", "absolutePath", "paths", "files"}

// toolMetadataFileKeys lists the JSON object keys to scan on a tool call's
// `state.metadata` (Kilo wraps the resolved path here for write/edit tools).
var toolMetadataFileKeys = []string{"filePath", "filepath", "path", "absolutePath"}

func modifiedFilesFromMessage(msg SessionMessage) []string {
	var files []string
	for _, part := range msg.Parts {
		if part.Type != PartTool || part.State == nil || !isMutatingToolName(part.Tool) {
			continue
		}
		files = append(files, filePathsFromJSON(part.State.Input, toolInputFileKeys)...)
		files = append(files, filePathsFromJSON(part.State.Metadata, toolMetadataFileKeys)...)
	}
	return cleanFiles(files)
}

func filePathsFromJSON(raw json.RawMessage, keys []string) []string {
	if len(raw) == 0 {
		return nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil
	}
	var files []string
	for _, key := range keys {
		files = append(files, stringValues(obj[key])...)
	}
	return files
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

func isMutatingToolName(name string) bool {
	switch normalizeToolName(name) {
	case "write", "edit", "multiedit", "multi_edit",
		"patch", "create", "create_file",
		"delete", "delete_file",
		"move", "move_file",
		"rename", "rename_file":
		return true
	}
	return false
}

func normalizeToolName(name string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(name)), "-", "_")
}

func messageText(msg SessionMessage) string {
	var parts []string
	for _, part := range msg.Parts {
		if part.Type == PartText && part.Text != "" {
			parts = append(parts, part.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func messageTimestamp(msg SessionMessage) string {
	if msg.Info.Time == nil {
		return ""
	}
	if t := msg.Info.Time.Created.Time(); !t.IsZero() {
		return t.Format(time.RFC3339)
	}
	return ""
}
