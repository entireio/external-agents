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

const (
	ompTimeoutMultiplier = 2.0
	ompBusyPattern       = `(?m)^[\t ]*(?:[⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏] [^\r\n]*(?:⟦esc⟧|⟨esc⟩)|[-\\|/] [^\r\n]*\[esc\])[\t ]*$`
)

func init() {
	if env := os.Getenv("E2E_AGENT"); env != "" && env != "omp" {
		return
	}
	Register(&OMP{})
	RegisterGate("omp", 1)
}

type OMP struct{}

type ompSession struct {
	*TmuxSession
}

func (s *ompSession) WaitFor(pattern string, timeout time.Duration) (string, error) {
	return s.TmuxSession.WaitFor(pattern, time.Duration(float64(timeout)*ompTimeoutMultiplier))
}

func (o *OMP) Name() string               { return "omp" }
func (o *OMP) Binary() string             { return "omp" }
func (o *OMP) EntireAgent() string        { return "omp" }
func (o *OMP) PromptPattern() string      { return `╭──` }
func (o *OMP) TimeoutMultiplier() float64 { return ompTimeoutMultiplier }
func (o *OMP) IsExternalAgent() bool      { return true }

func (o *OMP) IsTransientError(out Output, _ error) bool {
	combined := strings.ToLower(out.Stdout + out.Stderr)
	patterns := []string{
		"overloaded",
		"rate limit",
		"429",
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

func (o *OMP) Bootstrap() error {
	return nil
}

func (o *OMP) RunPrompt(ctx context.Context, dir string, prompt string, _ ...Option) (Output, error) {
	bin, err := exec.LookPath(o.Binary())
	if err != nil {
		return Output{}, fmt.Errorf("%s not in PATH: %w", o.Binary(), err)
	}

	args := []string{"-p", prompt, "--approval-mode=yolo", "--no-skills", "--no-rules", "--thinking=off", "--tools=write"}
	displayArgs := []string{"-p", fmt.Sprintf("%q", prompt), "--approval-mode=yolo", "--no-skills", "--no-rules", "--thinking=off", "--tools=write"}

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
		Command:  o.Binary() + " " + strings.Join(displayArgs, " "),
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
	}, err
}

func (o *OMP) StartSession(_ context.Context, dir string) (Session, error) {
	name := fmt.Sprintf("omp-test-%d", time.Now().UnixNano())
	s, err := NewTmuxSession(
		name,
		dir,
		[]string{"ENTIRE_TEST_TTY"},
		o.Binary(),
		"--approval-mode=yolo",
		"--no-skills",
		"--no-rules",
		"--thinking=off",
		"--tools=write",
	)
	if err != nil {
		return nil, err
	}
	if err := s.SetBusyPattern(ompBusyPattern); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("compile omp busy pattern: %w", err)
	}
	if _, err := s.WaitFor(o.PromptPattern(), 30*time.Second); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("waiting for initial prompt: %w", err)
	}
	s.stableAtSend = ""
	return &ompSession{TmuxSession: s}, nil
}
