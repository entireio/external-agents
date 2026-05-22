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
	if env := os.Getenv("E2E_AGENT"); env != "" && env != "kilo" {
		return
	}
	Register(&Kilo{})
	RegisterGate("kilo", 2)
}

type Kilo struct{}

func (k *Kilo) Name() string               { return "kilo" }
func (k *Kilo) Binary() string             { return "kilo" }
func (k *Kilo) EntireAgent() string        { return "kilo" }
func (k *Kilo) PromptPattern() string      { return `>` }
func (k *Kilo) TimeoutMultiplier() float64 { return 1.5 }
func (k *Kilo) IsExternalAgent() bool      { return true }

func (k *Kilo) IsTransientError(out Output, _ error) bool {
	combined := strings.ToLower(out.Stdout + out.Stderr)
	patterns := []string{
		"overloaded",
		"rate limit",
		"status 429", "status 500", "status 503",
		"http 429", "http 500", "http 503",
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

func (k *Kilo) Bootstrap() error {
	return nil
}

func (k *Kilo) RunPrompt(ctx context.Context, dir string, prompt string, opts ...Option) (Output, error) {
	cfg := &runConfig{}
	for _, o := range opts {
		o(cfg)
	}

	bin, err := exec.LookPath(k.Binary())
	if err != nil {
		return Output{}, fmt.Errorf("%s not in PATH: %w", k.Binary(), err)
	}

	args := []string{"run", "--print", "--no-color", prompt}
	displayArgs := []string{"run", "--print", "--no-color", fmt.Sprintf("%q", prompt)}

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

func (k *Kilo) StartSession(ctx context.Context, dir string) (Session, error) {
	name := fmt.Sprintf("kilo-test-%d", time.Now().UnixNano())
	s, err := NewTmuxSession(name, dir, []string{"ENTIRE_TEST_TTY"}, k.Binary())
	if err != nil {
		return nil, err
	}
	if _, err := s.WaitFor(k.PromptPattern(), 30*time.Second); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("waiting for initial prompt: %w", err)
	}
	s.stableAtSend = ""
	return s, nil
}
