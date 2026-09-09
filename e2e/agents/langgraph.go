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
	if env := os.Getenv("E2E_AGENT"); env != "" && env != "langgraph" {
		return
	}
	Register(&LangGraph{})
	RegisterGate("langgraph", 2)
}

type LangGraph struct{}

func (l *LangGraph) Name() string               { return "langgraph" }
func (l *LangGraph) Binary() string             { return "entire-agent-langgraph" }
func (l *LangGraph) EntireAgent() string        { return "langgraph" }
func (l *LangGraph) PromptPattern() string      { return `Entire session:` }
func (l *LangGraph) TimeoutMultiplier() float64 { return 1.0 }
func (l *LangGraph) IsExternalAgent() bool      { return true }
func (l *LangGraph) Bootstrap() error           { return nil }

func (l *LangGraph) IsTransientError(_ Output, _ error) bool {
	return false
}

func (l *LangGraph) RunPrompt(ctx context.Context, dir string, prompt string, _ ...Option) (Output, error) {
	bin, err := exec.LookPath(l.Binary())
	if err != nil {
		return Output{}, fmt.Errorf("%s not in PATH: %w", l.Binary(), err)
	}

	args := []string{"__e2e_run_prompt", prompt}
	displayArgs := []string{"__e2e_run_prompt", fmt.Sprintf("%q", prompt)}

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
		Command:  l.Binary() + " " + strings.Join(displayArgs, " "),
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
	}, err
}

func (l *LangGraph) StartSession(context.Context, string) (Session, error) {
	return nil, nil
}
