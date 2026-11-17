package vibe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/entireio/external-agents/agents/entire-agent-mistral-vibe/internal/protocol"
)

const (
	// fetchTimeout is for prepare-transcript, which Entire calls outside Vibe's
	// hook budget. The hook timeouts are shorter: Vibe stops hooks five seconds
	// after it quits.
	fetchTimeout           = 20 * time.Second
	hookFetchTimeout       = 8 * time.Second
	sessionEndFetchTimeout = 1500 * time.Millisecond
	// freshWindow lets prepare-transcript skip a fetch when the turn-end hook
	// stored the transcript a moment ago.
	freshWindow = 3 * time.Second
)

// refresh fetches the session from Vibe and stores it at sessionRef as one
// history entry per line. Hook notices are left out. It never replaces a
// stored transcript with an empty one.
func (a *Agent) refresh(sessionRef, sessionID string, timeout time.Duration) (*Snapshot, error) {
	if strings.TrimSpace(sessionRef) == "" || strings.TrimSpace(sessionID) == "" {
		return nil, errors.New("session id and session ref are required")
	}
	fetcher := a.Fetcher
	if fetcher == nil {
		fetcher = &AppServerFetcher{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	snapshot, err := fetcher.FetchSession(ctx, sessionID, protocol.RepoRoot())
	if err != nil {
		return nil, err
	}

	existing, _ := os.ReadFile(sessionRef)
	lines, err := buildLines(snapshot, existing)
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return snapshot, nil
	}
	if err := os.MkdirAll(filepath.Dir(sessionRef), 0o700); err != nil {
		return nil, err
	}
	if err := atomicWriteFile(sessionRef, encodeLines(lines), 0o600); err != nil {
		return nil, err
	}
	return snapshot, nil
}

// buildLines turns a snapshot into transcript lines. The last line gets the
// current usage and model; lines that carried a snapshot in the previous
// transcript keep it, so per-turn token deltas survive a refresh.
func buildLines(snapshot *Snapshot, existing []byte) ([]json.RawMessage, error) {
	previous := map[string]json.RawMessage{}
	for line := range bytes.SplitSeq(existing, []byte("\n")) {
		var probe struct {
			ID   string          `json:"id"`
			Meta json.RawMessage `json:"entire"`
		}
		if json.Unmarshal(line, &probe) == nil && probe.ID != "" && len(probe.Meta) > 0 {
			previous[probe.ID] = probe.Meta
		}
	}

	type kept struct {
		id     string
		fields map[string]json.RawMessage
	}
	entries := make([]kept, 0, len(snapshot.Entries))
	for _, raw := range snapshot.Entries {
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil {
			continue
		}
		var id, entryType string
		_ = json.Unmarshal(fields["id"], &id)
		_ = json.Unmarshal(fields["type"], &entryType)
		if entryType == EntryNotice || entryType == "" {
			continue
		}
		// Never let a history entry supply its own snapshot.
		delete(fields, metaKey)
		if meta, ok := previous[id]; ok {
			fields[metaKey] = meta
		}
		entries = append(entries, kept{id: id, fields: fields})
	}

	if len(entries) > 0 && (snapshot.Usage != nil || snapshot.Model != "") {
		meta, err := json.Marshal(EntryMeta{Usage: snapshot.Usage, Model: snapshot.Model})
		if err != nil {
			return nil, err
		}
		entries[len(entries)-1].fields[metaKey] = meta
	}

	lines := make([]json.RawMessage, 0, len(entries))
	for _, entry := range entries {
		line, err := json.Marshal(entry.fields)
		if err != nil {
			return nil, err
		}
		lines = append(lines, line)
	}
	return lines, nil
}

// PrepareTranscript brings the stored transcript up to date. A failed refresh
// is an error only when there is nothing stored: an older transcript is still
// worth checkpointing, and Vibe may simply not be installed on this machine.
func (a *Agent) PrepareTranscript(sessionRef string) error {
	if strings.TrimSpace(sessionRef) == "" {
		return errors.New("session_ref is required")
	}
	info, statErr := os.Stat(sessionRef)
	hasContent := statErr == nil && info.Size() > 0
	if hasContent && time.Since(info.ModTime()) < freshWindow {
		return nil
	}
	sessionID := sessionIDFromRef(sessionRef)
	if sessionID == "" {
		return nil
	}
	if _, err := a.refresh(sessionRef, sessionID, fetchTimeout); err != nil {
		if hasContent {
			fmt.Fprintf(os.Stderr, "entire-agent-%s: transcript not refreshed: %v\n", agentName, err)
			return nil
		}
		return fmt.Errorf("refresh transcript: %w", err)
	}
	return nil
}

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
	entries, err := decodeTranscript(data)
	if err != nil {
		return protocol.AgentSessionJSON{}, err
	}
	if sessionID == "" {
		for _, entry := range entries {
			if entry.SessionID != "" {
				sessionID = entry.SessionID
				break
			}
		}
	}
	if sessionID == "" {
		sessionID = sessionIDFromRef(sessionRef)
	}
	return protocol.AgentSessionJSON{
		SessionID:     sessionID,
		AgentName:     agentName,
		RepoPath:      protocol.RepoRoot(),
		SessionRef:    sessionRef,
		StartTime:     startTime(entries).Format(time.RFC3339),
		NativeData:    data,
		ModifiedFiles: modifiedFiles(entries),
		NewFiles:      []string{},
		DeletedFiles:  []string{},
	}, nil
}

