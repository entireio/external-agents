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
	if env := os.Getenv("E2E_AGENT"); env != "" && env != "goose" {
		return
	}
	Register(&Goose{})
	RegisterGate("goose", 2)
}

type Goose struct{}

func (g *Goose) Name() string          { return "goose" }
func (g *Goose) Binary() string        { return "goose" }
func (g *Goose) EntireAgent() string   { return "goose" }
func (g *Goose) PromptPattern() string { return `\( O\)>` }

// TimeoutMultiplier is generous: goose runs against remote providers and
// streams full tool loops.
func (g *Goose) TimeoutMultiplier() float64 { return 2.0 }
func (g *Goose) IsExternalAgent() bool      { return true }

func (g *Goose) IsTransientError(out Output, _ error) bool {
	combined := strings.ToLower(out.Stdout + out.Stderr)
	patterns := []string{
		"overloaded",
		"rate limit",
		"429",
		"500",
		"503",
		"529",
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

func (g *Goose) Bootstrap() error {
	return nil
}

func (g *Goose) RunPrompt(ctx context.Context, dir string, prompt string, opts ...Option) (Output, error) {
	cfg := &runConfig{}
	for _, o := range opts {
		o(cfg)
	}

	bin, err := exec.LookPath(g.Binary())
	if err != nil {
		return Output{}, fmt.Errorf("%s not in PATH: %w", g.Binary(), err)
	}

	args := []string{"run", "-t", prompt}
	displayArgs := []string{"run", "-t", fmt.Sprintf("%q", prompt)}

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	cmd.Env = filterEnv(os.Environ(), "ENTIRE_TEST_TTY")
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
		Command:  g.Binary() + " " + strings.Join(displayArgs, " "),
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
	}, err
}

func (g *Goose) StartSession(ctx context.Context, dir string) (Session, error) {
	name := fmt.Sprintf("goose-test-%d", time.Now().UnixNano())
	s, err := NewTmuxSession(name, dir, []string{"ENTIRE_TEST_TTY"}, g.Binary(), "session")
	if err != nil {
		return nil, err
	}
	if _, err := s.WaitFor(g.PromptPattern(), 30*time.Second); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("waiting for initial prompt: %w", err)
	}
	s.stableAtSend = ""
	return s, nil
}
