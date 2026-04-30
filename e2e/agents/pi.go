package agents

import (
	"context"
	"fmt"
	"os"
	"time"
)

func init() {
	if env := os.Getenv("E2E_AGENT"); env != "" && env != "pi" {
		return
	}
	Register(&Pi{})
	RegisterGate("pi", 2)
}

// Pi implements Agent for the Pi coding agent CLI.
type Pi struct{}

func (p *Pi) Name() string               { return "pi" }
func (p *Pi) Binary() string             { return "pi" }
func (p *Pi) EntireAgent() string        { return "pi" }
func (p *Pi) PromptPattern() string      { return `\$\d` }
func (p *Pi) TimeoutMultiplier() float64 { return 1.5 }
func (p *Pi) IsExternalAgent() bool      { return true }

func (p *Pi) IsTransientError(out Output, _ error) bool {
	return isTransient(out, []string{
		"overloaded", "rate limit", "429", "503",
		"ECONNRESET", "ETIMEDOUT", "timeout",
	})
}

func (p *Pi) Bootstrap() error {
	return nil
}

func (p *Pi) RunPrompt(ctx context.Context, dir string, prompt string, _ ...Option) (Output, error) {
	args := []string{"-p", prompt, "--no-skills", "--no-prompt-templates", "--no-themes"}
	displayArgs := []string{"-p", fmt.Sprintf("%q", prompt), "--no-skills", "--no-prompt-templates", "--no-themes"}
	return runAgentCmd(ctx, dir, p.Binary(), args, displayArgs)
}

func (p *Pi) StartSession(ctx context.Context, dir string) (Session, error) {
	name := fmt.Sprintf("pi-test-%d", time.Now().UnixNano())

	s, err := NewTmuxSession(name, dir, []string{"ENTIRE_TEST_TTY"}, p.Binary())
	if err != nil {
		return nil, err
	}

	// Wait for the initial prompt to appear.
	if _, err := s.WaitFor(p.PromptPattern(), 30*time.Second); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("waiting for initial prompt: %w", err)
	}
	s.stableAtSend = ""

	return s, nil
}
