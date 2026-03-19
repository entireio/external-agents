//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// agentBinaries maps agent names to their built binary paths.
var agentBinaries = map[string]string{}

func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "e2e-agents-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmpDir)

	agents, err := discoverAgents()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to discover agents: %v\n", err)
		os.Exit(1)
	}

	if len(agents) == 0 {
		fmt.Fprintln(os.Stderr, "no agents found in agents/ directory")
		os.Exit(1)
	}

	for _, agentDir := range agents {
		agentName := filepath.Base(agentDir)
		binPath := filepath.Join(tmpDir, agentName)
		// Each agent has its own go.mod, so build from the agent's directory
		agentAbsDir := filepath.Join(repoRoot(), agentDir)
		mainPkg := "./cmd/" + agentName

		fmt.Printf("Building %s...\n", agentName)
		cmd := exec.Command("go", "build", "-o", binPath, mainPkg)
		cmd.Dir = agentAbsDir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "failed to build %s: %v\n", agentName, err)
			os.Exit(1)
		}

		agentBinaries[agentName] = binPath
		fmt.Printf("Built %s -> %s\n", agentName, binPath)
	}

	// Add the temp bin directory to PATH so that `entire enable` can discover
	// the agent binaries (e.g. entire-agent-kiro) during lifecycle tests.
	os.Setenv("PATH", tmpDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	os.Exit(m.Run())
}

func discoverAgents() ([]string, error) {
	agentsDir := filepath.Join(repoRoot(), "agents")
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return nil, fmt.Errorf("read agents dir: %w", err)
	}

	var agents []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if !strings.HasPrefix(entry.Name(), "entire-agent-") {
			continue
		}
		// Verify it has a cmd/<name>/main.go
		mainFile := filepath.Join(agentsDir, entry.Name(), "cmd", entry.Name(), "main.go")
		if _, err := os.Stat(mainFile); err != nil {
			continue
		}
		agents = append(agents, filepath.Join("agents", entry.Name()))
	}
	return agents, nil
}

func repoRoot() string {
	// Walk up from e2e/ to find the repo root
	dir, err := os.Getwd()
	if err != nil {
		return ".."
	}
	// If we're in the e2e directory, go up one level
	if filepath.Base(dir) == "e2e" {
		return filepath.Dir(dir)
	}
	return dir
}
