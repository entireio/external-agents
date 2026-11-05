package dsh

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/entireio/external-agents/agents/entire-agent-dsh/internal/protocol"
)

// Agent implements the Entire Agent Integration Protocol for DeepSeek Harness
// (DSH). DSH has no repository-local hook file: the @sredevopsorg/dsh-entire
// Host plugin forwards lifecycle events to `entire hooks dsh`, and this binary
// normalizes them and materializes the native session log as JSONL.
type Agent struct{}

// New returns a protocol agent for DSH.
func New() *Agent { return &Agent{} }

// agentName is the registry name, which must match the `entire-agent-<name>`
// binary name and the marker the Host plugin reads.
const agentName = "dsh"

// Hook names match the bridge marker and the event names the DSH plugin uses.
const (
	HookSessionStart  = "session-start"
	HookTurnStart     = "turn-start"
	HookTurnEnd       = "turn-end"
	HookCompaction    = "compaction"
	HookSessionEnd    = "session-end"
	HookSubagentStart = "subagent-start"
	HookSubagentEnd   = "subagent-end"
)

var hookNames = []string{
	HookSessionStart,
	HookTurnStart,
	HookTurnEnd,
	HookCompaction,
	HookSessionEnd,
	HookSubagentStart,
	HookSubagentEnd,
}

func (a *Agent) Info() protocol.InfoResponse {
	return protocol.InfoResponse{
		ProtocolVersion: protocol.ProtocolVersion,
		Name:            agentName,
		Type:            "DeepSeek Harness",
		Description:     "DeepSeek Harness (DSH) - terminal and web coding agent",
		IsPreview:       true,
		ProtectedDirs:   nil,
		HookNames:       append([]string(nil), hookNames...),
		Capabilities: protocol.DeclaredCapabilities{
			Hooks:              true,
			TranscriptAnalyzer: true,
			TranscriptPreparer: true,
			TokenCalculator:    true,
			CompactTranscript:  true,
		},
	}
}

// Detect reports whether a DSH installation is discoverable. An explicit
// DSH_HOME is authoritative; otherwise a configured home or a `dsh` binary on
// PATH counts as present.
func (a *Agent) Detect() protocol.DetectResponse {
	if strings.TrimSpace(os.Getenv("DSH_HOME")) != "" {
		return protocol.DetectResponse{Present: true}
	}
	if _, err := exec.LookPath("dsh"); err == nil {
		return protocol.DetectResponse{Present: true}
	}
	if info, err := os.Stat(dshHome()); err == nil && info.IsDir() {
		return protocol.DetectResponse{Present: true}
	}
	return protocol.DetectResponse{Present: false}
}

func (a *Agent) GetSessionID(input *protocol.HookInputJSON) string {
	if input == nil {
		return ""
	}
	if input.SessionID != "" {
		return input.SessionID
	}
	if raw := input.RawData; raw != nil {
		if dsh, ok := raw["dsh"].(map[string]any); ok {
			if id, ok := dsh["session_id"].(string); ok {
				return id
			}
		}
		if id, ok := raw["session_id"].(string); ok {
			return id
		}
	}
	return ""
}

func (a *Agent) ReadSession(input *protocol.HookInputJSON) (protocol.AgentSessionJSON, error) {
	sessionID := a.GetSessionID(input)
	repoRoot := protocol.RepoRoot()

	ref := ""
	if input != nil {
		ref = input.SessionRef
	}
	if ref == "" && sessionID != "" {
		dir, err := a.GetSessionDir(repoRoot)
		if err != nil {
			return protocol.AgentSessionJSON{}, err
		}
		ref = a.ResolveSessionFile(dir, sessionID)
	}
	if ref == "" {
		return protocol.AgentSessionJSON{}, errors.New("session reference is required")
	}

	var data []byte
	var readErr error
	if fileExists(ref) {
		data, readErr = readTranscriptFile(ref)
	} else {
		_, data, readErr = a.materializeTranscript(sessionID, repoRoot, "", materializeOptions{})
	}
	if readErr != nil {
		// A session can be announced before DSH materializes its log. Report
		// the identity rather than failing the hook lifecycle.
		data = nil
	}

	modified, newFiles, deleted := extractFileLists(data, repoRoot)
	startTime := sessionStartTime(data)
	return protocol.AgentSessionJSON{
		SessionID:     sessionID,
		AgentName:     agentName,
		RepoPath:      repoRoot,
		SessionRef:    ref,
		StartTime:     startTime,
		NativeData:    data,
		ModifiedFiles: modified,
		NewFiles:      newFiles,
		DeletedFiles:  deleted,
	}, nil
}

