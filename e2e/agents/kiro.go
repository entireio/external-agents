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
	if env := os.Getenv("E2E_AGENT"); env != "" && env != "kiro" {
		return
	}
	Register(&Kiro{})
	RegisterGate("kiro", 2)
}

// Kiro implements Agent for the Kiro CLI (kiro-cli-chat).
type Kiro struct{}

func (k *Kiro) Name() string               { return "kiro" }
func (k *Kiro) Binary() string             { return "kiro-cli-chat" }
func (k *Kiro) EntireAgent() string        { return "kiro" }
func (k *Kiro) PromptPattern() string      { return `>` }
func (k *Kiro) TimeoutMultiplier() float64 { return 1.0 }
func (k *Kiro) IsExternalAgent() bool      { return true }

func (k *Kiro) IsTransientError(out Output, _ error) bool {
	combined := out.Stdout + out.Stderr
	transientPatterns := []string{
		"overloaded",
		"rate limit",
		"529",
		"503",
		"500",
		"ECONNRESET",
		"ETIMEDOUT",
	}
	for _, p := range transientPatterns {
		if strings.Contains(combined, p) {
			return true
		}
	}
	return false
}

func (k *Kiro) Bootstrap() error {
	// No-op locally. On CI, write config for non-interactive auth if needed.
	return nil
}

func (k *Kiro) RunPrompt(ctx context.Context, dir string, prompt string, opts ...Option) (Output, error) {
	cfg := &runConfig{}
	for _, o := range opts {
		o(cfg)
	}

	bin, err := exec.LookPath(k.Binary())
	if err != nil {
		return Output{}, fmt.Errorf("%s not in PATH: %w", k.Binary(), err)
	}

	args := []string{"chat", "--no-interactive", "--trust-all-tools", "--agent", "entire", prompt}
	displayArgs := []string{"chat", "--no-interactive", "--trust-all-tools", "--agent", "entire", fmt.Sprintf("%q", prompt)}

	env := filterEnv(os.Environ(), "ENTIRE_TEST_TTY")

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 5 * time.Second

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	exitCode := 0
	if err != nil {
		exitErr := &exec.ExitError{}
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	return Output{
		Command:  k.Binary() + " " + strings.Join(displayArgs, " "),
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
	}, err
}

func (k *Kiro) StartSession(ctx context.Context, dir string) (Session, error) {
	name := fmt.Sprintf("kiro-test-%d", time.Now().UnixNano())

	s, err := NewTmuxSession(name, dir, []string{"ENTIRE_TEST_TTY"}, k.Binary(), "chat", "--agent", "entire")
	if err != nil {
		return nil, err
	}

	// Wait for the initial prompt to appear.
	if _, err := s.WaitFor(k.PromptPattern(), 30*time.Second); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("waiting for initial prompt: %w", err)
	}
	s.stableAtSend = ""

	return s, nil
}
