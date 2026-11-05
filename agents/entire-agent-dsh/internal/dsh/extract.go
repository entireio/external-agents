package dsh

import (
	"encoding/json"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/entireio/external-agents/agents/entire-agent-dsh/internal/protocol"
)

// transcriptContextKinds are DSH user-role events that inject context rather
// than carry a prompt. They are not prompts and do not appear as user lines in
// compact transcripts.
var transcriptContextKinds = map[string]struct{}{
	"runtime-context":       {},
	"skill-catalog":         {},
	"time-context":          {},
	"user-approval":         {},
	"repeat-tool-reminder":  {},
	"working-directory":     {},
	"model-selection":       {},
	"tool-jobs":             {},
	"system-reminder":       {},
	"command-result":        {},
	"compaction-summary":    {},
	"goal-continuation":     {},
	"agent-team-notice":     {},
	"plugin-configuration":  {},
	"session-title-context": {},
}

func isPromptSource(source messageSource) bool {
	if source.Form != "" {
		return false
	}
	_, context := transcriptContextKinds[source.Kind]
	return !context
}

// blocksTextOrPlaceholder keeps non-text user content visible in the compact
// transcript without inventing a DSH attachment representation Entire cannot
// read yet.
func blocksTextOrPlaceholder(blocks []contentBlock) string {
	if text := blocksText(blocks); text != "" {
		return text
	}
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case "image":
			parts = append(parts, "[image]")
		case "file":
			parts = append(parts, "[file]")
		}
	}
	return strings.Join(parts, "\n")
}

func blocksText(blocks []contentBlock) string {
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block.Type != "text" {
			continue
		}
		if block.Text == "" {
			continue
		}
		parts = append(parts, block.Text)
	}
	return strings.Join(parts, "\n")
}

func (a *Agent) GetTranscriptPosition(path string) (int, error) {
	data, exists, err := analyzerTranscript(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	if !exists {
		return 0, nil
	}
	return countTranscriptLines(data), nil
}

func (a *Agent) ExtractModifiedFiles(path string, offset int) ([]string, int, error) {
	data, exists, err := analyzerTranscript(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []string{}, 0, nil
		}
		return nil, 0, err
	}
	if !exists {
		return []string{}, 0, nil
	}
	files := extractModifiedFilesAfter(data, offset, repoBaseDir(data, protocol.RepoRoot()))
	return files, countTranscriptLines(data), nil
}

func (a *Agent) ExtractPrompts(sessionRef string, offset int) ([]string, error) {
	data, exists, err := analyzerTranscript(sessionRef)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []string{}, nil
		}
		return nil, err
	}
	if !exists {
		return []string{}, nil
	}
	prompts := make([]string, 0)
	forEachEventLine(data, offset, func(line []byte) {
		var event eventEnvelope[userMessageData]
		if err := json.Unmarshal(line, &event); err != nil {
			return
		}
		if event.Type != "user/message" || !isPromptSource(event.Data.Source) {
			return
		}
		if prompt := blocksText(event.Data.Content); prompt != "" {
			prompts = append(prompts, prompt)
		}
	})
	return prompts, nil
}

func (a *Agent) ExtractSummary(sessionRef string) (string, bool, error) {
	data, exists, err := analyzerTranscript(sessionRef)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", false, nil
		}
		return "", false, err
	}
	if !exists {
		return "", false, nil
	}
	summary := ""
	forEachEventLine(data, 0, func(line []byte) {
		var event eventEnvelope[assistantMessageData]
		if err := json.Unmarshal(line, &event); err != nil {
			return
		}
		if event.Type != "assistant/message" {
			return
		}
		if text := blocksText(event.Data.Message.Content); text != "" {
			summary = text
		}
	})
	if summary == "" {
		return "", false, nil
	}
	if len(summary) > 4000 {
		summary = summary[:4000]
	}
	return summary, true, nil
}

func (a *Agent) CalculateTokens(data []byte, offset int) (protocol.TokenUsageResponse, error) {
	usage := protocol.TokenUsageResponse{}
	forEachEventLine(data, offset, func(line []byte) {
		var event eventEnvelope[assistantMessageData]
		if err := json.Unmarshal(line, &event); err != nil {
			return
		}
		if event.Type != "assistant/message" || event.Data.Usage == nil {
			return
		}
		usage.InputTokens += event.Data.Usage.InputTokens
		usage.OutputTokens += event.Data.Usage.OutputTokens
		usage.CacheReadTokens += event.Data.Usage.CacheReadTokens
		usage.CacheCreationTokens += event.Data.Usage.CacheWriteTokens
		usage.APICallCount++
	})
	return usage, nil
}