func (a *Agent) WriteSession(session protocol.AgentSessionJSON) error {
	if session.AgentName != "" && session.AgentName != agentName {
		return fmt.Errorf("session belongs to agent %q, not %q", session.AgentName, agentName)
	}
	if strings.TrimSpace(session.SessionRef) == "" {
		return errors.New("session reference is required")
	}
	if len(session.NativeData) == 0 {
		return errors.New("session has no native data to write")
	}
	if !filepath.IsAbs(session.SessionRef) {
		return fmt.Errorf("session reference %s must be absolute", session.SessionRef)
	}
	allowedRoot := filepath.Join(dshHome(), cacheSubdir)
	if strings.TrimSpace(session.RepoPath) != "" {
		dir, err := a.GetSessionDir(session.RepoPath)
		if err != nil {
			return err
		}
		allowedRoot = dir
	}
	if !pathWithin(session.SessionRef, allowedRoot) {
		return fmt.Errorf("session reference %s is outside the DSH session directory %s", session.SessionRef, allowedRoot)
	}
	restoredCWD := session.RepoPath
	if strings.TrimSpace(restoredCWD) != "" {
		restoredCWD = physicalPath(restoredCWD)
	}
	restoredData := rewriteSessionHeader(session.NativeData, session.SessionID, restoredCWD)
	if err := writeFileAtomic(session.SessionRef, restoredData, 0o600); err != nil {
		return err
	}
	if session.SessionID != "" {
		if err := writeSessionIDSidecar(session.SessionRef, session.SessionID); err != nil {
			return err
		}
	}
	// Make the restored transcript visible to DSH itself, so
	// `dsh --profile headless --session-id <id>` actually adopts it. Never
	// overwrite an existing native log; a live DSH session owns that file.
	if err := a.restoreNativeTranscript(session, restoredData); err != nil {
		return fmt.Errorf("restore native DSH session: %w", err)
	}
	return nil
}

// rewriteSessionHeader points a restored transcript at the current checkout.
// A checkpoint captured on another machine or path would otherwise be written
// under the old project key and refused by DSH for a cwd mismatch.
func rewriteSessionHeader(data []byte, id, cwd string) []byte {
	lines := bytes.Split(data, []byte("\n"))
	headerIndex := -1
	for i, line := range lines {
		if len(bytes.TrimSpace(line)) > 0 {
			headerIndex = i
			break
		}
	}
	if headerIndex < 0 {
		return data
	}
	var header map[string]any
	if err := json.Unmarshal(lines[headerIndex], &header); err != nil {
		return data
	}
	if header["type"] != "session" {
		return data
	}
	if id != "" {
		header["id"] = id
	}
	if cwd != "" {
		header["cwd"] = cwd
	}
	encoded, err := json.Marshal(header)
	if err != nil {
		return data
	}
	lines[headerIndex] = encoded
	return bytes.Join(lines, []byte("\n"))
}

// restoreNativeTranscript writes a restored transcript back into DSH's own
// session store so `dsh --session-id` can actually adopt it. It never
// overwrites an existing native generation: a live DSH session owns that file.
func (a *Agent) restoreNativeTranscript(session protocol.AgentSessionJSON, data []byte) error {
	header, err := parseSessionHeader(data)
	if err != nil {
		// Restored data need not be a DSH transcript (compliance fixtures are
		// arbitrary JSON); there is simply nothing native to restore.
		return nil
	}
	sessionID := session.SessionID
	if sessionID == "" {
		sessionID = header.ID
	}
	if sessionID == "" {
		return nil
	}
	cwd := session.RepoPath
	if strings.TrimSpace(cwd) == "" {
		cwd = header.CWD
	}
	if strings.TrimSpace(cwd) == "" {
		return nil
	}
	cwd = physicalPath(cwd)
	if header.Version > 4 {
		return fmt.Errorf("unsupported DSH session format v%d", header.Version)
	}
	encodedID, err := encodeSegment(sessionID)
	if err != nil {
		return err
	}
	dir := filepath.Join(sessionsRoot(), projectKey(cwd), encodedID)
	if hasAnyGeneration(dir) {
		return nil
	}
	lockPath := filepath.Join(dir, ".entire-restore.lock")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	lock, lockErr := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) // #nosec G304 -- directory derived from the restored session
	if lockErr != nil && errors.Is(lockErr, fs.ErrExist) {
		// Recover from a restore killed before its deferred cleanup. Fresh
		// locks are honored; an old one is treated as abandoned.
		if info, statErr := os.Stat(lockPath); statErr == nil && time.Since(info.ModTime()) > 5*time.Minute {
			if removeErr := os.Remove(lockPath); removeErr == nil {
				lock, lockErr = os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) // #nosec G304
			}
		}
		if lockErr != nil {
			return nil
		}
	}
	if lockErr != nil {
		return lockErr
	}
	defer func() {
		_ = lock.Close()
		_ = os.Remove(lockPath)
	}()
	if hasAnyGeneration(dir) {
		return nil
	}
	frames, err := encodeZstdFrames(data)
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dir, nativeGenerationName(header.Version)), frames, 0o600)
}

