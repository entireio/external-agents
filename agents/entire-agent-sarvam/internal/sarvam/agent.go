package sarvam

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/entireio/external-agents/agents/entire-agent-sarvam/internal/protocol"
)

type Agent struct {
	LookPath func(string) (string, error)
}

func New() *Agent {
	return &Agent{LookPath: exec.LookPath}
}

func (a *Agent) Info() protocol.InfoResponse {
	hookNames := make([]string, 0, len(hookSpecs))
	for _, spec := range hookSpecs {
		hookNames = append(hookNames, spec.HookName)
	}
	return protocol.InfoResponse{
		ProtocolVersion:  protocol.ProtocolVersion,
		Name:             AgentName,
		Type:             AgentType,
		Description:      "Sarvam Code external agent integration for Entire",
		IsPreview:        true,
		TranscriptFormat: "codex",
		ProtectedDirs:    []string{".sarvam"},
		HookNames:        hookNames,
		Capabilities: protocol.DeclaredCapabilities{
			Hooks:              true,
			TranscriptAnalyzer: true,
			CompactTranscript:  true,
			TokenCalculator:    true,
			UsesTerminal:       true,
		},
	}
}

func (a *Agent) Detect() protocol.DetectResponse {
	lookPath := a.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	if _, err := lookPath(sarvamBinary); err == nil {
		return protocol.DetectResponse{Present: true}
	}
	// A sessions store under SARVAM_HOME means the CLI has run before, even
	// when the binary is temporarily off PATH.
	if stat, err := os.Stat(filepath.Join(sarvamHome(), "sessions")); err == nil && stat.IsDir() {
		return protocol.DetectResponse{Present: true}
	}
	return protocol.DetectResponse{Present: false}
}

func (a *Agent) GetSessionID(input *protocol.HookInputJSON) string {
	if input != nil && strings.TrimSpace(input.SessionID) != "" {
		return input.SessionID
	}
	return stubSessionID
}

// sarvamHome resolves $SARVAM_HOME, defaulting to ~/.sarvam.
func sarvamHome() string {
	if v := os.Getenv("SARVAM_HOME"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".sarvam"
	}
	return filepath.Join(home, ".sarvam")
}

// GetSessionDir returns the root of sarvam's session store. The CLI treats
// this directory as the store root and refuses any path resolved outside it
// (ErrOutsideSessionStore), while the store itself is date-partitioned
// (sessions/YYYY/MM/DD/), so the root — the answer the CLI's own Codex agent
// gives for the same layout — is what keeps resolve-session-file's
// whole-tree glob inside the store for sessions from earlier days.
func (a *Agent) GetSessionDir(repoPath string) (string, error) {
	_ = repoPath // kept for interface symmetry; the store is global
	return filepath.Join(sarvamHome(), "sessions"), nil
}

// ResolveSessionFile maps a session ID to its native rollout JSONL when one
// exists, and to a deterministic in-store path otherwise. The fallback keeps
// read/write-session round-trips stable without ever touching a native
// transcript that exists on disk.
func (a *Agent) ResolveSessionFile(sessionDir, sessionID string) string {
	if strings.TrimSpace(sessionDir) == "" {
		sessionDir, _ = a.GetSessionDir("")
	}
	if id := strings.TrimSpace(sessionID); id != "" {
		if path := findRolloutFile(sessionDir, id); path != "" {
			return path
		}
	}
	return filepath.Join(sessionDir, safeFilename(sessionID)+".jsonl")
}

// findRolloutFile searches for the rollout file whose name ends with the
// thread ID, returning the newest match. The given directory is searched
// first — both directly (the day directory GetSessionDir returns) and
// date-partitioned three levels deep (the sessions root older callers pass) —
// and then the whole sessions store, because a session still running from an
// earlier day lives outside today's day directory and must not fall back to
// the synthetic path.
func findRolloutFile(sessionDir, sessionID string) string {
	safe := safeFilename(sessionID)
	if safe == "" || safe == stubSessionID {
		return ""
	}
	patterns := []string{
		filepath.Join(sessionDir, "rollout-*-"+safe+".jsonl"),
		filepath.Join(sessionDir, "*", "*", "*", "rollout-*-"+safe+".jsonl"),
	}
	if root := filepath.Join(sarvamHome(), "sessions"); root != sessionDir {
		patterns = append(patterns, filepath.Join(root, "*", "*", "*", "rollout-*-"+safe+".jsonl"))
	}
	var matches []string
	for _, pattern := range patterns {
		found, err := filepath.Glob(pattern)
		if err == nil {
			matches = append(matches, found...)
		}
	}
	if len(matches) == 0 {
		return ""
	}
	sort.Strings(matches) // date-prefixed names sort chronologically
	return matches[len(matches)-1]
}

