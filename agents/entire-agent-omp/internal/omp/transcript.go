package omp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/entireio/external-agents/agents/entire-agent-omp/internal/protocol"
)

func (a *Agent) ReadSession(input *protocol.HookInputJSON) (protocol.AgentSessionJSON, error) {
	if input == nil || strings.TrimSpace(input.SessionRef) == "" {
		return protocol.AgentSessionJSON{}, errors.New("session_ref is required")
	}
	data, err := os.ReadFile(input.SessionRef)
	if err != nil {
		return protocol.AgentSessionJSON{}, err
	}
	header, headerErr := parseSessionHeader(data)
	if headerErr == nil {
		session, err := parseSessionEntries(data, header)
		if err != nil {
			return protocol.AgentSessionJSON{}, err
		}
		return protocol.AgentSessionJSON{
			SessionID:     session.Header.ID,
			AgentName:     "omp",
			RepoPath:      session.Header.Cwd,
			SessionRef:    input.SessionRef,
			StartTime:     session.Header.Timestamp,
			NativeData:    data,
			ModifiedFiles: modifiedFiles(session.Active, 0),
			NewFiles:      []string{},
			DeletedFiles:  []string{},
		}, nil
	}
	return protocol.AgentSessionJSON{
		SessionID:     input.SessionID,
		AgentName:     "omp",
		RepoPath:      protocol.RepoRoot(),
		SessionRef:    input.SessionRef,
		StartTime:     time.Now().UTC().Format(time.RFC3339),
		NativeData:    data,
		ModifiedFiles: []string{},
		NewFiles:      []string{},
		DeletedFiles:  []string{},
	}, nil
}

func (a *Agent) WriteSession(session protocol.AgentSessionJSON) error {
	if strings.TrimSpace(session.SessionRef) == "" {
		return errors.New("session_ref is required")
	}
	dir := filepath.Dir(session.SessionRef)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".entire-omp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		return errors.Join(err, tmp.Close())
	}
	if _, err := tmp.Write(session.NativeData); err != nil {
		return errors.Join(err, tmp.Close())
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, session.SessionRef); err != nil {
		return err
	}
	return os.Chmod(session.SessionRef, 0o600)
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
		size := min(maxSize, len(content))
		chunks = append(chunks, content[:size])
		content = content[size:]
	}
	return chunks, nil
}

func (a *Agent) ReassembleTranscript(chunks [][]byte) ([]byte, error) {
	return bytes.Join(chunks, nil), nil
}

func (a *Agent) GetTranscriptPosition(path string) (int, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	session, err := parseSessionSlice(data)
	if err != nil {
		return 0, err
	}
	return session.Lines, nil
}

func (a *Agent) ExtractModifiedFiles(path string, offset int) ([]string, int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	session, err := parseSessionSlice(data)
	if err != nil {
		return nil, 0, err
	}
	return modifiedFiles(session.Active, offset), session.Lines, nil
}

func (a *Agent) ExtractPrompts(path string, offset int) ([]string, error) {
	session, err := readParsedSession(path)
	if err != nil {
		return nil, err
	}
	prompts := []string{}
	for _, record := range session.Active {
		if record.Line <= offset || record.Entry.Message == nil || record.Entry.Message.Role != "user" {
			continue
		}
		if text := messageText(record.Entry.Message.Content); text != "" {
			prompts = append(prompts, text)
		}
	}
	return prompts, nil
}

func (a *Agent) ExtractSummary(path string) (string, bool, error) {
	session, err := readParsedSession(path)
	if err != nil {
		return "", false, err
	}
	for i := len(session.Active) - 1; i >= 0; i-- {
		message := session.Active[i].Entry.Message
		if message != nil && message.Role == "assistant" {
			if text := messageText(message.Content); text != "" {
				return text, true, nil
			}
		}
	}
	return "", false, nil
}

func readParsedSession(path string) (*parsedSession, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseSessionSlice(data)
}

func extractModelFromPath(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	model, err := latestAssistantModel(data)
	if err != nil {
		return ""
	}
	return model
}

func latestAssistantModel(data []byte) (string, error) {
	session, err := parseFullSession(data)
	if err != nil {
		return "", err
	}
	for i := len(session.Active) - 1; i >= 0; i-- {
		message := session.Active[i].Entry.Message
		if message != nil && message.Role == "assistant" && message.Model != "" {
			return message.Model, nil
		}
	}
	return "", nil
}

