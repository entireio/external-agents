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
	// The CLI itself is enough: sessions and transcripts live under
	// SARVAM_HOME even when the binary is temporarily off PATH.
	if stat, err := os.Stat(sarvamHome()); err == nil && stat.IsDir() {
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

// GetSessionDir returns the root of sarvam's session store. Sessions are
// global, not repo-scoped: one JSONL per run under sessions/YYYY/MM/DD/.
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

// findRolloutFile searches the YYYY/MM/DD tree for the rollout file whose
// name ends with the thread ID, returning the newest match.
func findRolloutFile(sessionDir, sessionID string) string {
	safe := safeFilename(sessionID)
	if safe == "" || safe == stubSessionID {
		return ""
	}
	pattern := filepath.Join(sessionDir, "*", "*", "*", "rollout-*-"+safe+".jsonl")
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
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
// anything hostile collapses to a hash instead of reaching the filesystem.
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
		case r == '-' || r == '_' || r == '.' || r == ':':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "._")
	if out == "" {
		return stubSessionID
	}
	if out == original {
		return out
	}
	sum := sha256.Sum256([]byte(original))
	return fmt.Sprintf("~%x", sum[:16])
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

// WriteSession persists native session bytes at the ref Entire resolved. The
// native rollout for the ID is never a write target: ResolveSessionFile only
// returns it when it already exists, and Entire passes the ref it asked us to
// resolve, which for capture flows is the deterministic store path.
func (a *Agent) WriteSession(session protocol.AgentSessionJSON) error {
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
