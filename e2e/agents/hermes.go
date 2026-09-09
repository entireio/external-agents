package agents

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func init() {
	if os.Getenv("HERMES_E2E") != "1" || os.Getenv("E2E_AGENT") != "hermes" {
		return
	}
	Register(&Hermes{})
	RegisterGate("hermes", 1)
}

type Hermes struct{}

func (h *Hermes) Name() string               { return "hermes" }
func (h *Hermes) Binary() string             { return "hermes" }
func (h *Hermes) EntireAgent() string        { return "hermes" }
func (h *Hermes) PromptPattern() string      { return `❯` }
func (h *Hermes) TimeoutMultiplier() float64 { return 2.0 }
func (h *Hermes) IsExternalAgent() bool      { return true }

func (h *Hermes) Bootstrap() error {
	homeValue := strings.TrimSpace(os.Getenv("HERMES_HOME"))
	if homeValue == "" {
		return errors.New("Hermes E2E requires an explicit disposable HERMES_HOME")
	}
	home, err := filepath.Abs(homeValue)
	if err != nil {
		return fmt.Errorf("resolve HERMES_HOME: %w", err)
	}
	if resolved, resolveErr := filepath.EvalSymlinks(home); resolveErr == nil {
		home = resolved
	}
	userHome, err := os.UserHomeDir()
	if err == nil {
		realProfile := filepath.Join(userHome, ".hermes")
		if resolved, resolveErr := filepath.EvalSymlinks(realProfile); resolveErr == nil {
			realProfile = resolved
		}
		rel, relErr := filepath.Rel(realProfile, home)
		if relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("Hermes E2E refuses the real profile tree %s", realProfile)
		}
	}
	return nil
}

func (h *Hermes) IsTransientError(out Output, _ error) bool {
	combined := strings.ToLower(out.Stdout + out.Stderr)
	for _, pattern := range []string{"overloaded", "rate limit", "429", "500", "503", "529", "econnreset", "etimedout", "timeout"} {
		if strings.Contains(combined, pattern) {
			return true
		}
	}
	return false
}

func (h *Hermes) RunPrompt(ctx context.Context, dir, prompt string, _ ...Option) (Output, error) {
	if err := h.Bootstrap(); err != nil {
		return Output{}, err
	}
	bin, err := exec.LookPath(h.Binary())
	if err != nil {
		return Output{}, fmt.Errorf("%s not in PATH: %w", h.Binary(), err)
	}
	args := []string{"--yolo", "--ignore-rules", "-z", prompt}
	displayArgs := []string{"--yolo", "--ignore-rules", "-z", fmt.Sprintf("%q", prompt)}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	cmd.Env = filterEnv(os.Environ(), "ENTIRE_TEST_TTY")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	return Output{Command: h.Binary() + " " + strings.Join(displayArgs, " "), Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: exitCode}, err
}

func (h *Hermes) StartSession(_ context.Context, dir string) (Session, error) {
	if err := h.Bootstrap(); err != nil {
		return nil, err
	}
	name := fmt.Sprintf("hermes-test-%d", time.Now().UnixNano())
	session, err := NewTmuxSession(
		name,
		dir,
		[]string{"ENTIRE_TEST_TTY"},
		h.Binary(),
		"--cli",
		"--yolo",
		"--ignore-rules",
	)
	if err != nil {
		return nil, err
	}
	if _, err := session.WaitFor(h.PromptPattern(), 45*time.Second); err != nil {
		_ = session.Close()
		return nil, fmt.Errorf("waiting for initial prompt: %w", err)
	}
	session.stableAtSend = ""
	return session, nil
}
