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
	if env := os.Getenv("E2E_AGENT"); env != "" && env != "mistral-vibe" {
		return
	}
	Register(&MistralVibe{})
	RegisterGate("mistral-vibe", 2)
}

// MistralVibe implements Agent for the Mistral Vibe CLI.
type MistralVibe struct{}

func (v *MistralVibe) Name() string               { return "mistral-vibe" }
func (v *MistralVibe) Binary() string             { return "vibe" }
func (v *MistralVibe) EntireAgent() string        { return "mistral-vibe" }
func (v *MistralVibe) PromptPattern() string      { return `>` }
func (v *MistralVibe) TimeoutMultiplier() float64 { return 2.0 }
func (v *MistralVibe) IsExternalAgent() bool      { return true }

func (v *MistralVibe) IsTransientError(out Output, _ error) bool {
	combined := out.Stdout + out.Stderr
	transientPatterns := []string{
		"Rate limits exceeded",
		"overloaded",
		"429",
		"Too Many Requests",
		"BackendError",
		"timeout",
		"503",
	}
	for _, p := range transientPatterns {
		if strings.Contains(combined, p) {
			return true
		}
	}
	return false
}

func (v *MistralVibe) Bootstrap() error {
	return nil
}

func (v *MistralVibe) RunPrompt(ctx context.Context, dir string, prompt string, opts ...Option) (Output, error) {
	cfg := &runConfig{}
	for _, o := range opts {
		o(cfg)
	}

	bin, err := exec.LookPath(v.Binary())
	if err != nil {
		return Output{}, fmt.Errorf("%s not in PATH: %w", v.Binary(), err)
	}

	args := []string{"-p", prompt, "--output", "json", "--workdir", dir}
	displayArgs := []string{"-p", fmt.Sprintf("%q", prompt), "--output", "json", "--workdir", dir}

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
		Command:  v.Binary() + " " + strings.Join(displayArgs, " "),
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
	}, err
}

func (v *MistralVibe) StartSession(ctx context.Context, dir string) (Session, error) {
	// Vibe's interactive mode uses Textual TUI which is not tmux-friendly.
	return nil, nil
}
