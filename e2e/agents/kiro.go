package agents

import (
	"context"
	"fmt"
	"os"
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
	return isTransient(out, []string{
		"overloaded", "rate limit", "529", "503", "500",
		"ECONNRESET", "ETIMEDOUT",
	})
}

func (k *Kiro) Bootstrap() error {
	return nil
}

func (k *Kiro) RunPrompt(ctx context.Context, dir string, prompt string, _ ...Option) (Output, error) {
	args := []string{"chat", "--no-interactive", "--trust-all-tools", "--agent", "entire", prompt}
	displayArgs := []string{"chat", "--no-interactive", "--trust-all-tools", "--agent", "entire", fmt.Sprintf("%q", prompt)}
	return runAgentCmd(ctx, dir, k.Binary(), args, displayArgs)
}

func (k *Kiro) StartSession(ctx context.Context, dir string) (Session, error) {
	name := fmt.Sprintf("kiro-test-%d", time.Now().UnixNano())

	s, err := NewTmuxSession(name, dir, []string{"ENTIRE_TEST_TTY"}, k.Binary(), "chat", "--trust-all-tools", "--agent", "entire")
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
