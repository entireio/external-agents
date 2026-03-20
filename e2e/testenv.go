//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestEnv provides an isolated filesystem environment for E2E tests.
type TestEnv struct {
	t       *testing.T
	Dir     string
	HomeDir string
	Runner  *AgentRunner
}

// NewTestEnv creates a bare test environment with ENTIRE_REPO_ROOT and isolated HOME.
func NewTestEnv(t *testing.T, agentName string) *TestEnv {
	t.Helper()

	binPath, ok := AgentBinaries[agentName]
	if !ok {
		t.Fatalf("agent binary not found: %s (available: %v)", agentName, agentBinaryNames())
	}

	dir := t.TempDir()
	homeDir := t.TempDir()

	env := baseEnv(dir, homeDir)

	return &TestEnv{
		t:       t,
		Dir:     dir,
		HomeDir: homeDir,
		Runner: &AgentRunner{
			BinaryPath: binPath,
			Env:        env,
		},
	}
}

// NewTestEnvWithBinary creates a test environment using an explicit binary path.
// Use this from subpackages that build their own agent binary in TestMain.
func NewTestEnvWithBinary(t *testing.T, binPath string) *TestEnv {
	t.Helper()

	dir := t.TempDir()
	homeDir := t.TempDir()

	env := baseEnv(dir, homeDir)

	return &TestEnv{
		t:       t,
		Dir:     dir,
		HomeDir: homeDir,
		Runner: &AgentRunner{
			BinaryPath: binPath,
			Env:        env,
		},
	}
}

// WriteFile writes content to a path relative to the test environment root.
func (e *TestEnv) WriteFile(relPath, content string) {
	e.t.Helper()
	abs := filepath.Join(e.Dir, relPath)
	if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
		e.t.Fatalf("mkdir for %s: %v", relPath, err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o600); err != nil {
		e.t.Fatalf("write %s: %v", relPath, err)
	}
}

// WriteJSON writes a JSON-encoded value to a path relative to the test root.
func (e *TestEnv) WriteJSON(relPath string, v any) {
	e.t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		e.t.Fatalf("marshal JSON for %s: %v", relPath, err)
	}
	e.WriteFile(relPath, string(data))
}

// ReadFile reads a file relative to the test environment root.
func (e *TestEnv) ReadFile(relPath string) string {
	e.t.Helper()
	data, err := os.ReadFile(filepath.Join(e.Dir, relPath))
	if err != nil {
		e.t.Fatalf("read %s: %v", relPath, err)
	}
	return string(data)
}

// FileExists checks whether a relative path exists in the test environment.
func (e *TestEnv) FileExists(relPath string) bool {
	_, err := os.Stat(filepath.Join(e.Dir, relPath))
	return err == nil
}

// MkdirAll creates a directory (and parents) relative to the test root.
func (e *TestEnv) MkdirAll(relPath string) {
	e.t.Helper()
	if err := os.MkdirAll(filepath.Join(e.Dir, relPath), 0o750); err != nil {
		e.t.Fatalf("mkdir %s: %v", relPath, err)
	}
}

// GitInit initializes a git repo in the test environment root.
func (e *TestEnv) GitInit() {
	e.t.Helper()
	cmd := exec.Command("git", "init")
	cmd.Dir = e.Dir
	cmd.Env = e.Runner.Env
	if out, err := cmd.CombinedOutput(); err != nil {
		e.t.Fatalf("git init failed: %v\n%s", err, out)
	}
}

// AbsPath returns the absolute path for a relative path in the test environment.
func (e *TestEnv) AbsPath(relPath string) string {
	return filepath.Join(e.Dir, relPath)
}

func baseEnv(repoRoot, homeDir string) []string {
	return []string{
		fmt.Sprintf("ENTIRE_REPO_ROOT=%s", repoRoot),
		fmt.Sprintf("HOME=%s", homeDir),
		fmt.Sprintf("PATH=%s", os.Getenv("PATH")),
		"LANG=en_US.UTF-8",
	}
}

func agentBinaryNames() []string {
	names := make([]string, 0, len(AgentBinaries))
	for name := range AgentBinaries {
		names = append(names, name)
	}
	return names
}