// hasAnyGeneration reports whether DSH already owns a session generation in
// dir, compressed or plain. The resolver prefers zstd, so creating a zstd
// sibling beside a plain generation would shadow the live one.
func hasAnyGeneration(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if _, _, ok := parseGenerationName(entry.Name()); ok {
			return true
		}
	}
	return false
}

func (a *Agent) ReadTranscript(sessionRef string) ([]byte, error) {
	if strings.TrimSpace(sessionRef) == "" {
		return nil, errors.New("transcript reference is empty")
	}
	if sessionID, ok := sessionIDFromRef(sessionRef); ok {
		_, data, err := a.materializeTranscript(sessionID, protocol.RepoRoot(), "", materializeOptions{})
		if err == nil {
			return data, nil
		}
	}
	return readTranscriptFile(sessionRef)
}

func (a *Agent) ChunkTranscript(content []byte, maxSize int) ([][]byte, error) {
	if maxSize <= 0 {
		return nil, errors.New("max-size must be greater than zero")
	}
	if len(content) == 0 {
		return [][]byte{[]byte{}}, nil
	}
	var chunks [][]byte
	for start := 0; start < len(content); start += maxSize {
		end := start + maxSize
		if end > len(content) {
			end = len(content)
		}
		chunk := make([]byte, end-start)
		copy(chunk, content[start:end])
		chunks = append(chunks, chunk)
	}
	return chunks, nil
}

func (a *Agent) ReassembleTranscript(chunks [][]byte) ([]byte, error) {
	return bytes.Join(chunks, nil), nil
}

func (a *Agent) PrepareTranscript(sessionRef string) error {
	if strings.TrimSpace(sessionRef) == "" {
		return errors.New("transcript reference is empty")
	}
	dir, dirErr := a.GetSessionDir(protocol.RepoRoot())
	inCache := dirErr == nil && pathWithin(sessionRef, dir)
	if !inCache && fileExists(sessionRef) {
		// The protocol allows any regular transcript path here (for example a
		// temp file an agent hands to the CLI). Only refresh files this binary
		// owns; never turn an unrelated session.jsonl into a DSH lookup failure.
		return nil
	}
	if sessionID, ok := sessionIDFromRef(sessionRef); ok {
		_, _, err := a.materializeTranscript(sessionID, protocol.RepoRoot(), "", materializeOptions{})
		if err == nil {
			return nil
		}
		if fileExists(sessionRef) {
			return nil
		}
		return err
	}
	if fileExists(sessionRef) {
		return nil
	}
	return fmt.Errorf("transcript file not found: %s", sessionRef)
}

func (a *Agent) FormatResumeCommand(sessionID string) string {
	// DSH has no interactive --resume flag. Headless is the only shipped app
	// that adopts a persisted session by ID; interactive resume is opening the
	// session in `dsh web`. Only a conservative ID is safe to print as a shell
	// command: session IDs can arrive from hook payloads and are user-visible.
	if !safeCommandArgument(sessionID) {
		return "dsh web"
	}
	return fmt.Sprintf("dsh --profile headless --session-id %s \"continue\"", sessionID)
}

func safeCommandArgument(value string) bool {
	if value == "" || strings.HasPrefix(value, "-") {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func sessionStartTime(data []byte) string {
	if header, err := parseSessionHeader(data); err == nil && header.CreatedAt > 0 {
		return time.UnixMilli(header.CreatedAt).UTC().Format(time.RFC3339)
	}
	return time.Now().UTC().Format(time.RFC3339)
}
