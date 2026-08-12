package hermes

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
)

var errTranscriptPathNotOwned = errors.New("transcript path is not owned by the Hermes observer")

type transcriptEntry struct {
	Version       int                `json:"v"`
	Type          string             `json:"type"`
	HermesType    string             `json:"hermes_type,omitempty"`
	Timestamp     string             `json:"timestamp"`
	Content       string             `json:"content,omitempty"`
	Message       *transcriptMessage `json:"message,omitempty"`
	Model         string             `json:"model,omitempty"`
	Name          string             `json:"name,omitempty"`
	Status        string             `json:"status,omitempty"`
	ModifiedFiles []string           `json:"modified_files,omitempty"`
}

type transcriptMessage struct {
	Content json.RawMessage `json:"content"`
}

type portableTranscriptEntry struct {
	Version       int              `json:"v"`
	Type          string           `json:"type"`
	HermesType    string           `json:"hermes_type,omitempty"`
	Timestamp     string           `json:"timestamp,omitempty"`
	Message       *portableMessage `json:"message,omitempty"`
	Model         string           `json:"model,omitempty"`
	Name          string           `json:"name,omitempty"`
	Status        string           `json:"status,omitempty"`
	ModifiedFiles []string         `json:"modified_files,omitempty"`
}

type portableMessage struct {
	Content any `json:"content"`
}

type portableAssistantBlock struct {
	Type  string         `json:"type"`
	Text  string         `json:"text,omitempty"`
	Name  string         `json:"name,omitempty"`
	Input map[string]any `json:"input,omitempty"`
}

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?is)-----BEGIN [^-\r\n]+ PRIVATE KEY-----.*?-----END [^-\r\n]+ PRIVATE KEY-----`),
	regexp.MustCompile(`(?i)\b(?:bearer|basic)\s+[A-Za-z0-9._~+/=-]{8,}`),
	regexp.MustCompile(`(?i)\b(?:password|passwd|pwd|secret|client_secret|api[_-]?key|access[_-]?token|refresh[_-]?token|authorization)\b\s*[:=]\s*(?:"[^"\r\n]*"|'[^'\r\n]*'|[^\s,;]+)`),
	regexp.MustCompile(`\b(?:sk|rk)_(?:live|test)_[A-Za-z0-9]{12,}\b`),
	regexp.MustCompile(`\b(?:sk|pk)-(?:proj-)?[A-Za-z0-9_-]{16,}\b`),
	regexp.MustCompile(`(?i)\b(?:gh[pousr]|github_pat)_[A-Za-z0-9_]{16,}\b`),
	regexp.MustCompile(`(?i)\b(?:xox[baprs]-[A-Za-z0-9-]{10,})\b`),
	regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
	regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b`),
	regexp.MustCompile(`(?i)(?:https?|ssh)://[^\s/@:]+:[^\s/@]+@`),
}

var unsafeModel = regexp.MustCompile(`[^A-Za-z0-9._:/+\-]+`)
var sensitiveTranscriptPath = regexp.MustCompile(`(?i)(?:^|/)(?:\.env(?:\..*)?|\.npmrc|\.pypirc|credentials(?:\..*)?|id_(?:rsa|dsa|ecdsa|ed25519)(?:\.pub)?|[^/]*(?:secret|token|private[_-]?key)[^/]*)$`)

func sanitizeText(value string) string {
	value = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || r >= 32 {
			return r
		}
		return ' '
	}, value)
	for _, pattern := range secretPatterns {
		value = pattern.ReplaceAllString(value, "[REDACTED]")
	}
	const maxText = 32768
	if len(value) > maxText {
		value = value[:maxText] + "\n[TRUNCATED]"
	}
	return value
}

func sanitizeModel(value string) string {
	if len(value) > 256 {
		value = value[:256]
	}
	return unsafeModel.ReplaceAllString(value, "_")
}

