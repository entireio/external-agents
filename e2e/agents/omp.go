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
	if env := os.Getenv("E2E_AGENT"); env != "" && env != "omp" {
		return
	}
	Register(&Omp{})
	RegisterGate("omp", 2)
}

// Omp implements Agent for the Oh My Pi coding agent CLI.
type Omp struct{}

func (o *Omp) Name() string               { return "omp" }
func (o *Omp) Binary() string             { return "omp" }
func (o *Omp) EntireAgent() string        { return "omp" }
func (o *Omp) PromptPattern() string      { return `\$\d` }
func (o *Omp) TimeoutMultiplier() float64 { return 1.5 }
func (o *Omp) IsExternalAgent() bool      { return true }

func (o *Omp) IsTransientError(out Output, _ error) bool {
	combined := out.Stdout + out.Stderr
	transientPatterns := []string{
		"overloaded",
		"rate limit",
		"429",
		"503",
		"ECONNRESET",
		"ETIMEDOUT",
		"timeout",
	}
	for _, pat := range transientPatterns {
		if strings.Contains(combined, pat) {
			return true
		}
	}
	return false
}

func (o *Omp) Bootstrap() error {
	return nil
}

func (o *Omp) RunPrompt(ctx context.Context, dir string, prompt string, opts ...Option) (Output, error) {
	cfg := &runConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	bin, err := exec.LookPath(o.Binary())
	if err != nil {
		return Output{}, fmt.Errorf("%s not in PATH: %w", o.Binary(), err)
	}

	args := []string{"-p", prompt, "--no-skills", "--no-rules"}
	displayArgs := []string{"-p", fmt.Sprintf("%q", prompt), "--no-skills", "--no-rules"}

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
		Command:  o.Binary() + " " + strings.Join(displayArgs, " "),
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
	}, err
}

func (o *Omp) StartSession(ctx context.Context, dir string) (Session, error) {
	name := fmt.Sprintf("omp-test-%d", time.Now().UnixNano())

	s, err := NewTmuxSession(name, dir, []string{"ENTIRE_TEST_TTY"}, o.Binary())
	if err != nil {
		return nil, err
	}

	// Wait for the initial prompt to appear.
	if _, err := s.WaitFor(o.PromptPattern(), 30*time.Second); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("waiting for initial prompt: %w", err)
	}
	s.stableAtSend = ""

	return s, nil
}
