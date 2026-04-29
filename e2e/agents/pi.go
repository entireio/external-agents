package agents

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func (p *Pi) Bootstrap() error {
	if !isAPIKeyAuthMode() {
		return nil
	}
	if err := requireAnyEnv("ANTHROPIC_API_KEY", "GEMINI_API_KEY", "OPENAI_API_KEY"); err != nil {
		return err
	}
	return nil
}

func (p *Pi) RunPrompt(ctx context.Context, dir string, prompt string, opts ...Option) (Output, error) {
	cfg := &runConfig{}
	for _, o := range opts {
		o(cfg)
	}

	bin, err := exec.LookPath(p.Binary())
	if err != nil {
		return Output{}, fmt.Errorf("%s not in PATH: %w", p.Binary(), err)
	}

	args := []string{"-p", prompt, "--no-skills", "--no-prompt-templates", "--no-themes"}
	displayArgs := []string{"-p", fmt.Sprintf("%q", prompt), "--no-skills", "--no-prompt-templates", "--no-themes"}

	env := filterEnv(os.Environ(), "ENTIRE_TEST_TTY")

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	cmd.Env = env
	configureCmdProcAttr(cmd)
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
		Command:  filepath.Base(bin) + " " + strings.Join(displayArgs, " "),
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
	}, err
}

func (p *Pi) StartSession(ctx context.Context, dir string) (Session, error) {
	name := fmt.Sprintf("pi-test-%d", time.Now().UnixNano())

	s, err := newInteractiveSession(name, dir, []string{"ENTIRE_TEST_TTY"}, p.Binary())
	if err != nil {
		return nil, err
	}

	// Wait for the initial prompt to appear.
	if _, err := s.WaitFor(p.PromptPattern(), 30*time.Second); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("waiting for initial prompt: %w", err)
	}
	return s, nil
}
