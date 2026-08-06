package hermes

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/entireio/external-agents/agents/entire-agent-hermes/internal/protocol"
)

const agentName = "hermes"

type Agent struct{}

func New() *Agent { return &Agent{} }

func (a *Agent) Info() protocol.InfoResponse {
	return protocol.InfoResponse{
		ProtocolVersion: protocol.ProtocolVersion,
		Name:            agentName,
		Type:            "Hermes Agent",
		Description:     "Hermes Agent integration using a profile-scoped sanitized observer transcript",
		IsPreview:       true,
		ProtectedDirs:   []string{},
		ProtectedFiles:  []string{},
		HookNames:       []string{"on_session_start", "pre_llm_call", "on_session_end", "on_session_finalize"},
		Capabilities:    protocol.DeclaredCapabilities{Hooks: true, TranscriptAnalyzer: true, CompactTranscript: true, UsesTerminal: true},
	}
}

func (a *Agent) Detect() protocol.DetectResponse {
	_, err := exec.LookPath("hermes")
	return protocol.DetectResponse{Present: err == nil}
}

func (a *Agent) GetSessionID(input *protocol.HookInputJSON) string {
	if input == nil {
		return ""
	}
	return input.SessionID
}

func explicitHome() (string, error) {
	home := strings.TrimSpace(os.Getenv("HERMES_HOME"))
	if home == "" {
		return "", errors.New("HERMES_HOME must be set explicitly")
	}
	abs, err := filepath.Abs(home)
	if err != nil {
		return "", fmt.Errorf("resolve HERMES_HOME: %w", err)
	}
	return filepath.Clean(abs), nil
}

func canonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if resolved, resolveErr := filepath.EvalSymlinks(abs); resolveErr == nil {
		return filepath.Clean(resolved), nil
	}
	return filepath.Clean(abs), nil
}

func (a *Agent) GetSessionDir(repoPath string) (string, error) {
	home, err := explicitHome()
	if err != nil {
		return "", err
	}
	if repoPath == "" {
		repoPath = protocol.RepoRoot()
	}
	repoPath, err = canonicalPath(repoPath)
	if err != nil {
		return "", fmt.Errorf("resolve repository path: %w", err)
	}
	sum := sha256.Sum256([]byte(repoPath))
	return filepath.Join(home, "entire", "transcripts", hex.EncodeToString(sum[:8])), nil
}

func (a *Agent) ResolveSessionFile(sessionDir, sessionID string) string {
	sum := sha256.Sum256([]byte(sessionID))
	return filepath.Join(sessionDir, hex.EncodeToString(sum[:])+".jsonl")
}

func (a *Agent) ReadSession(input *protocol.HookInputJSON) (protocol.AgentSessionJSON, error) {
	if input == nil {
		return protocol.AgentSessionJSON{}, errors.New("hook input is required")
	}
	ref := input.SessionRef
	if ref == "" {
		dir, err := a.GetSessionDir(protocol.RepoRoot())
		if err != nil {
			return protocol.AgentSessionJSON{}, err
		}
		ref = a.ResolveSessionFile(dir, input.SessionID)
	}
	entries, data, err := readEntries(ref, 0)
	if err != nil {
		return protocol.AgentSessionJSON{}, err
	}
	start := input.Timestamp
	var files []string
	for _, entry := range entries {
		if start == "" && entry.Timestamp != "" {
			start = entry.Timestamp
		}
		files = append(files, entry.ModifiedFiles...)
	}
	return protocol.AgentSessionJSON{SessionID: input.SessionID, AgentName: agentName, RepoPath: protocol.RepoRoot(), SessionRef: ref, StartTime: start, NativeData: data, ModifiedFiles: uniqueSorted(files), NewFiles: []string{}, DeletedFiles: []string{}}, nil
}

func (a *Agent) WriteSession(session protocol.AgentSessionJSON) error {
	if session.SessionRef == "" {
		return errors.New("session_ref is required")
	}
	path, err := resolveTranscriptPath(session.SessionRef, true)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return atomicWrite(path, session.NativeData, 0o600)
}

func (a *Agent) ReadTranscript(ref string) ([]byte, error) {
	_, data, err := readEntries(ref, 0)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (a *Agent) ChunkTranscript(data []byte, maxSize int) ([][]byte, error) {
	if maxSize <= 0 {
		return nil, fmt.Errorf("max-size must be positive, got %d", maxSize)
	}
	chunks := make([][]byte, 0, (len(data)+maxSize-1)/maxSize)
	for len(data) > 0 {
		n := min(len(data), maxSize)
		chunks = append(chunks, append([]byte(nil), data[:n]...))
		data = data[n:]
	}
	return chunks, nil
}

func (a *Agent) ReassembleTranscript(chunks [][]byte) ([]byte, error) {
	var out []byte
	for _, chunk := range chunks {
		out = append(out, chunk...)
	}
	return out, nil
}

func (a *Agent) FormatResumeCommand(id string) string { return "hermes --resume " + shellQuote(id) }

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }

func (a *Agent) ParseHook(hook string, input []byte) (*protocol.EventJSON, error) {
	if len(strings.TrimSpace(string(input))) == 0 {
		return nil, nil
	}
	var raw struct {
		SessionID  string `json:"session_id"`
		SessionRef string `json:"session_ref"`
		Timestamp  string `json:"timestamp"`
		Prompt     string `json:"prompt"`
		UserPrompt string `json:"user_prompt"`
		Model      string `json:"model"`
	}
	if err := json.Unmarshal(input, &raw); err != nil {
		return nil, err
	}
	if raw.SessionID == "" {
		return nil, nil
	}
	if raw.Timestamp == "" {
		raw.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}
	if raw.Prompt == "" {
		raw.Prompt = raw.UserPrompt
	}
	raw.Prompt = sanitizeText(raw.Prompt)
	raw.Model = sanitizeModel(raw.Model)
	types := map[string]int{"on_session_start": 1, "pre_llm_call": 2, "on_session_end": 3, "on_session_finalize": 5}
	typ := types[hook]
	if typ == 0 {
		return nil, nil
	}
	if raw.SessionRef == "" {
		dir, err := a.GetSessionDir(protocol.RepoRoot())
		if err == nil {
			raw.SessionRef = a.ResolveSessionFile(dir, raw.SessionID)
		}
	}
	return &protocol.EventJSON{Type: typ, SessionID: raw.SessionID, SessionRef: raw.SessionRef, Prompt: raw.Prompt, Model: raw.Model, Timestamp: raw.Timestamp}, nil
}

func (a *Agent) GetTranscriptPosition(path string) (int, error) {
	resolved, err := resolveTranscriptPath(path, false)
	if errors.Is(err, errTranscriptPathNotOwned) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	position, err := transcriptFileLineCount(resolved)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return position, nil
}