func messageText(message *transcriptMessage) string {
	if message == nil || len(message.Content) == 0 {
		return ""
	}
	var text string
	if json.Unmarshal(message.Content, &text) == nil {
		return text
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(message.Content, &blocks) != nil {
		return ""
	}
	texts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block.Type == "text" && block.Text != "" {
			texts = append(texts, block.Text)
		}
	}
	return strings.Join(texts, "\n\n")
}

func marshalPortableEntry(entry transcriptEntry) ([]byte, error) {
	version := entry.Version
	if version == 0 {
		version = 1
	}
	portable := portableTranscriptEntry{
		Version:       version,
		Type:          entry.Type,
		Timestamp:     entry.Timestamp,
		Model:         entry.Model,
		Name:          entry.Name,
		Status:        entry.Status,
		ModifiedFiles: entry.ModifiedFiles,
	}
	switch entry.Type {
	case "user":
		if entry.Content != "" {
			portable.Message = &portableMessage{Content: entry.Content}
		}
	case "assistant":
		if entry.Content != "" {
			portable.Message = &portableMessage{Content: []portableAssistantBlock{{Type: "text", Text: entry.Content}}}
		}
	case "tool":
		portable.Type = "assistant"
		portable.HermesType = "tool"
		portable.Message = &portableMessage{Content: []portableAssistantBlock{{
			Type:  "tool_use",
			Name:  entry.Name,
			Input: map[string]any{"modified_files": entry.ModifiedFiles},
		}}}
	}
	return json.Marshal(portable)
}

func transcriptFromLine(data []byte, offset int) []byte {
	start := 0
	for range offset {
		next := bytes.IndexByte(data[start:], '\n')
		if next < 0 {
			return nil
		}
		start += next + 1
	}
	return data[start:]
}

func transcriptFileLineCount(path string) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	count := 0
	for scanner.Scan() {
		count++
	}
	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("count Hermes observer transcript lines: %w", err)
	}
	return count, nil
}

func readEntries(path string, offset int) ([]transcriptEntry, []byte, error) {
	resolved, err := resolveTranscriptPath(path, false)
	if err != nil {
		return nil, nil, err
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return nil, nil, err
	}
	return parseEntries(data, offset)
}

func isObserverTranscript(entries []transcriptEntry) bool {
	if len(entries) == 0 {
		return false
	}
	for _, entry := range entries {
		switch entry.Type {
		case "session_start", "user", "assistant", "tool", "session_end":
		default:
			return false
		}
	}
	return true
}

func parseEntries(data []byte, offset int) ([]transcriptEntry, []byte, error) {
	if offset < 0 {
		return nil, nil, fmt.Errorf("offset must not be negative: %d", offset)
	}
	rawSelected := transcriptFromLine(data, offset)
	scanner := bufio.NewScanner(bytes.NewReader(rawSelected))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	entries := make([]transcriptEntry, 0)
	var sanitized bytes.Buffer
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var entry transcriptEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		if entry.HermesType != "" {
			entry.Type = entry.HermesType
		}
		if entry.Content == "" {
			entry.Content = messageText(entry.Message)
		}
		entry.Content = sanitizeText(entry.Content)
		entry.Model = sanitizeModel(entry.Model)
		entry.ModifiedFiles = sanitizeModifiedFiles(entry.ModifiedFiles)
		encoded, err := marshalPortableEntry(entry)
		if err != nil {
			return nil, nil, fmt.Errorf("encode sanitized Hermes observer entry: %w", err)
		}
		entries = append(entries, entry)
		sanitized.Write(encoded)
		sanitized.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("scan Hermes observer transcript: %w", err)
	}
	return entries, sanitized.Bytes(), nil
}

