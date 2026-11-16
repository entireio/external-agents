package gemini

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/entireio/external-agents/agents/entire-agent-gemini/internal/protocol"
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
	"Edit":               {},
	"MultiEdit":          {},
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
	sessionDir, err := a.GetSessionDir(session.RepoPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		return err
	}
	path := a.ResolveSessionFile(sessionDir, session.SessionID)
	return os.WriteFile(path, session.NativeData, 0o644)
}

func (a *Agent) ReadTranscript(sessionRef string) ([]byte, error) {
	if strings.TrimSpace(sessionRef) == "" {
		return nil, errMissingSessionRef
	}
	data, err := os.ReadFile(sessionRef)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (a *Agent) ChunkTranscript(content []byte, maxSize int) ([][]byte, error) {
	if maxSize <= 0 {
		return [][]byte{content}, nil
	}
	var chunks [][]byte
	for len(content) > maxSize {
		split := bytes.LastIndexByte(content[:maxSize], '\n')
		if split < 0 {
			split = maxSize
		}
		chunks = append(chunks, content[:split])
		content = content[split:]
		if len(content) > 0 && content[0] == '\n' {
			content = content[1:]
		}
	}
	if len(content) > 0 {
		chunks = append(chunks, content)
	}
	return chunks, nil
}

func (a *Agent) ReassembleTranscript(chunks [][]byte) ([]byte, error) {
	return bytes.Join(chunks, []byte("\n")), nil
}

func (a *Agent) GetTranscriptPosition(path string) (int, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return int(info.Size()), nil
}

func (a *Agent) ExtractModifiedFiles(path string, offset int) ([]string, int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	currentPosition := len(data)
	if offset >= currentPosition {
		return nil, currentPosition, nil
	}

	lines := bytes.Split(data[offset:], []byte("\n"))
	seen := map[string]struct{}{}
	var files []string

	for _, line := range lines {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var rec sidecarRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		if _, isMod := fileModificationTools[rec.ToolName]; !isMod {
			continue
		}
		filePath := extractFilePathFromInput(rec.ToolInput)
		if filePath != "" {
			if _, ok := seen[filePath]; !ok {
				seen[filePath] = struct{}{}
				files = append(files, filePath)
			}
		}
	}
	return files, currentPosition, nil
}

func (a *Agent) ExtractPrompts(sessionRef string, offset int) ([]string, error) {
	data, err := os.ReadFile(sessionRef)
	if err != nil {
		return nil, err
	}
	lines := bytes.Split(data, []byte("\n"))
	var prompts []string
	for _, line := range lines {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var rec sidecarRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		if rec.Event == "BeforeAgent" && rec.Prompt != "" {
			prompts = append(prompts, rec.Prompt)
		}
	}
	return prompts, nil
}

func (a *Agent) ExtractSummary(sessionRef string) (string, bool, error) {
	data, err := os.ReadFile(sessionRef)
	if err != nil {
		return "", false, err
	}
	lines := bytes.Split(data, []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var rec sidecarRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		if rec.Event == "PreCompress" && rec.CompactSummary != "" {
			return rec.CompactSummary, true, nil
		}
	}
	return "", false, nil
}

func (a *Agent) sessionIDAndRef(input *protocol.HookInputJSON) (string, string) {
	sessionID := a.GetSessionID(input)
	sessionRef := a.sidecarPath(sessionID)
	return sessionID, sessionRef
}

func extractFilePathFromInput(toolInput json.RawMessage) string {
	if len(toolInput) == 0 {
		return ""
	}
	var m map[string]interface{}
	if err := json.Unmarshal(toolInput, &m); err != nil {
		return ""
	}
	for _, key := range commonPathKeys {
		if v, ok := m[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				return filepath.Clean(s)
			}
		}
	}
	// Search nested
	for _, v := range m {
		if s, ok := v.(string); ok {
			if looksLikeFilePath(s) {
				return filepath.Clean(s)
			}
		}
	}
	return ""
}

var pathLikeRe = regexp.MustCompile(`[/\\]\w+|^\w+\.\w+`)

func looksLikeFilePath(s string) bool {
	if len(s) == 0 || len(s) > 4096 {
		return false
	}
	if !unicode.IsLetter(rune(s[0])) && s[0] != '/' && s[0] != '.' && s[0] != '~' {
		return false
	}
	return pathLikeRe.MatchString(s)
}

// Suppress unused import warning
var _ = fmt.Sprintf
var _ = errors.New
