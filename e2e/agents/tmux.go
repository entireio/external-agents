package agents

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// TmuxSession implements Session using tmux for PTY-based interactive agents.
type TmuxSession struct {
	name         string
	stableAtSend string   // stable content snapshot when Send was last called
	cleanups     []func() // run on Close
}

// OnClose registers a function to run when the session is closed.
func (s *TmuxSession) OnClose(fn func()) {
	s.cleanups = append(s.cleanups, fn)
}

// NewTmuxSession creates a new tmux session running the given command in dir.
// unsetEnv lists environment variable names to strip from the session.
//
// The command is wrapped with `env` to propagate PATH from the current process.
// tmux sessions inherit the tmux server's environment (not the client's), so
// without this, binaries added to PATH by the test runner would not be found.
func NewTmuxSession(name string, dir string, unsetEnv []string, command string, args ...string) (*TmuxSession, error) {
	s := &TmuxSession{name: name}

	tmuxArgs := []string{"new-session", "-d", "-s", name, "-c", dir}
	var parts []string
	parts = append(parts, "env")
	// Options (-u) must precede variable assignments for BSD env on macOS.
	for _, v := range unsetEnv {
		parts = append(parts, "-u", shellQuote(v))
	}
	parts = append(parts, "PATH="+shellQuote(os.Getenv("PATH")))
	parts = append(parts, shellQuote(command))
	for _, a := range args {
		parts = append(parts, shellQuote(a))
	}
	tmuxArgs = append(tmuxArgs, strings.Join(parts, " "))

	cmd := exec.Command("tmux", tmuxArgs...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("tmux new-session: %w\n%s", err, out)
	}
	// Keep the pane around after the command exits so we can capture error output.
	setCmd := exec.Command("tmux", "set-option", "-t", name, "remain-on-exit", "on")
	_ = setCmd.Run()
	return s, nil
}

func (s *TmuxSession) Send(input string) error {
	preSend := stableContent(s.Capture())
	// Send text and Enter separately — some TUIs can swallow Enter
	// if it arrives before the input handler finishes processing the text.
	if err := s.SendKeys(input); err != nil {
		return err
	}
	time.Sleep(200 * time.Millisecond)
	if err := s.SendKeys("Enter"); err != nil {
		return err
	}

	// Wait for the terminal to reflect the echoed input, then snapshot.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
		current := stableContent(s.Capture())
		if current != preSend {
			s.stableAtSend = current
			return nil
		}
	}
	s.stableAtSend = stableContent(s.Capture())
	return nil
}

// SendKeys sends raw tmux key names without appending Enter.
func (s *TmuxSession) SendKeys(keys ...string) error {
	args := append([]string{"send-keys", "-t", s.name}, keys...)
	cmd := exec.Command("tmux", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux send-keys: %w\n%s", err, out)
	}
	return nil
}

const (
	settleTime   = 2 * time.Second
	pollInterval = 500 * time.Millisecond
)

// stableContent returns the content with the last few lines stripped,
// so that TUI status bar updates don't prevent the settle timer.
func stableContent(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) > 3 {
		lines = lines[:len(lines)-3]
	}
	return strings.Join(lines, "\n")
}

func (s *TmuxSession) WaitFor(pattern string, timeout time.Duration) (string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid pattern: %w", err)
	}

	deadline := time.Now().Add(timeout)
	var matchedAt time.Time
	var lastStable string
	contentChanged := s.stableAtSend == "" // skip change requirement for initial waits

	for time.Now().Before(deadline) {
		content := s.Capture()
		stable := stableContent(content)

		// Bail early if the process has exited and the pattern doesn't match.
		if !re.MatchString(content) {
			if s.IsPaneDead() {
				return content, fmt.Errorf("process exited while waiting for %q\n--- pane content ---\n%s\n--- end pane content ---", pattern, content)
			}
			matchedAt = time.Time{}
			lastStable = ""
			time.Sleep(pollInterval)
			continue
		}

		if !contentChanged && stable != s.stableAtSend {
			contentChanged = true
		}

		if stable != lastStable {
			matchedAt = time.Now()
			lastStable = stable
			time.Sleep(pollInterval)
			continue
		}

		if contentChanged && time.Since(matchedAt) >= settleTime {
			return content, nil
		}

		time.Sleep(pollInterval)
	}
	content := s.Capture()
	return content, fmt.Errorf("timed out waiting for %q after %s\n--- pane content ---\n%s\n--- end pane content ---", pattern, timeout, content)
}

// IsPaneDead returns true if the process inside the tmux pane has exited.
func (s *TmuxSession) IsPaneDead() bool {
	cmd := exec.Command("tmux", "display-message", "-t", s.name, "-p", "#{pane_dead}")
	out, err := cmd.Output()
	if err != nil {
		return true
	}
	return strings.TrimSpace(string(out)) == "1"
}

func (s *TmuxSession) Capture() string {
	cmd := exec.Command("tmux", "capture-pane", "-t", s.name, "-p")
	out, _ := cmd.Output()
	return strings.TrimRight(string(out), "\n")
}

func (s *TmuxSession) Close() error {
	for _, fn := range s.cleanups {
		fn()
	}
	cmd := exec.Command("tmux", "kill-session", "-t", s.name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux kill-session: %w\n%s", err, out)
	}
	return nil
}

// shellQuote wraps s in single quotes with proper escaping for POSIX shells.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
