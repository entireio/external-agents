package agents

import (
	"context"
	"fmt"
	"os"
	"time"
)

func init() {
	if env := os.Getenv("E2E_AGENT"); env != "" && env != "omp" {
		return
	}
	Register(&Omp{})
	RegisterGate("omp", 2)
}

// Omp implements Agent for the oh-my-pi coding agent CLI
// (https://github.com/can1357/oh-my-pi), a fork of badlogic/pi-mono that
// ships its CLI as `omp` and drops the upstream `--no-prompt-templates` /
// `--no-themes` flags.
type Omp struct{}

func (o *Omp) Name() string               { return "omp" }
func (o *Omp) Binary() string             { return "omp" }
func (o *Omp) EntireAgent() string        { return "omp" }
func (o *Omp) PromptPattern() string      { return `\$\d` }
func (o *Omp) TimeoutMultiplier() float64 { return 1.5 }
func (o *Omp) IsExternalAgent() bool      { return true }

func (o *Omp) IsTransientError(out Output, _ error) bool {
	return isTransient(out, []string{
		"overloaded", "rate limit", "429", "503",
		"ECONNRESET", "ETIMEDOUT", "timeout",
	})
}

func (o *Omp) Bootstrap() error {
	return nil
}

func (o *Omp) RunPrompt(ctx context.Context, dir string, prompt string, _ ...Option) (Output, error) {
	args := []string{"-p", prompt, "--no-skills"}
	displayArgs := []string{"-p", fmt.Sprintf("%q", prompt), "--no-skills"}
	return runAgentCmd(ctx, dir, o.Binary(), args, displayArgs)
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
