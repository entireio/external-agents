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
	if os.Getenv("GROK_E2E") != "1" || os.Getenv("E2E_AGENT") != "grok" {
		return
	}
	Register(&Grok{})
	RegisterGate("grok", 1)
}

type Grok struct{}

func (g *Grok) Name() string               { return "grok" }
func (g *Grok) Binary() string             { return "grok" }
func (g *Grok) EntireAgent() string        { return "grok" }
func (g *Grok) PromptPattern() string      { return `>`
func (g *Grok) TimeoutMultiplier() float64 { return 2.0 }
func (g *Grok) IsExternalAgent() bool      { return true }

func (g *Grok) IsTransientError(out Output, _ error) bool {
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

func (g *Grok) Bootstrap() error {
	return nil
}

func (g *Grok) RunPrompt(ctx context.Context, dir string, prompt string, opts ...Option) (Output, error) {
	cfg := &runConfig{}
	for _, o := range opts {
		o(cfg)
	}

	bin, err := exec.LookPath(g.Binary())
	if err != nil {
		return Output{}, fmt.Errorf("%s not in PATH: %w", g.Binary(), err)
	}

	args := []string{prompt, "--headless", "--always-approve"}
	displayArgs := []string{fmt.Sprintf("%q", prompt), "--headless", "--always-approve"}
	if cfg.Model != "" {
		args = append(args, "--model", cfg.Model)
		displayArgs = append(displayArgs, "--model", cfg.Model)
	}

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
		Command:  g.Binary() + " " + strings.Join(displayArgs, " "),
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
	}, err
}

func (g *Grok) StartSession(context.Context, string) (Session, error) {
	return nil, nil
}