func startTime(entries []Entry) time.Time {
	for _, entry := range entries {
		if t := entry.createdTime(); !t.IsZero() {
			return t
		}
	}
	return time.Now().UTC()
}

// WriteSession restores Entire's copy of the transcript. It does not recreate a
// Vibe session, so `vibe --resume` only works where Vibe still has the original.
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
	return os.ReadFile(sessionRef)
}

// ChunkTranscript splits a transcript into pieces of at most maxSize bytes, on
// line boundaries, so each chunk is itself valid JSONL and no entry or UTF-8
// character is cut in two. A single line longer than maxSize has no boundary
// to use and is cut at maxSize. Concatenating the chunks gives back the input
// byte for byte.
func (a *Agent) ChunkTranscript(content []byte, maxSize int) ([][]byte, error) {
	if maxSize <= 0 {
		return nil, fmt.Errorf("max-size must be positive, got %d", maxSize)
	}
	chunks := [][]byte{}
	start := 0 // start of the chunk being built
	for pos := 0; pos < len(content); {
		lineEnd := len(content)
		if i := bytes.IndexByte(content[pos:], '\n'); i >= 0 {
			lineEnd = pos + i + 1
		}
		if lineEnd-start <= maxSize {
			pos = lineEnd
			continue
		}
		if pos > start {
			// The line does not fit after what is already in the chunk.
			chunks = append(chunks, content[start:pos])
			start = pos
			continue
		}
		// One line on its own is too long.
		chunks = append(chunks, content[start:start+maxSize])
		start += maxSize
		pos = start
	}
	if start < len(content) {
		chunks = append(chunks, content[start:])
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

func readEntries(path string) ([]Entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return decodeTranscript(data)
}

// GetTranscriptPosition returns the number of stored entries. Positions are
// entry (line) indexes, the unit Entire uses to scope a checkpoint.
func (a *Agent) GetTranscriptPosition(path string) (int, error) {
	entries, err := readEntries(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	return len(entries), nil
}

func (a *Agent) ExtractModifiedFiles(path string, offset int) ([]string, int, error) {
	entries, err := readEntries(path)
	if err != nil {
		return nil, 0, err
	}
	return modifiedFiles(fromOffset(entries, offset)), len(entries), nil
}

func (a *Agent) ExtractPrompts(sessionRef string, offset int) ([]string, error) {
	entries, err := readEntries(sessionRef)
	if err != nil {
		return nil, err
	}
	prompts := []string{}
	for _, entry := range fromOffset(entries, offset) {
		if entry.Type != EntryMessage || entry.Role != RoleUser {
			continue
		}
		if text := entry.text(); text != "" {
			prompts = append(prompts, text)
		}
	}
	return prompts, nil
}

func (a *Agent) ExtractSummary(sessionRef string) (string, bool, error) {
	entries, err := readEntries(sessionRef)
	if err != nil {
		return "", false, err
	}
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		if entry.Type != EntryMessage || entry.Role != RoleAssistant {
			continue
		}
		if text := entry.text(); text != "" {
			return text, true, nil
		}
	}
	return "", false, nil
}

// CalculateTokens reports usage for the entries from offset on. Vibe only
// exposes a running session total, so the figure is the difference between
// the last snapshot in range and the last snapshot before it. Snapshots are
// taken when a turn ends, so the range lines up with turns.
func (a *Agent) CalculateTokens(data []byte, offset int) (protocol.TokenUsageResponse, error) {
	entries, err := decodeTranscript(data)
	if err != nil {
		return protocol.TokenUsageResponse{}, err
	}
	if offset < 0 {
		offset = 0
	}
	var before, after *TokenUsage
	usage := protocol.TokenUsageResponse{APICallCount: modelCalls(fromOffset(entries, offset))}
	for i, entry := range entries {
		if entry.Meta == nil || entry.Meta.Usage == nil {
			continue
		}
		if i < offset {
			before = entry.Meta.Usage
		} else {
			after = entry.Meta.Usage
		}
	}
	if after == nil {
		return usage, nil
	}
	if before == nil {
		before = &TokenUsage{}
	}
	cached := max(after.CachedInputTokens-before.CachedInputTokens, 0)
	input := max(after.InputTokens-before.InputTokens, 0)
	usage.CacheReadTokens = cached
	usage.InputTokens = max(input-cached, 0)
	usage.OutputTokens = max(after.OutputTokens-before.OutputTokens, 0)
	return usage, nil
}

// modelCalls estimates how many model responses produced these entries. Vibe's
// history has no request count, so this groups entries by the response they
// came from: reasoning and reply entries carry a generation id, and the tool
// calls of one response sit together. Parallel tool calls therefore count
// once, not once each.
func modelCalls(entries []Entry) int {
	calls := 0
	generation := ""
	afterCallStart := false // the previous entry already belongs to a counted call
	for _, entry := range entries {
		switch {
		case entry.Type == EntryReasoning, entry.Type == EntryMessage && entry.Role == RoleAssistant:
			if id := generationID(entry); id == "" || id != generation {
				calls++
				generation = id
			}
			afterCallStart = true
		case entry.Type == EntryEffect:
			// Tool calls with no reasoning before them are a response of their own.
			if !afterCallStart {
				calls++
				generation = ""
			}
			afterCallStart = true
		default:
			afterCallStart = false
		}
	}
	return calls
}

var generationSuffix = regexp.MustCompile(`-\d+$`)

// generationID extracts the response id shared by "reasoning-<id>-<n>" and
// "assistant-<id>".
func generationID(entry Entry) string {
	if id, ok := strings.CutPrefix(entry.ID, "reasoning-"); ok {
		return generationSuffix.ReplaceAllString(id, "")
	}
	if id, ok := strings.CutPrefix(entry.ID, "assistant-"); ok {
		return id
	}
	return ""
}

func fromOffset(entries []Entry, offset int) []Entry {
	if offset < 0 {
		offset = 0
	}
	if offset >= len(entries) {
		return nil
	}
	return entries[offset:]
}

func modifiedFiles(entries []Entry) []string {
	root := protocol.RepoRoot()
	seen := map[string]bool{}
	for _, entry := range entries {
		if !isFileChange(entry) {
			continue
		}
		var paths []string
		if entry.State != nil {
			paths = append(paths, pathsFromJSON(entry.State.Output)...)
		}
		if len(paths) == 0 {
			paths = pathsFromJSON(entry.Detail.Input)
		}
		for _, path := range paths {
			if path = repoRelative(root, path); path != "" {
				seen[path] = true
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

// isFileChange reports whether an entry is a tool call that changed a file and
// was not refused, skipped or failed.
func isFileChange(entry Entry) bool {
	if entry.Type != EntryEffect || entry.Detail == nil {
		return false
	}
	if entry.State != nil {
		switch entry.State.Status {
		case "failed", "cancelled", "skipped", "blocked":
			return false
		}
	}
	switch entry.Detail.Kind {
	case "file_write", "file_edit", "file_change":
		return true
	}
	name := entry.Detail.ToolName
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	}
	switch strings.ToLower(name) {
	case "write_file", "search_replace", "edit", "edit_file", "apply_patch", "delete_file", "move_file":
		return true
	}
	return false
}

var filePathKeys = []string{"filePath", "file_path", "path"}

func pathsFromJSON(raw json.RawMessage) []string {
	var obj map[string]json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &obj) != nil {
		return nil
	}
	var paths []string
	for _, key := range filePathKeys {
		var value string
		if json.Unmarshal(obj[key], &value) == nil && strings.TrimSpace(value) != "" {
			paths = append(paths, strings.TrimSpace(value))
		}
	}
	return paths
}

// repoRelative returns path relative to the repository, or "" when it is not
// inside it. Entire reads these as repo-relative paths, so a file the agent
// wrote elsewhere (/tmp/x, ~/.bashrc) must not be reported at all.
func repoRelative(root, path string) string {
	if !filepath.IsAbs(path) {
		// Vibe's tools take paths relative to the working directory.
		return insideRepo(filepath.Clean(path))
	}
	if root == "" {
		return ""
	}
	for _, base := range []string{root, resolved(root)} {
		for _, candidate := range []string{path, resolved(path)} {
			if rel, err := filepath.Rel(base, candidate); err == nil {
				if inside := insideRepo(rel); inside != "" {
					return inside
				}
			}
		}
	}
	return ""
}

// insideRepo returns a cleaned relative path in slash form, or "" when it is
// the repository itself or leaves it. Only a ".." path element leaves the
// repository; a name such as "..foo" does not.
func insideRepo(rel string) string {
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	return filepath.ToSlash(rel)
}

// resolved follows symlinks in the longest existing prefix of path, so a file
// under /var and a root under /private/var still compare as inside.
func resolved(path string) string {
	if target, err := filepath.EvalSymlinks(path); err == nil {
		return target
	}
	dir, file := filepath.Split(filepath.Clean(path))
	if dir == "" || dir == path {
		return path
	}
	return filepath.Join(resolved(filepath.Clean(dir)), file)
}