// analyzerTranscript resolves any transcript reference the protocol may hand
// this binary: a materialized cache file or the DSH native log.
func analyzerTranscript(path string) ([]byte, bool, error) {
	if strings.TrimSpace(path) == "" {
		return nil, false, nil
	}
	if fileExists(path) {
		data, err := readTranscriptFile(path)
		return data, err == nil, err
	}
	if sessionID, ok := sessionIDFromRef(path); ok {
		_, data, err := (&Agent{}).materializeTranscript(sessionID, protocol.RepoRoot(), "", materializeOptions{})
		if err == nil {
			return data, true, nil
		}
		if errors.Is(err, fs.ErrNotExist) {
			return nil, false, nil
		}
	}
	return nil, false, nil
}

func repoBaseDir(data []byte, repoRoot string) string {
	if header, err := parseSessionHeader(data); err == nil {
		if header.CWD != "" && filepath.IsAbs(header.CWD) {
			return filepath.Clean(header.CWD)
		}
	}
	return repoRoot
}

func extractModifiedFilesAfter(data []byte, offset int, base string) []string {
	files := make([]string, 0)
	forEachEventLine(data, offset, func(line []byte) {
		var event eventEnvelope[toolCallData]
		if err := json.Unmarshal(line, &event); err != nil {
			return
		}
		if event.Type != "tool/call" {
			return
		}
		for _, path := range mutatingPaths(event.Data.Name, event.Data.Arguments) {
			if resolved := resolveToolPath(path, base); resolved != "" {
				files = append(files, resolved)
			}
		}
	})
	return dedupeStrings(files)
}

// extractFileLists classifies every mutating tool call in a session. Only
// apply_patch records create/delete intent explicitly; all other mutations are
// reported as modified.
func extractFileLists(data []byte, repoRoot string) (modified, created, deleted []string) {
	modified = make([]string, 0)
	created = make([]string, 0)
	deleted = make([]string, 0)
	base := repoBaseDir(data, repoRoot)
	forEachEventLine(data, 0, func(line []byte) {
		var event eventEnvelope[toolCallData]
		if err := json.Unmarshal(line, &event); err != nil {
			return
		}
		if event.Type != "tool/call" {
			return
		}
		name := event.Data.Name
		arguments := parseArguments(event.Data.Arguments)
		for _, path := range mutatingPaths(name, event.Data.Arguments) {
			resolved := resolveToolPath(path, base)
			if resolved == "" {
				continue
			}
			kind := "modified"
			if name == "apply_patch" {
				kind = patchPathKind(arguments, path)
			}
			switch kind {
			case "created":
				created = append(created, resolved)
			case "deleted":
				deleted = append(deleted, resolved)
			default:
				modified = append(modified, resolved)
			}
		}
	})
	return dedupeStrings(modified), dedupeStrings(created), dedupeStrings(deleted)
}

func mutatingPaths(name, rawArguments string) []string {
	args := parseArguments(rawArguments)
	switch name {
	case "write", "edit":
		return stringField(args, "file_path")
	case "str_replace_editor":
		command, _ := args["command"].(string)
		if command == "view" || command == "" {
			return nil
		}
		return stringField(args, "path")
	case "apply_patch":
		patch, _ := args["input"].(string)
		return applyPatchPaths(patch)
	default:
		return nil
	}
}

func parseArguments(raw string) map[string]any {
	if raw == "" {
		return map[string]any{}
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil || parsed == nil {
		return map[string]any{}
	}
	return parsed
}

func stringField(args map[string]any, key string) []string {
	value, ok := args[key].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return nil
	}
	return []string{value}
}

func applyPatchPaths(patch string) []string {
	if patch == "" {
		return nil
	}
	var paths []string
	for _, line := range strings.Split(patch, "\n") {
		for _, prefix := range []string{"*** Add File: ", "*** Update File: ", "*** Delete File: ", "*** Move to: "} {
			if strings.HasPrefix(line, prefix) {
				path := strings.TrimSpace(strings.TrimPrefix(line, prefix))
				if path != "" {
					paths = append(paths, path)
				}
				break
			}
		}
	}
	return paths
}

func patchPathKind(args map[string]any, path string) string {
	patch, _ := args["input"].(string)
	for _, line := range strings.Split(patch, "\n") {
		switch {
		case strings.HasPrefix(line, "*** Add File: ") && strings.TrimSpace(strings.TrimPrefix(line, "*** Add File: ")) == path:
			return "created"
		case strings.HasPrefix(line, "*** Delete File: ") && strings.TrimSpace(strings.TrimPrefix(line, "*** Delete File: ")) == path:
			return "deleted"
		}
	}
	return "modified"
}

func resolveToolPath(path, base string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	if base == "" {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(base, path))
}

func dedupeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
