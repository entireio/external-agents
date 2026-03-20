//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	// Import agents package to trigger init() registration.
	_ "github.com/entireio/external-agents/e2e/agents"
	"github.com/entireio/external-agents/e2e/entire"
	"github.com/entireio/external-agents/e2e/testutil"
)

func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "e2e-agents-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmpDir)

	discoveredAgents, err := DiscoverAgents()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to discover agents: %v\n", err)
		os.Exit(1)
	}

	if len(discoveredAgents) == 0 {
		fmt.Fprintln(os.Stderr, "no agents found in agents/ directory")
		os.Exit(1)
	}

	for _, agentDir := range discoveredAgents {
		agentName := filepath.Base(agentDir)
		fmt.Printf("Building %s...\n", agentName)
		binPath, err := BuildAgent(agentName, tmpDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to build %s: %v\n", agentName, err)
			os.Exit(1)
		}
		AgentBinaries[agentName] = binPath
		fmt.Printf("Built %s -> %s\n", agentName, binPath)
	}

	// Add the temp bin directory to PATH so that `entire enable` can discover
	// the agent binaries (e.g. entire-agent-kiro) during lifecycle tests.
	os.Setenv("PATH", tmpDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	// --- Artifact directory setup ---
	runDir := os.Getenv("E2E_ARTIFACT_DIR")
	if runDir == "" {
		_, file, _, _ := runtime.Caller(0)
		testutil.ArtifactRoot = filepath.Join(filepath.Dir(file), "artifacts")
		runDir = testutil.ArtifactRunDir()
	}
	_ = os.MkdirAll(runDir, 0o755)
	testutil.SetRunDir(runDir)

	// Prepend the entire binary's directory to PATH so git hooks resolve
	// to the same binary the test harness uses.
	entireBin := entire.BinPath()
	if dir := filepath.Dir(entireBin); dir != "." {
		os.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	}

	// --- Preflight checks ---
	if _, err := exec.LookPath("tmux"); err != nil {
		fmt.Fprintln(os.Stderr, "warning: tmux not found — interactive session tests will fail")
	}

	// Write entire version info to artifact dir.
	version := "unknown"
	if out, err := exec.Command(entireBin, "version").Output(); err == nil {
		version = string(out)
	}
	preflight := fmt.Sprintf("entire binary:  %s\nentire version: %s\n", entireBin, version)
	_ = os.WriteFile(filepath.Join(runDir, "entire-version.txt"), []byte(preflight), 0o644)

	// Isolate git config to prevent user's ~/.gitconfig from interfering.
	os.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")

	os.Exit(m.Run())
}
