package agents

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

func init() {
	if os.Getenv("DEVIN_E2E") != "1" || os.Getenv("E2E_AGENT") != "devin" {
		return
	}
	Register(&Devin{})
	RegisterGate("devin", 1)
}

// Devin drives the Devin CLI ("Devin for Terminal", Cognition) for lifecycle
// tests. Requires an authenticated `devin` binary (`devin auth login`).
type Devin struct{}

func (d *Devin) Name() string               { return "devin" }
func (d *Devin) Binary() string             { return "devin" }
func (d *Devin) EntireAgent() string        { return "devin" }
func (d *Devin) PromptPattern() string      { return `>` }
func (d *Devin) TimeoutMultiplier() float64 { return 2.0 }
func (d *Devin) IsExternalAgent() bool      { return true }

func (d *Devin) IsTransientError(out Output, _ error) bool {
	combined := strings.ToLower(out.Stdout + out.Stderr)
	patterns := []string{
		"overloaded",
		"rate limit",
		"429",
		"500",
		"503",
		"econnreset",
		"etimedout",
		"timeout",
	}
	for _, pattern := range patterns {
		if strings.Contains(combined, pattern) {
			return true
		}
	}
	return false
}

func (d *Devin) Bootstrap() error {
	return nil
}

func (d *Devin) RunPrompt(ctx context.Context, dir string, prompt string, opts ...Option) (Output, error) {
	cfg := &runConfig{}
	for _, o := range opts {
		o(cfg)
	}

	bin, err := exec.LookPath(d.Binary())
	if err != nil {
		return Output{}, fmt.Errorf("%s not in PATH: %w", d.Binary(), err)
	}

	// Print mode: Devin awaits Stop hooks, writes the canonical transcript,
	// then fires SessionEnd before exiting. Workspace trust is disabled so
	// fresh test repos don't hang on the trust prompt.
	args := []string{"-p", prompt, "--permission-mode", "accept-edits", "--respect-workspace-trust", "false"}
	displayArgs := []string{"-p", fmt.Sprintf("%q", prompt), "--permission-mode", "accept-edits", "--respect-workspace-trust", "false"}
	if cfg.Model != "" {
		args = append(args, "--model", cfg.Model)
		displayArgs = append(displayArgs, "--model", cfg.Model)
	}

	timeout := 3 * time.Minute
	if cfg.PromptTimeout > 0 {
		timeout = cfg.PromptTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	env := filterEnv(os.Environ(), "ENTIRE_TEST_TTY")
	cmd := exec.CommandContext(runCtx, bin, args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	out := Output{
		Command:  d.Binary() + " " + strings.Join(displayArgs, " "),
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: cmd.ProcessState.ExitCode(),
	}
	if runErr != nil && errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return out, fmt.Errorf("devin prompt timed out after %s", timeout)
	}
	return out, runErr
}

func (d *Devin) StartSession(context.Context, string) (Session, error) {
	return nil, nil
}
