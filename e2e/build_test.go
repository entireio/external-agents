//go:build e2e

package e2e

import "testing"

func TestAgentBinaryPathAddsExeOnWindows(t *testing.T) {
	got := agentBinaryPath("/tmp/out", "entire-agent-kiro", "windows")
	want := "/tmp/out/entire-agent-kiro.exe"
	if got != want {
		t.Fatalf("agentBinaryPath(..., windows) = %q, want %q", got, want)
	}
}

func TestAgentBinaryPathKeepsUnixName(t *testing.T) {
	got := agentBinaryPath("/tmp/out", "entire-agent-pi", "darwin")
	want := "/tmp/out/entire-agent-pi"
	if got != want {
		t.Fatalf("agentBinaryPath(..., darwin) = %q, want %q", got, want)
	}
}
