//go:build e2e

package e2e

import (
	"fmt"
	"io"
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

// BuildAgent builds a single agent binary into the given output directory.
// Agents with mise.toml use their local build task; older Go-only agents fall
// back to go build from cmd/<agentName>/.
func BuildAgent(agentName, outputDir string) (string, error) {
	agentDir := filepath.Join(RepoRoot(), "agents", agentName)
	binPath := filepath.Join(outputDir, agentName)

	if _, err := os.Stat(filepath.Join(agentDir, "mise.toml")); err == nil {
		cmd := exec.Command("mise", "run", "build")
		cmd.Dir = agentDir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("mise build %s: %w", agentName, err)
		}

		builtPath := filepath.Join(agentDir, agentName)
		if err := copyExecutable(builtPath, binPath); err != nil {
			return "", fmt.Errorf("copy built %s: %w", agentName, err)
		}
		return binPath, nil
	}

	mainPkg := "./cmd/" + agentName
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
// all agent directories with either a mise build contract or a Go main package.
func DiscoverAgents() ([]string, error) {
	agentsDir := filepath.Join(RepoRoot(), "agents")
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return nil, fmt.Errorf("read agents dir: %w", err)
	}

	var agentDirs []string
	filter := os.Getenv("E2E_AGENT")
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "entire-agent-") {
			continue
		}
		if filter != "" && strings.TrimPrefix(entry.Name(), "entire-agent-") != filter {
			continue
		}
		miseFile := filepath.Join(agentsDir, entry.Name(), "mise.toml")
		if _, err := os.Stat(miseFile); err == nil {
			agentDirs = append(agentDirs, filepath.Join("agents", entry.Name()))
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

func copyExecutable(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory", src)
	}

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode()|0o755)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Chmod(info.Mode() | 0o755)
}
