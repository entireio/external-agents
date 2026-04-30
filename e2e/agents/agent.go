package agents

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Output holds the result of running an agent prompt.
type Output struct {
	Command  string
	Stdout   string
	Stderr   string
	ExitCode int
}

// Option configures how an agent prompt is executed.
type Option func(*runConfig)
type runConfig struct {
	Model          string
	PermissionMode string
	PromptTimeout  time.Duration
}

// WithModel sets the model to use for the prompt.
func WithModel(model string) Option {
	return func(c *runConfig) { c.Model = model }
}

// WithPermissionMode sets the permission mode for the prompt.
func WithPermissionMode(mode string) Option {
	return func(c *runConfig) { c.PermissionMode = mode }
}

// WithPromptTimeout sets a custom timeout for the prompt execution.
func WithPromptTimeout(d time.Duration) Option {
	return func(c *runConfig) { c.PromptTimeout = d }
}

// Agent defines the interface for an AI coding agent that can be tested.
type Agent interface {
	Name() string
	// Binary returns the CLI binary name (e.g. "kiro-cli-chat").
	Binary() string
	EntireAgent() string
	PromptPattern() string
	// TimeoutMultiplier returns a factor applied to per-test timeouts.
	// Slower agents return values > 1.
	TimeoutMultiplier() float64
	RunPrompt(ctx context.Context, dir string, prompt string, opts ...Option) (Output, error)
	StartSession(ctx context.Context, dir string) (Session, error)
	// Bootstrap performs one-time CI setup (auth config, warmup, etc.).
	// Called before any tests run. Implementations should be idempotent.
	Bootstrap() error
	// IsTransientError returns true if the error from RunPrompt looks like
	// a transient API failure (e.g. 500, rate limit, network error) that
	// is worth retrying.
	IsTransientError(out Output, err error) bool
}

// Session represents an interactive terminal session with an agent.
type Session interface {
	Send(input string) error
	WaitFor(pattern string, timeout time.Duration) (string, error)
	Capture() string
	Close() error
}

// ExternalAgent is an optional interface for agents discovered via the
// external agent protocol (entire-agent-* binaries). SetupRepo uses this
// to pre-configure external_agents in settings before running `entire enable`.
type ExternalAgent interface {
	IsExternalAgent() bool
}

// registry and gates are populated by init() functions in agent implementation
// files (e.g. kiro.go). Go guarantees init() functions run sequentially, so
// no synchronization is needed. Do not call Register/RegisterGate from tests.
var registry []Agent
var gates = map[string]chan struct{}{}

// Register adds an agent to the global registry.
func Register(a Agent) {
	registry = append(registry, a)
}

// RegisterGate sets a concurrency limit for an agent's tests.
// Tests call AcquireSlot/ReleaseSlot to respect this limit.
// The limit can be overridden via E2E_CONCURRENT_TEST_LIMIT.
func RegisterGate(name string, defaultMax int) {
	max := defaultMax
	if v, err := strconv.Atoi(os.Getenv("E2E_CONCURRENT_TEST_LIMIT")); err == nil && v > 0 {
		max = v
	}
	gates[name] = make(chan struct{}, max)
}

// AcquireSlot blocks until a test slot is available for the agent or the
// context is cancelled. Returns a non-nil error if the context expires
// before a slot opens.
func AcquireSlot(ctx context.Context, a Agent) error {
	g, ok := gates[a.Name()]
	if !ok {
		return nil
	}
	select {
	case g <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ReleaseSlot frees a test slot for the agent.
func ReleaseSlot(a Agent) {
	if g, ok := gates[a.Name()]; ok {
		<-g
	}
}

// All returns all registered agents.
func All() []Agent {
	return registry
}

// runAgentCmd runs the agent binary with the given args and returns the captured
// Output. displayArgs is what gets recorded in Output.Command (typically with the
// prompt %q-quoted for log readability); args is what's actually exec'd.
func runAgentCmd(ctx context.Context, dir, binary string, args, displayArgs []string) (Output, error) {
	bin, err := exec.LookPath(binary)
	if err != nil {
		return Output{}, fmt.Errorf("%s not in PATH: %w", binary, err)
	}

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

	runErr := cmd.Run()
	exitCode := 0
	if runErr != nil {
		exitErr := &exec.ExitError{}
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	return Output{
		Command:  binary + " " + strings.Join(displayArgs, " "),
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
	}, runErr
}

// isTransient returns true if the combined stdout+stderr of out contains any
// of the given substring patterns. Used by per-agent IsTransientError methods.
func isTransient(out Output, patterns []string) bool {
	combined := out.Stdout + out.Stderr
	for _, pat := range patterns {
		if strings.Contains(combined, pat) {
			return true
		}
	}
	return false
}

// filterEnv returns env with entries matching any of the given variable names
// removed. Used to strip test-only overrides from agent processes.
func filterEnv(env []string, names ...string) []string {
	out := make([]string, 0, len(env))
	for _, e := range env {
		skip := false
		for _, name := range names {
			if strings.HasPrefix(e, name+"=") {
				skip = true
				break
			}
		}
		if !skip {
			out = append(out, e)
		}
	}
	return out
}
