//go:build e2e

package vibe

import (
	"fmt"
	"os"
	"testing"

	e2e "github.com/entireio/external-agents/e2e"
)

// vibeBinary holds the path to the built entire-agent-mistral-vibe binary.
var vibeBinary string

func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "e2e-vibe-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmpDir)

	fmt.Println("Building entire-agent-mistral-vibe...")
	binPath, err := e2e.BuildAgent("entire-agent-mistral-vibe", tmpDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to build entire-agent-mistral-vibe: %v\n", err)
		os.Exit(1)
	}
	vibeBinary = binPath
	fmt.Printf("Built entire-agent-mistral-vibe -> %s\n", binPath)

	// Isolate git config to prevent user's ~/.gitconfig from interfering.
	os.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")

	os.Exit(m.Run())
}
