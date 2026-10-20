package aider

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/entireio/external-agents/agents/entire-agent-aider/internal/protocol"
)

// GetSessionID persists randomness, never a launch-time commit SHA. A short-lived
// exclusive lock prevents simultaneous invocations from returning different IDs.
func (*Agent) GetSessionID(_ *protocol.HookInput) (string, error) {
	dir := filepath.Join(RepoRoot(), ".entire", "tmp")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "aider-session")
	lockPath := path + ".lock"
	deadline := time.Now().Add(3 * time.Second)
	for {
		lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			if err := lock.Close(); err != nil {
				_ = os.Remove(lockPath)
				return "", err
			}
			defer os.Remove(lockPath)
			break
		}
		// Windows can report access denied while a competing lock file is
		// pending deletion. Retry it like contention, but bound the wait.
		deleting := runtime.GOOS == "windows" && errors.Is(err, os.ErrPermission)
		if !errors.Is(err, os.ErrExist) && !deleting {
			return "", err
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("cannot acquire session ID lock %s: %w", lockPath, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	data, err := os.ReadFile(path)
	if err == nil && strings.TrimSpace(string(data)) != "" {
		return strings.TrimSpace(string(data)), nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	id := hex.EncodeToString(random[:])
	if err := os.WriteFile(path, []byte(id+"\n"), 0o600); err != nil {
		return "", err
	}
	return id, nil
}

func (*Agent) GetSessionDir(repo string) (string, error) {
	if repo == "" {
		repo = RepoRoot()
	}
	return filepath.Abs(repo)
}
func (*Agent) ResolveSessionFile(dir, _ string) string {
	if dir == "" {
		dir = RepoRoot()
	}
	return filepath.Join(dir, HistoryFile)
}
func (a *Agent) ReadSession(input *protocol.HookInput) (protocol.Session, error) {
	if input == nil {
		input = &protocol.HookInput{}
	}
	id, err := a.GetSessionID(input)
	if err != nil {
		return protocol.Session{}, err
	}
	ref := input.SessionRef
	if ref == "" {
		ref = a.ResolveSessionFile(RepoRoot(), id)
	}
	data, err := a.ReadTranscript(ref)
	if err != nil {
		return protocol.Session{}, err
	}
	return protocol.Session{SessionID: id, AgentName: AgentName, RepoPath: RepoRoot(), SessionRef: ref, StartTime: input.Timestamp, NativeData: data, ModifiedFiles: []string{}, NewFiles: []string{}, DeletedFiles: []string{}}, nil
}
func (a *Agent) WriteSession(session protocol.Session) error {
	if len(session.NativeData) == 0 {
		return errors.New("refusing to overwrite Aider history with empty session data")
	}
	ref := session.SessionRef
	if ref == "" {
		ref = a.ResolveSessionFile(session.RepoPath, session.SessionID)
	}
	if err := os.MkdirAll(filepath.Dir(ref), 0o700); err != nil {
		return err
	}
	return os.WriteFile(ref, session.NativeData, 0o600)
}
