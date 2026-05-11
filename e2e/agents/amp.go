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
	if env := os.Getenv("E2E_AGENT"); env != "" && env != "amp" {
		return
	}
	Register(&Amp{})
	RegisterGate("amp", 2)
}

type Amp struct{}

func (a *Amp) Name() string               { return "amp" }
func (a *Amp) Binary() string             { return "amp" }
func (a *Amp) EntireAgent() string        { return "amp" }
func (a *Amp) PromptPattern() string      { return `>` }
func (a *Amp) TimeoutMultiplier() float64 { return 1.5 }
func (a *Amp) IsExternalAgent() bool      { return true }

func (a *Amp) IsTransientError(out Output, _ error) bool {
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

func (a *Amp) Bootstrap() error {
	return nil
}

func (a *Amp) RunPrompt(ctx context.Context, dir string, prompt string, opts ...Option) (Output, error) {
	cfg := &runConfig{}
	for _, o := range opts {
		o(cfg)
	}

	bin, err := exec.LookPath(a.Binary())
	if err != nil {
		return Output{}, fmt.Errorf("%s not in PATH: %w", a.Binary(), err)
	}

	args := []string{"--dangerously-allow-all", "--no-notifications", "--no-ide", "-x", prompt}
	displayArgs := []string{"--dangerously-allow-all", "--no-notifications", "--no-ide", "-x", fmt.Sprintf("%q", prompt)}

	env := append(filterEnv(os.Environ(), "ENTIRE_TEST_TTY"), "PLUGINS=all")

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
		Command:  "PLUGINS=all " + a.Binary() + " " + strings.Join(displayArgs, " "),
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
	}, err
}

func (a *Amp) StartSession(ctx context.Context, dir string) (Session, error) {
	name := fmt.Sprintf("amp-test-%d", time.Now().UnixNano())
	s, err := NewTmuxSession(name, dir, []string{"ENTIRE_TEST_TTY"}, "env", "PLUGINS=all", a.Binary())
	if err != nil {
		return nil, err
	}
	if _, err := s.WaitFor(a.PromptPattern(), 30*time.Second); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("waiting for initial prompt: %w", err)
	}
	s.stableAtSend = ""
	return s, nil
}
