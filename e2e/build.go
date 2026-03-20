//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// AgentBinaries maps agent names to their built binary paths.
var AgentBinaries = map[string]string{}

// RepoRoot returns the absolute path to the repository root.
// Uses runtime.Caller to locate the source file at compile time, which is
// reliable regardless of the working directory at runtime (go test runs
// from a temp dir, not the source dir).
func RepoRoot() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ".."
	}
	// file = /absolute/path/to/e2e/build.go → up one level to repo root
	return filepath.Dir(filepath.Dir(file))
}

// BuildAgent compiles a single agent binary from agents/<agentName>/cmd/<agentName>/
// into the given output directory. Returns the absolute path to the built binary.
func BuildAgent(agentName, outputDir string) (string, error) {
	agentDir := filepath.Join(RepoRoot(), "agents", agentName)
	mainPkg := "./cmd/" + agentName
	binPath := filepath.Join(outputDir, agentName)

	cmd := exec.Command("go", "build", "-o", binPath, mainPkg)
	cmd.Dir = agentDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("build %s: %w", agentName, err)
	}
	return binPath, nil
}

// DiscoverAgents returns relative paths (e.g. "agents/entire-agent-kiro") for
// all agent directories that have a cmd/<name>/main.go.
func DiscoverAgents() ([]string, error) {
	agentsDir := filepath.Join(RepoRoot(), "agents")
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return nil, fmt.Errorf("read agents dir: %w", err)
	}

	var agentDirs []string
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "entire-agent-") {
			continue
		}
		mainFile := filepath.Join(agentsDir, entry.Name(), "cmd", entry.Name(), "main.go")
		if _, err := os.Stat(mainFile); err != nil {
			continue
		}
		agentDirs = append(agentDirs, filepath.Join("agents", entry.Name()))
	}
	return agentDirs, nil
}
