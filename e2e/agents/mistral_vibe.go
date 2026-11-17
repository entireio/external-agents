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

const (
	mistralVibeName         = "mistral-vibe"
	mistralVibeDefaultModel = "mistral-medium-3.5"
)

// Live Mistral Vibe tests call the Mistral API, so they are opt-in:
// VIBE_E2E=1 E2E_AGENT=mistral-vibe.
func init() {
	if os.Getenv("VIBE_E2E") != "1" || os.Getenv("E2E_AGENT") != mistralVibeName {
		return
	}
	if err := isolateVibeHome(); err != nil {
		fmt.Fprintf(os.Stderr, "mistral-vibe e2e setup: %v\n", err)
		return
	}
	Register(&MistralVibe{})
	RegisterGate(mistralVibeName, 1)
}

// isolateVibeHome points this process, and everything it starts, at a private
// VIBE_HOME so the tests neither load the developer's hooks, plugins and config
// nor write sessions into their real home. Git hooks started by the tests
// inherit it, which is how Entire finds the session when it refreshes a
// transcript at commit time.
//
// The API key is the one thing taken from the real setup: Vibe keeps it in
// $VIBE_HOME/.env, which the private home does not have.
func isolateVibeHome() error {
	realHome := os.Getenv("VIBE_HOME")
	if realHome == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		realHome = filepath.Join(userHome, ".vibe")
	}
	if os.Getenv("MISTRAL_API_KEY") == "" {
		key := apiKeyFromEnvFile(filepath.Join(realHome, ".env"))
		if key == "" {
			return errors.New("MISTRAL_API_KEY is not set and not found in the Vibe .env file")
		}
		if err := os.Setenv("MISTRAL_API_KEY", key); err != nil {
			return err
		}
	}
	privateHome, err := os.MkdirTemp("", "e2e-vibe-home-*")
	if err != nil {
		return err
	}
	// The TUI has no --model flag, so the model is set in the private config.
	config := fmt.Sprintf("active_model = %q\n", mistralVibeModel())
	if err := os.WriteFile(filepath.Join(privateHome, "config.toml"), []byte(config), 0o600); err != nil {
		return err
	}
	return os.Setenv("VIBE_HOME", privateHome)
}

// mistralVibeModel is explicit because, with no model configured, a key
// without a paid Vibe plan is routed to a default model it cannot use, and Vibe
// reports that as an invalid API key.
func mistralVibeModel() string {
	if model := os.Getenv("VIBE_E2E_MODEL"); model != "" {
		return model
	}
	return mistralVibeDefaultModel
}

func apiKeyFromEnvFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), "MISTRAL_API_KEY="); ok {
			return strings.Trim(strings.TrimSpace(value), `"'`)
		}
	}
	return ""
}

type MistralVibe struct{}

func (v *MistralVibe) Name() string               { return mistralVibeName }
func (v *MistralVibe) Binary() string             { return "vibe" }
func (v *MistralVibe) EntireAgent() string        { return mistralVibeName }
func (v *MistralVibe) PromptPattern() string      { return `>` }
func (v *MistralVibe) TimeoutMultiplier() float64 { return 2.0 }
func (v *MistralVibe) IsExternalAgent() bool      { return true }
func (v *MistralVibe) Bootstrap() error           { return nil }

func (v *MistralVibe) IsTransientError(out Output, _ error) bool {
	combined := strings.ToLower(out.Stdout + out.Stderr)
	for _, pattern := range []string{
		"overloaded",
		"rate limit",
		"status 429", "status 500", "status 502", "status 503",
		"service unavailable",
		"econnreset",
		"etimedout",
		"timed out",
	} {
		if strings.Contains(combined, pattern) {
			return true
		}
	}
	return false
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

	// --trust is needed for Vibe to load the project hooks.
	model := cfg.Model
	if model == "" {
		model = mistralVibeModel()
	}
	args := []string{"--model", model, "--trust", "--auto-approve", "--output", "text", "-p", prompt}
	displayArgs := []string{"--model", model, "--trust", "--auto-approve", "--output", "text", "-p", fmt.Sprintf("%q", prompt)}

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
		Command:  v.Binary() + " " + strings.Join(displayArgs, " "),
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
	}, err
}

// StartSession opens the Vibe TUI in tmux. The tmux server inherits this
// process's environment, including the private VIBE_HOME and the API key.
func (v *MistralVibe) StartSession(_ context.Context, dir string) (Session, error) {
	name := fmt.Sprintf("vibe-test-%d", time.Now().UnixNano())
	s, err := NewTmuxSession(name, dir, []string{"ENTIRE_TEST_TTY"}, v.Binary(), "--trust", "--auto-approve")
	if err != nil {
		return nil, err
	}
	if _, err := s.WaitFor(v.PromptPattern(), 30*time.Second); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("waiting for initial prompt: %w", err)
	}
	s.stableAtSend = ""
	return s, nil
}
