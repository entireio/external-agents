package goose

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/entireio/external-agents/agents/entire-agent-goose/internal/protocol"
)

// gooseExport models the output of `goose session export --format json`
// (verified against goose 1.37.0). Message content is camelCase, unlike the
// snake_case hook payloads.
type gooseExport struct {
	ID                     string         `json:"id"`
	WorkingDir             string         `json:"working_dir"`
	Name                   string         `json:"name"`
	CreatedAt              string         `json:"created_at"`
	AccumulatedInputTokens int            `json:"accumulated_input_tokens"`
	AccumulatedOutput      int            `json:"accumulated_output_tokens"`
	Conversation           []gooseMessage `json:"conversation"`
	ProviderName           string         `json:"provider_name"`
	ModelConfig            struct {
		ModelName string `json:"model_name"`
	} `json:"model_config"`
}

type gooseMessage struct {
	ID      string         `json:"id"`
	Role    string         `json:"role"`
	Created int64          `json:"created"`
	Content []gooseContent `json:"content"`
}

type gooseContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ID       string `json:"id,omitempty"`
	ToolCall *struct {
		Status string `json:"status"`
		Value  struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		} `json:"value"`
	} `json:"toolCall,omitempty"`
	ToolResult *struct {
		Status string `json:"status"`
		Value  struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"value"`
	} `json:"toolResult,omitempty"`
}

// parseGooseExport is tolerant: any valid JSON object parses, and callers
// check ID to decide whether it is a real goose export.
func parseGooseExport(data []byte) (*gooseExport, error) {
	var export gooseExport
	if err := json.Unmarshal(data, &export); err != nil {
		return nil, fmt.Errorf("parse goose export: %w", err)
	}
	return &export, nil
}

// gooseSessionIDPattern matches goose's YYYYMMDD_N session IDs.
var gooseSessionIDPattern = regexp.MustCompile(`^\d{8}_\d+$`)

func (a *Agent) ReadSession(input *protocol.HookInputJSON) (protocol.AgentSessionJSON, error) {
	var sessionID, sessionRef string
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

	export, parseErr := parseGooseExport(data)
	if parseErr != nil || export.ID == "" {
		// Not a goose export (e.g. round-tripped opaque native data); return
		// the session as stored without goose-specific enrichment.
		if sessionID == "" {
			sessionID = strings.TrimSuffix(filepath.Base(sessionRef), filepath.Ext(sessionRef))
		}
		return protocol.AgentSessionJSON{
			SessionID:     sessionID,
			AgentName:     "goose",
			RepoPath:      protocol.RepoRoot(),
			SessionRef:    sessionRef,
			StartTime:     time.Now().UTC().Format(time.RFC3339),
			NativeData:    data,
			ModifiedFiles: []string{},
			NewFiles:      []string{},
			DeletedFiles:  []string{},
		}, nil
	}

	startTime := export.CreatedAt
	if _, err := time.Parse(time.RFC3339, startTime); err != nil {
		startTime = time.Now().UTC().Format(time.RFC3339)
	}

	return protocol.AgentSessionJSON{
		SessionID:     export.ID,
		AgentName:     "goose",
		RepoPath:      protocol.RepoRoot(),
		SessionRef:    sessionRef,
		StartTime:     startTime,
		NativeData:    data,
		ModifiedFiles: modifiedFilesFromExport(export, 0),
		NewFiles:      []string{},
		DeletedFiles:  []string{},
	}, nil
}

func (a *Agent) WriteSession(session protocol.AgentSessionJSON) error {
	if session.SessionRef == "" {
		return errors.New("session_ref is required")
	}
	if err := os.MkdirAll(filepath.Dir(session.SessionRef), 0o750); err != nil {
		return err
	}
	return os.WriteFile(session.SessionRef, session.NativeData, 0o600)
}

func (a *Agent) ReadTranscript(sessionRef string) ([]byte, error) {
	return os.ReadFile(sessionRef)
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
	var out []byte
	for _, chunk := range chunks {
		out = append(out, chunk...)
	}
	return out, nil
}

// PrepareTranscript materializes the transcript by exporting the goose
// session named by the file (<session-id>.json). Files that don't follow
// goose's session ID naming are left untouched so opaque round-tripped data
// survives.
func (a *Agent) PrepareTranscript(sessionRef string) error {
	if strings.TrimSpace(sessionRef) == "" {
		return errors.New("session_ref is required")
	}
	sessionID := strings.TrimSuffix(filepath.Base(sessionRef), filepath.Ext(sessionRef))
	if !gooseSessionIDPattern.MatchString(sessionID) {
		if _, err := os.Stat(sessionRef); err == nil {
			return nil
		}
		return fmt.Errorf("cannot prepare transcript: %q is not a goose session id", sessionID)
	}
	return a.exportSession(sessionID, sessionRef)
}

// GetTranscriptPosition returns the number of conversation messages in the
// exported transcript. Positions are message indexes, not bytes: every
// export rewrites the whole JSON document, so byte offsets would not be
// stable across turns.
func (a *Agent) GetTranscriptPosition(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	export, err := parseGooseExport(data)
	if err != nil {
		return 0, nil
	}
	return len(export.Conversation), nil
}

func (a *Agent) ExtractModifiedFiles(path string, offset int) ([]string, int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	export, err := parseGooseExport(data)
	if err != nil {
		return []string{}, 0, nil
	}
	return modifiedFilesFromExport(export, offset), len(export.Conversation), nil
}

func (a *Agent) ExtractPrompts(sessionRef string, offset int) ([]string, error) {
	data, err := os.ReadFile(sessionRef)
	if err != nil {
		return nil, err
	}
	export, err := parseGooseExport(data)
	if err != nil {
		return []string{}, nil
	}
	prompts := []string{}
	for _, msg := range messagesFromOffset(export, offset) {
		if msg.Role != "user" {
			continue
		}
		for _, content := range msg.Content {
			if content.Type == "text" && content.Text != "" {
				prompts = append(prompts, content.Text)
			}
		}
	}
	return prompts, nil
}

// ExtractSummary returns goose's session name, which goose auto-generates
// from the conversation.
func (a *Agent) ExtractSummary(sessionRef string) (string, bool, error) {
	data, err := os.ReadFile(sessionRef)
	if err != nil {
		return "", false, err
	}
	export, err := parseGooseExport(data)
	if err != nil || export.ID == "" || export.Name == "" {
		return "", false, nil
	}
	return export.Name, true, nil
}

// CalculateTokens reports goose's session-level accumulated token totals.
// Goose does not persist reliable per-message token counts, so the offset
// cannot slice usage by turn; totals always cover the whole session.
func (a *Agent) CalculateTokens(data []byte, _ int) (protocol.TokenUsageResponse, error) {
	export, err := parseGooseExport(data)
	if err != nil {
		return protocol.TokenUsageResponse{}, err
	}
	usage := protocol.TokenUsageResponse{
		InputTokens:  export.AccumulatedInputTokens,
		OutputTokens: export.AccumulatedOutput,
	}
	for _, msg := range export.Conversation {
		if msg.Role == "assistant" {
			usage.APICallCount++
		}
	}
	return usage, nil
}

func messagesFromOffset(export *gooseExport, offset int) []gooseMessage {
	if export == nil || offset >= len(export.Conversation) {
		return nil
	}
	if offset < 0 {
		offset = 0
	}
	return export.Conversation[offset:]
}

// fileWritingTools maps goose developer-extension tool names to the argument
// key holding the file path.
var fileWritingTools = map[string]string{
	"write":       "path",
	"text_editor": "path",
	"edit":        "path",
}

func modifiedFilesFromExport(export *gooseExport, offset int) []string {
	files := []string{}
	seen := map[string]bool{}
	for _, msg := range messagesFromOffset(export, offset) {
		for _, content := range msg.Content {
			if content.Type != "toolRequest" || content.ToolCall == nil {
				continue
			}
			name := content.ToolCall.Value.Name
			// Extension-qualified names look like developer__text_editor.
			if idx := strings.LastIndex(name, "__"); idx >= 0 {
				name = name[idx+2:]
			}
			pathKey, ok := fileWritingTools[name]
			if !ok {
				continue
			}
			var args map[string]json.RawMessage
			if err := json.Unmarshal(content.ToolCall.Value.Arguments, &args); err != nil {
				continue
			}
			var path string
			if raw, ok := args[pathKey]; ok {
				_ = json.Unmarshal(raw, &path)
			}
			if path != "" && !seen[path] {
				seen[path] = true
				files = append(files, path)
			}
		}
	}
	return files
}

// modelFromSessionRef returns the model recorded in the materialized export,
// or "" when the export is missing or not yet written.
func modelFromSessionRef(sessionRef string) string {
	data, err := os.ReadFile(sessionRef)
	if err != nil {
		return ""
	}
	export, err := parseGooseExport(data)
	if err != nil {
		return ""
	}
	return export.ModelConfig.ModelName
}