// resolveTranscriptPath allows production access only below the explicit
// observer transcript root. Read-only test fixtures are limited to the
// adapter-owned testdata directory next to the executable. It never treats an
// arbitrary existing absolute path as a transcript.
func resolveTranscriptPath(path string, write bool) (string, error) {
	if strings.TrimSpace(path) == "" || strings.IndexByte(path, 0) >= 0 {
		return "", errTranscriptPathNotOwned
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", errTranscriptPathNotOwned
	}
	candidate, err := canonicalContainmentPath(abs)
	if err != nil {
		return "", errTranscriptPathNotOwned
	}

	home, homeErr := explicitHome()
	if homeErr == nil {
		root, rootErr := canonicalContainmentPath(filepath.Join(home, "entire", "transcripts"))
		if rootErr == nil && pathWithin(root, candidate) {
			return candidate, nil
		}
	}
	if write {
		return "", errTranscriptPathNotOwned
	}

	testdataRoot, rootErr := executableTestdataRoot()
	if rootErr != nil {
		return "", errTranscriptPathNotOwned
	}
	if pathWithin(testdataRoot, candidate) {
		return candidate, nil
	}
	// The shared compliance runner resolves fixture-relative paths against its
	// own working directory. Remap only a testdata-marked request to a file
	// that actually exists in this adapter's owned testdata directory.
	if hasPathComponent(path, "testdata") {
		fixture, fixtureErr := canonicalContainmentPath(filepath.Join(testdataRoot, filepath.Base(path)))
		if fixtureErr == nil && pathWithin(testdataRoot, fixture) {
			if info, statErr := os.Stat(fixture); statErr == nil && !info.IsDir() {
				return fixture, nil
			}
		}
	}
	return "", errTranscriptPathNotOwned
}

func executableTestdataRoot() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	return canonicalContainmentPath(filepath.Join(filepath.Dir(executable), "testdata"))
}

func canonicalContainmentPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	probe := abs
	var suffix []string
	for {
		resolved, resolveErr := filepath.EvalSymlinks(probe)
		if resolveErr == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return filepath.Clean(resolved), nil
		}
		if !errors.Is(resolveErr, os.ErrNotExist) {
			return "", resolveErr
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return abs, nil
		}
		suffix = append(suffix, filepath.Base(probe))
		probe = parent
	}
}

func pathWithin(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func hasPathComponent(path, component string) bool {
	for _, part := range strings.FieldsFunc(filepath.Clean(path), func(r rune) bool {
		return r == '/' || r == '\\'
	}) {
		if part == component {
			return true
		}
	}
	return false
}

func sanitizeModifiedFiles(values []string) []string {
	files := make([]string, 0, len(values))
	for _, value := range values {
		value = filepath.ToSlash(filepath.Clean(value))
		if value == "." || filepath.IsAbs(value) || value == ".." || strings.HasPrefix(value, "../") {
			continue
		}
		if strings.HasPrefix(value, ".git/") || strings.HasPrefix(value, ".entire/") {
			continue
		}
		if sensitiveTranscriptPath.MatchString(value) {
			continue
		}
		if len(value) > 4096 {
			value = value[:4096]
		}
		files = append(files, value)
	}
	return uniqueSorted(files)
}

func (a *Agent) ExtractModifiedFiles(path string, offset int) ([]string, int, error) {
	entries, _, err := readEntries(path, offset)
	if errors.Is(err, errTranscriptPathNotOwned) {
		return []string{}, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	position, err := a.GetTranscriptPosition(path)
	if err != nil {
		return nil, 0, err
	}
	var files []string
	for _, entry := range entries {
		files = append(files, entry.ModifiedFiles...)
	}
	return uniqueSorted(files), position, nil
}

func (a *Agent) ExtractPrompts(path string, offset int) ([]string, error) {
	entries, _, err := readEntries(path, offset)
	if errors.Is(err, errTranscriptPathNotOwned) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	prompts := make([]string, 0)
	for _, entry := range entries {
		if entry.Type == "user" && entry.Content != "" {
			prompts = append(prompts, entry.Content)
		}
	}
	return prompts, nil
}

func (a *Agent) ExtractSummary(path string) (string, bool, error) {
	entries, _, err := readEntries(path, 0)
	if errors.Is(err, errTranscriptPathNotOwned) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Type == "assistant" && entries[i].Content != "" {
			return entries[i].Content, true, nil
		}
	}
	return "", false, nil
}

func uniqueSorted(values []string) []string {
	set := map[string]struct{}{}
	for _, value := range values {
		if value != "" {
			set[value] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
