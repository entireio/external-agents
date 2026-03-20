//go:build e2e

package kiro

import (
	"fmt"
	"os"
	"testing"

	e2e "github.com/entireio/external-agents/e2e"
)

// kiroBinary holds the path to the built entire-agent-kiro binary.
var kiroBinary string

func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "e2e-kiro-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmpDir)

	fmt.Println("Building entire-agent-kiro...")
	binPath, err := e2e.BuildAgent("entire-agent-kiro", tmpDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to build entire-agent-kiro: %v\n", err)
		os.Exit(1)
	}
	kiroBinary = binPath
	fmt.Printf("Built entire-agent-kiro -> %s\n", binPath)

	// Isolate git config to prevent user's ~/.gitconfig from interfering.
	os.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")

	os.Exit(m.Run())
}
