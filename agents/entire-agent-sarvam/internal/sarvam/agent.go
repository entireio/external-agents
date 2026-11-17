package sarvam

import (
	"crypto/sha256"
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
		ProtocolVersion: protocol.ProtocolVersion,
		Name:            AgentName,
		Type:            AgentType,
		Description:     "Sarvam Code external agent integration for Entire",
		IsPreview:       true,
		ProtectedDirs:   []string{".sarvam"},
		HookNames:       hookNames,
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

// GetSessionDir returns where sarvam keeps the sessions it is writing today.
// The rollout store is date-partitioned (sessions/YYYY/MM/DD/), and callers
// — including Entire's session discovery — read the directory
// non-recursively, so the day directory is the useful answer, not the root.
func (a *Agent) GetSessionDir(repoPath string) (string, error) {
	_ = repoPath // kept for interface symmetry; the store is global
	return filepath.Join(sarvamHome(), "sessions", time.Now().Format("2006/01/02")), nil
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

// WriteSession persists native session bytes at the ref Entire resolved.
// The ref may be the native rollout path itself — ResolveSessionFile returns
// it whenever it exists — so an empty snapshot must never be written: it
// would truncate sarvam's own transcript, which redaction cannot undo. A
// session with no native data is not a snapshot; refuse it.
func (a *Agent) WriteSession(session protocol.AgentSessionJSON) error {
	if len(session.NativeData) == 0 {
		return errEmptySessionData
	}
	ref := strings.TrimSpace(session.SessionRef)
	if ref == "" {
		if strings.TrimSpace(session.SessionID) == "" {
			return errMissingSessionRef
		}
		ref = a.ResolveSessionFile("", session.SessionID)
	}
	if err := os.MkdirAll(filepath.Dir(ref), 0o700); err != nil {
		return err
	}
	return os.WriteFile(ref, session.NativeData, 0o600)
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