func (a *Agent) FormatResumeCommand(sessionID string) string {
	if strings.TrimSpace(sessionID) == "" {
		return "sarvam-code resume --last"
	}
	return "sarvam-code resume " + shellQuote(sessionID)
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if strings.IndexFunc(value, func(r rune) bool {
		return !isSafeShellQuoteRune(r)
	}) == -1 {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func isSafeShellQuoteRune(r rune) bool {
	return r == '-' || r == '_' || r == '.' || r == ':' ||
		(r >= '0' && r <= '9') ||
		(r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z')
}

// safeFilename maps a session ID onto a filename-safe string. Session IDs are
// UUIDs in practice, but the protocol accepts any string: common safe
// punctuation is preserved so the ID stays greppable in resolved paths, and
// anything hostile — including Windows reserved device names, which are not
// valid files there even with an extension — collapses to a hash instead of
// reaching the filesystem. Colons are not safe punctuation: they are path
// separators or ADS markers on Windows.
func safeFilename(sessionID string) string {
	original := strings.TrimSpace(sessionID)
	if original == "" {
		return stubSessionID
	}
	var b strings.Builder
	for _, r := range original {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "._")
	if out == "" {
		return stubSessionID
	}
	if out == original && !isWindowsDeviceName(out) {
		return out
	}
	sum := sha256.Sum256([]byte(original))
	return fmt.Sprintf("~%x", sum[:16])
}

// isWindowsDeviceName reports whether the (already sanitized) name collides
// with a reserved DOS device: CON, PRN, AUX, NUL, COM1-9, LPT1-9,
// case-insensitively. Writing "<name>.jsonl" against these is not a real
// file on Windows, so they always take the hashed form.
func isWindowsDeviceName(name string) bool {
	upper := strings.ToUpper(name)
	switch upper {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}
	if len(upper) == 4 {
		switch {
		case strings.HasPrefix(upper, "COM") && upper[3] >= '1' && upper[3] <= '9':
			return true
		case strings.HasPrefix(upper, "LPT") && upper[3] >= '1' && upper[3] <= '9':
			return true
		}
	}
	return false
}

// sessionIDAndRef picks the session reference for a read: an explicit ref
// wins, then the native rollout for the ID, then the deterministic path.
func (a *Agent) sessionIDAndRef(input *protocol.HookInputJSON) (string, string) {
	sessionID := stubSessionID
	if input != nil && strings.TrimSpace(input.SessionID) != "" {
		sessionID = input.SessionID
	}
	if input != nil && strings.TrimSpace(input.SessionRef) != "" {
		return sessionID, input.SessionRef
	}
	return sessionID, a.ResolveSessionFile("", sessionID)
}

// ReadSession rebuilds the AgentSession envelope from the transcript at the
// session ref. Native data is the raw rollout JSONL; modified files are
// derived from it on the fly.
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

// WriteSession persists native session bytes at the ref Entire resolved —
// but never over sarvam's own rollout store. A native rollout that exists
// is sarvam's append-only transcript; replacing it with an entire-captured
// snapshot would truncate live appends or clobber history, which redaction
// cannot undo, so rollout-shaped targets are refused when present. A
// rollout-shaped ref that does not exist is the restore case — Entire
// recreating a captured snapshot on a fresh clone — and is created once,
// exclusively, so a native rollout appearing concurrently still wins. The
// ref the CLI's restore resolves on a fresh clone is not rollout-shaped,
// though: it is the deterministic fallback (<dir>/<id>.jsonl), and written
// verbatim it would be a file sarvam-code resume never discovers (resume
// scans for rollout-*-<id>.jsonl), so that exact ref is redirected to the
// native-shaped path derived from the snapshot's own session_meta. An empty
// payload is not a snapshot at all, and an empty ref resolves to the
// deterministic store path rather than ResolveSessionFile, which prefers
// the native file.
func (a *Agent) WriteSession(session protocol.AgentSessionJSON) error {
	if len(session.NativeData) == 0 {
		return errEmptySessionData
	}
	ref := strings.TrimSpace(session.SessionRef)
	if ref == "" {
		if strings.TrimSpace(session.SessionID) == "" {
			return errMissingSessionRef
		}
		ref = a.storePath(session.SessionID)
	}
	if !isNativeRolloutPath(ref) && isDeterministicFallbackRef(ref, session.SessionID) {
		if restored := restoredRolloutRef(ref, session.SessionID, session.NativeData); restored != "" {
			ref = restored
		}
	}
	if isNativeRolloutPath(ref) {
		if err := os.MkdirAll(filepath.Dir(ref), 0o700); err != nil {
			return err
		}
		f, err := os.OpenFile(ref, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			if os.IsExist(err) {
				return errNativeRolloutWrite
			}
			return err
		}
		if _, err := f.Write(session.NativeData); err != nil {
			_ = f.Close()
			// A failed restore must not leave a partial file masquerading
			// as a native rollout.
			_ = os.Remove(ref)
			return err
		}
		if err := f.Close(); err != nil {
			_ = os.Remove(ref)
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(ref), 0o700); err != nil {
		return err
	}
	return os.WriteFile(ref, session.NativeData, 0o600)
}

// storePath is the deterministic in-store location for a session ID. It
// deliberately bypasses ResolveSessionFile, which returns the native
// rollout whenever one exists.
func (a *Agent) storePath(sessionID string) string {
	dir, _ := a.GetSessionDir("")
	return filepath.Join(dir, safeFilename(sessionID)+".jsonl")
}

// isDeterministicFallbackRef reports whether ref is the <dir>/<id>.jsonl
// fallback resolve-session-file predicts when no rollout exists — the shape
// the CLI's restore resolves on a fresh clone. Only that ref is redirected
// to a discoverable path; anything else a caller names is written where it
// says.
func isDeterministicFallbackRef(ref, sessionID string) bool {
	sessionID = strings.TrimSpace(sessionID)
	return sessionID != "" && filepath.Base(ref) == safeFilename(sessionID)+".jsonl"
}

// restoredRolloutRef derives the native rollout path a restore should land
// at: the snapshot's own session_meta timestamp picks the date directory and
// the file name, so sarvam-code resume — which scans for
// rollout-*-<id>.jsonl under sessions/YYYY/MM/DD/ — discovers the restored
// session. This mirrors the CLI Codex agent's restoredRolloutPath for the
// same store layout. Returns "" when the snapshot carries no parseable
// session_meta; the caller then writes the ref as given.
func restoredRolloutRef(ref, sessionID string, nativeData []byte) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ""
	}
	first := nativeData
	if i := bytes.IndexByte(nativeData, '\n'); i >= 0 {
		first = first[:i]
	}
	var meta rolloutLine
	if json.Unmarshal(bytes.TrimSpace(first), &meta) != nil || meta.Type != "session_meta" {
		return ""
	}
	start, err := time.Parse(time.RFC3339, strings.TrimSpace(meta.Timestamp))
	if err != nil {
		return ""
	}
	start = start.UTC()
	dayPath := filepath.Join(filepath.Dir(ref), start.Format("2006"), start.Format("01"), start.Format("02"))
	name := fmt.Sprintf("rollout-%s-%s.jsonl", start.Format("2006-01-02T15-04-05"), safeFilename(sessionID))
	return filepath.Join(dayPath, name)
}

// isNativeRolloutPath reports whether the path names one of sarvam's own
// rollout transcripts (rollout-<timestamp>-<thread>.jsonl), which write
// targets must never touch.
func isNativeRolloutPath(path string) bool {
	base := filepath.Base(path)
	return strings.HasPrefix(base, "rollout-") && strings.HasSuffix(base, ".jsonl")
}

// ReadTranscript returns the raw transcript bytes at the ref. Entire runs its
// own redaction over the result and hands a temp copy back through
// compact-transcript for the Entire-facing projection.
func (a *Agent) ReadTranscript(sessionRef string) ([]byte, error) {
	return os.ReadFile(sessionRef)
}

func (a *Agent) ChunkTranscript(content []byte, maxSize int) ([][]byte, error) {
	if maxSize <= 0 {
		return nil, fmt.Errorf("max-size must be positive, got %d", maxSize)
	}
	if len(content) == 0 {
		return [][]byte{{}}, nil
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
	total := 0
	for _, chunk := range chunks {
		total += len(chunk)
	}
	out := make([]byte, 0, total)
	for _, chunk := range chunks {
		out = append(out, chunk...)
	}
	return out, nil
}
