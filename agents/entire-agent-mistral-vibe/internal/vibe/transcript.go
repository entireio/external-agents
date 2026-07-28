package vibe

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
)

// vibeFileModificationTools lists the Vibe tool names that modify files.
var vibeFileModificationTools = map[string]struct{}{
	"write_file":     {},
	"search_replace": {},
	"create_file":    {},
	"edit_file":      {},
	"bash":           {},
}

// GetTranscriptPosition returns the number of lines (messages) in the JSONL
// transcript at the given path.
func (a *Agent) GetTranscriptPosition(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	defer f.Close()

	count := 0
	scanner := bufio.NewScanner(f)
	// Increase buffer size for potentially large JSONL lines.
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			count++
		}
	}
	return count, scanner.Err()
}

// ExtractModifiedFiles scans the JSONL transcript for tool calls that modify
// files (write_file, search_replace, etc.) and returns the list of file paths.
func (a *Agent) ExtractModifiedFiles(path string, offset int) ([]string, int, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, nil
		}
		return nil, 0, err
	}
	defer f.Close()

	seen := make(map[string]bool)
	var files []string
	lineNum := 0

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		lineNum++
		if lineNum <= offset {
			continue
		}

		var msg VibeMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}

		for _, tc := range msg.ToolCalls {
			if !isVibeFileModificationTool(tc.Function.Name) {
				continue
			}
			filePath := extractVibeFilePath(tc.Function.Arguments)
			if filePath != "" && !seen[filePath] {
				seen[filePath] = true
				files = append(files, filePath)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, 0, err
	}
	return files, lineNum, nil
}

// ExtractPrompts filters the JSONL transcript for user messages and returns
// their content strings.
func (a *Agent) ExtractPrompts(sessionRef string, offset int) ([]string, error) {
	f, err := os.Open(sessionRef)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var prompts []string
	lineNum := 0

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		lineNum++
		if lineNum <= offset {
			continue
		}

		var msg VibeMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}

		if msg.Role == "user" && msg.Content != "" {
			prompts = append(prompts, msg.Content)
		}
	}

	return prompts, scanner.Err()
}

// ExtractSummary returns the content of the last assistant message in the
// JSONL transcript as a summary.
func (a *Agent) ExtractSummary(sessionRef string) (string, bool, error) {
	f, err := os.Open(sessionRef)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	defer f.Close()

	var lastAssistantContent string

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var msg VibeMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}

		if msg.Role == "assistant" && msg.Content != "" {
			lastAssistantContent = msg.Content
		}
	}

	if err := scanner.Err(); err != nil {
		return "", false, err
	}

	return lastAssistantContent, lastAssistantContent != "", nil
}

// isVibeFileModificationTool returns true if the tool name modifies files.
func isVibeFileModificationTool(name string) bool {
	_, ok := vibeFileModificationTools[name]
	return ok
}

// extractVibeFilePath extracts a file path from a tool call's arguments JSON string.
func extractVibeFilePath(argsStr string) string {
	if argsStr == "" {
		return ""
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(argsStr), &fields); err != nil {
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
	for _, key := range []string{"command", "cmd", "bash_command", "shell_command"} {
		raw, ok := fields[key]
		if !ok {
			continue
		}
		var command string
		if err := json.Unmarshal(raw, &command); err == nil {
			if path := extractVibeFilePathFromCommand(command); path != "" {
				return path
			}
		}
	}
	return ""
}

func extractVibeFilePathFromCommand(command string) string {
	if command == "" {
		return ""
	}

	tokens := strings.Fields(command)
	for i, token := range tokens {
		switch token {
		case ">", ">>", "1>", "1>>":
			if i+1 < len(tokens) {
				return strings.Trim(tokens[i+1], `"'`)
			}
		}
	}
	if len(tokens) >= 2 && tokens[0] == "touch" {
		return strings.Trim(tokens[1], `"'`)
	}
	return ""
}
