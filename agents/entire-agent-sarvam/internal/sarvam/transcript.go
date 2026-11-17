package sarvam

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
)

// The rollout JSONL is append-only, and Entire tracks progress with the
// (position, offset) pair this analyzer produces, so offsets are record
// indexes: get-transcript-position returns the number of physical lines, and
// extract --offset is the line index to resume from.

// maxRolloutLine bounds a single rollout record. One record can embed a whole
// tool output, so the 10 MB ceiling the other JSONL scanners use applies.
const maxRolloutLine = 10 * 1024 * 1024

// readRolloutLines parses the rollout JSONL. Unparseable lines become empty
// placeholders so line offsets stay aligned with the physical file. A final
// line that is still being written (no trailing newline) is excluded: the
// position must not cover a record whose remaining bytes arrive later, or
// the next read at that offset would skip them.
func readRolloutLines(path string) ([]rolloutLine, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var lines []rolloutLine
	reader := bufio.NewReaderSize(f, 64*1024)
	for {
		raw, readErr := reader.ReadBytes('\n')
		if len(raw) > 0 {
			line := strings.TrimSpace(string(raw))
			var record rolloutLine
			if json.Unmarshal([]byte(line), &record) != nil {
				record = rolloutLine{}
			}
			if readErr == nil {
				lines = append(lines, record)
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return lines, nil
			}
			return lines, readErr
		}
	}
}

func (a *Agent) GetTranscriptPosition(path string) (int, error) {
	lines, err := readRolloutLines(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return len(lines), nil
}

func clampOffset(offset, length int) int {
	if offset < 0 {
		return 0
	}
	if offset > length {
		return length
	}
	return offset
}

func (a *Agent) ExtractModifiedFiles(path string, offset int) ([]string, int, error) {
	lines, err := readRolloutLines(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	start := clampOffset(offset, len(lines))

	seen := map[string]struct{}{}
	for _, line := range lines[start:] {
		if line.Type != "response_item" {
			continue
		}
		var call rolloutFunctionCall
		if json.Unmarshal(line.Payload, &call) != nil || call.Type != "function_call" {
			continue
		}
		for _, file := range pathsFromToolCall(call) {
			normalized := strings.TrimSpace(file)
			if normalized != "" {
				seen[normalized] = struct{}{}
			}
		}
	}
	files := make([]string, 0, len(seen))
	for file := range seen {
		files = append(files, file)
	}
	sort.Strings(files)
	return files, len(lines), nil
}

// pathsFromToolCall extracts the file paths a tool call touches.
// write_file/edit_file carry "path"; exec_command carries a shell "command"
// whose redirects are best-effort parsed.
func pathsFromToolCall(call rolloutFunctionCall) []string {
	var args map[string]any
	if json.Unmarshal([]byte(call.Arguments), &args) != nil {
		return nil
	}
	switch call.Name {
	case "write_file":
		return stringField(args, "path")
	case "edit_file", "apply_patch", "edit":
		var paths []string
		paths = append(paths, stringField(args, "path")...)
		paths = append(paths, stringField(args, "file_path")...)
		return paths
	case "exec_command":
		command, _ := args["command"].(string)
		return pathsFromShellCommand(command)
	default:
		// Unknown tools: fall back to common path keys.
		var paths []string
		for _, key := range []string{"path", "file_path", "filePath", "notebook_path"} {
			paths = append(paths, stringField(args, key)...)
		}
		return paths
	}
}

func stringField(args map[string]any, key string) []string {
	value, ok := args[key].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return nil
	}
	return []string{value}
}

func (a *Agent) ExtractPrompts(sessionRef string, offset int) ([]string, error) {
	lines, err := readRolloutLines(sessionRef)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	start := clampOffset(offset, len(lines))

	var prompts []string
	for _, line := range lines[start:] {
		if line.Type != "response_item" {
			continue
		}
		var message rolloutMessage
		if json.Unmarshal(line.Payload, &message) != nil || message.Role != "user" {
			continue
		}
		text := messageText(message, "input_text")
		if text == "" || isHarnessContext(text) {
			continue
		}
		prompts = append(prompts, text)
	}
	return prompts, nil
}

// isHarnessContext filters the leading environment block sarvam records as a
// user item — it is injected context, not a human prompt.
func isHarnessContext(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), "<environment_context>")
}

// shellPathPatterns best-effort extract written files from shell commands:
// redirects, tee, and touch are the reliable signals in practice. The append
// form comes first in the alternation: Go's regexp is leftmost-first, so
// (>|>>) would match the single ">" of ">>" and capture ">" as the path.
var shellPathPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?:^|\s)(?:>>|>)\s*'?"?([^'"\s;&|]+)`),
	regexp.MustCompile(`(?:^|\s)tee\s+(?:-a\s+)?'?"?([^'"\s;&|]+)`),
	regexp.MustCompile(`(?:^|\s)touch\s+'?"?([^'"\s;&|]+)`),
}

func pathsFromShellCommand(command string) []string {
	if strings.TrimSpace(command) == "" {
		return nil
	}
	var paths []string
	for _, pattern := range shellPathPatterns {
		for _, match := range pattern.FindAllStringSubmatch(command, -1) {
			if len(match) > 1 {
				path := strings.Trim(match[1], `"'`)
				if isDevicePath(path) {
					continue
				}
				paths = append(paths, path)
			}
		}
	}
	return paths
}

// isDevicePath filters redirect targets that are device files rather than
// workspace paths: /dev/null dominates real usage, and /dev/std* and
// /dev/fd/* round out the family.
func isDevicePath(path string) bool {
	return path == "/dev" || strings.HasPrefix(path, "/dev/")
}

// messageText concatenates the text blocks of the given type.
func messageText(message rolloutMessage, blockType string) string {
	var b strings.Builder
	for _, block := range message.Content {
		if block.Type == blockType && block.Text != "" {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(block.Text)
		}
	}
	return b.String()
}

// ExtractSummary returns the agent's last assistant message as the session
// summary, mirroring the other external agents: it is the natural "what did
// the agent do" line for a checkpoint.
func (a *Agent) ExtractSummary(sessionRef string) (string, bool, error) {
	lines, err := readRolloutLines(sessionRef)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		if line.Type != "response_item" {
			continue
		}
		var message rolloutMessage
		if json.Unmarshal(line.Payload, &message) != nil || message.Role != "assistant" {
			continue
		}
		if text := messageText(message, "output_text"); strings.TrimSpace(text) != "" {
			return text, true, nil
		}
	}
	return "", false, nil
}
