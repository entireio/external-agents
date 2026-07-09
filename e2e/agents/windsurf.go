package agents

import (
	"context"
	"fmt"
	"os"
)

func init() {
	// Windsurf is an IDE agent with no automatable CLI, so it is excluded from
	// the default "run all agents" E2E suite. Register only when explicitly
	// targeted so hook-level smoke tests can still be run via
	//   E2E_AGENT=windsurf mise run test:e2e
	if os.Getenv("E2E_AGENT") != "windsurf" {
		return
	}
	Register(&Windsurf{})
	RegisterGate("windsurf", 1)
}

// Windsurf implements the Agent interface for the Windsurf IDE.
// Windsurf is an IDE, not a CLI, so RunPrompt and StartSession are not
// supported in automated E2E tests. Integration tests are covered by
// unit tests in the entire-agent-windsurf package.
type Windsurf struct{}

func (w *Windsurf) Name() string               { return "windsurf" }
func (w *Windsurf) Binary() string             { return "windsurf" }
func (w *Windsurf) EntireAgent() string        { return "windsurf" }
func (w *Windsurf) PromptPattern() string      { return "" }
func (w *Windsurf) TimeoutMultiplier() float64 { return 1.0 }
func (w *Windsurf) IsExternalAgent() bool      { return true }
func (w *Windsurf) IsIDEOnly() bool            { return true }

func (w *Windsurf) IsTransientError(_ Output, _ error) bool { return false }

func (w *Windsurf) Bootstrap() error { return nil }

func (w *Windsurf) RunPrompt(_ context.Context, _ string, _ string, _ ...Option) (Output, error) {
	return Output{}, fmt.Errorf("windsurf is an IDE agent and does not support automated RunPrompt")
}

func (w *Windsurf) StartSession(_ context.Context, _ string) (Session, error) {
	return nil, fmt.Errorf("windsurf is an IDE agent and does not support automated StartSession")
}
