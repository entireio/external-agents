//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"errors"
	"os/exec"
	"testing"
)

// CommandResult holds the output of a binary invocation.
type CommandResult struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	Err      error
}

// AgentRunner invokes an agent binary with subcommands.
type AgentRunner struct {
	BinaryPath string
	Env        []string
}

// Run executes the agent binary with the given subcommand, args, and optional stdin.
func (r *AgentRunner) Run(stdin string, subcommand string, args ...string) CommandResult {
	cmdArgs := append([]string{subcommand}, args...)
	cmd := exec.Command(r.BinaryPath, cmdArgs...)
	cmd.Stdin = bytes.NewBufferString(stdin)
	cmd.Env = r.Env

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	return CommandResult{
		Stdout:   stdout.Bytes(),
		Stderr:   stderr.Bytes(),
		ExitCode: exitCode,
		Err:      err,
	}
}

// RunJSON executes the subcommand and JSON-decodes stdout into dest.
func (r *AgentRunner) RunJSON(t *testing.T, dest any, stdin string, subcommand string, args ...string) CommandResult {
	t.Helper()
	result := r.Run(stdin, subcommand, args...)
	if result.ExitCode != 0 {
		t.Fatalf("%s %s failed (exit %d): %s", r.BinaryPath, subcommand, result.ExitCode, result.Stderr)
	}
	if err := json.Unmarshal(result.Stdout, dest); err != nil {
		t.Fatalf("failed to decode JSON from %s %s: %v\nstdout: %s", r.BinaryPath, subcommand, err, result.Stdout)
	}
	return result
}

// MustSucceed asserts the subcommand exits with code 0.
func (r *AgentRunner) MustSucceed(t *testing.T, stdin string, subcommand string, args ...string) CommandResult {
	t.Helper()
	result := r.Run(stdin, subcommand, args...)
	if result.ExitCode != 0 {
		t.Fatalf("%s %s: expected exit 0, got %d\nstderr: %s", r.BinaryPath, subcommand, result.ExitCode, result.Stderr)
	}
	return result
}

// MustFail asserts the subcommand exits with a non-zero code.
func (r *AgentRunner) MustFail(t *testing.T, stdin string, subcommand string, args ...string) CommandResult {
	t.Helper()
	result := r.Run(stdin, subcommand, args...)
	if result.ExitCode == 0 {
		t.Fatalf("%s %s: expected non-zero exit, got 0\nstdout: %s", r.BinaryPath, subcommand, result.Stdout)
	}
	return result
}