func modifiedFiles(entries []parsedEntry, offset int) []string {
	seen := make(map[string]bool)
	files := []string{}
	add := func(path string, trim bool) {
		if trim {
			path = strings.TrimSpace(path)
		}
		if path != "" && !strings.Contains(path, "://") && !seen[path] {
			seen[path] = true
			files = append(files, path)
		}
	}
	for _, record := range entries {
		message := record.Entry.Message
		if record.Line <= offset || message == nil || message.Role != "assistant" {
			continue
		}
		for _, block := range messageBlocks(message.Content) {
			if block.Type != "toolCall" || !isWriteTool(block.Name) {
				continue
			}
			var arguments map[string]any
			if json.Unmarshal(block.Arguments, &arguments) != nil {
				continue
			}
			if normalizeToolName(block.Name) == "apply_patch" {
				input, _ := arguments["input"].(string)
				for _, path := range patchPaths(input) {
					add(path, false)
				}
				continue
			}
			for _, key := range []string{"path", "file_path", "filePath"} {
				path, ok := arguments[key].(string)
				if ok {
					add(path, true)
				}
			}
		}
	}
	return files
}

func patchPaths(input string) []string {
	lines := strings.Split(trimECMAScriptSpace(input), "\n")
	if len(lines) >= 2 {
		switch lines[0] {
		case "<<EOF", "<<'EOF'", `<<"EOF"`:
			if trimECMAScriptSpace(lines[len(lines)-1]) == "EOF" {
				lines = lines[1 : len(lines)-1]
			}
		}
	}
	if len(lines) < 3 ||
		trimECMAScriptSpace(lines[0]) != "*** Begin Patch" ||
		trimECMAScriptSpace(lines[len(lines)-1]) != "*** End Patch" {
		return nil
	}

	var paths []string
	for remaining := lines[1 : len(lines)-1]; len(remaining) > 0; {
		if trimECMAScriptSpace(remaining[0]) == "" {
			remaining = remaining[1:]
			continue
		}

		firstLine := trimECMAScriptSpace(remaining[0])
		switch {
		case strings.HasPrefix(firstLine, "*** Add File: "):
			paths = append(paths, firstLine[len("*** Add File: "):])
			consumed := 1
			for consumed < len(remaining) && strings.HasPrefix(remaining[consumed], "+") {
				consumed++
			}
			remaining = remaining[consumed:]
		case strings.HasPrefix(firstLine, "*** Delete File: "):
			paths = append(paths, firstLine[len("*** Delete File: "):])
			remaining = remaining[1:]
		case strings.HasPrefix(firstLine, "*** Update File: "):
			paths = append(paths, firstLine[len("*** Update File: "):])
			remaining = remaining[1:]

			if len(remaining) > 0 && strings.HasPrefix(remaining[0], "*** Move to: ") {
				paths = append(paths, remaining[0][len("*** Move to: "):])
				remaining = remaining[1:]
			}

			diffLines := 0
			for len(remaining) > 0 &&
				!strings.HasPrefix(remaining[0], "*** Add File:") &&
				!strings.HasPrefix(remaining[0], "*** Delete File:") &&
				!strings.HasPrefix(remaining[0], "*** Update File:") {
				diffLines++
				remaining = remaining[1:]
			}
			if diffLines == 0 {
				return nil
			}
		default:
			return nil
		}
	}
	return paths
}

func trimECMAScriptSpace(s string) string {
	return strings.TrimFunc(s, func(r rune) bool {
		return unicode.Is(unicode.Zs, r) ||
			r == '\uFEFF' ||
			r == '\t' || r == '\v' || r == '\f' ||
			r == '\n' || r == '\r' || r == '\u2028' || r == '\u2029'
	})
}

func isWriteTool(name string) bool {
	switch normalizeToolName(name) {
	case "write", "edit", "apply_patch":
		return true
	default:
		return false
	}
}

func normalizeToolName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, "-", "_")
	if index := strings.LastIndexAny(name, ".:/"); index >= 0 {
		name = name[index+1:]
	}
	return name
}

func messageText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var parts []string
	for _, block := range messageBlocks(raw) {
		if block.Type == "text" && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func messageBlocks(raw json.RawMessage) []contentBlock {
	var encoded []json.RawMessage
	if json.Unmarshal(raw, &encoded) != nil {
		return nil
	}
	blocks := make([]contentBlock, 0, len(encoded))
	for _, item := range encoded {
		var block contentBlock
		if json.Unmarshal(item, &block) == nil && block.Type != "" {
			blocks = append(blocks, block)
		}
	}
	return blocks
}
