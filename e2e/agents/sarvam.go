package agents

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Sarvam Code lifecycle adapter.
//
// Two sarvam-specific constraints shape this adapter:
//
//  1. Hooks fire only in interactive (TUI) sessions — a headless
//     `sarvam-code exec` run never invokes them — so RunPrompt drives the
//     TUI through tmux instead of calling exec directly.
//  2. Project config is trust-gated: a directory is trusted on first run, and
//     freshly installed hooks need a one-time /hooks review. A trivial exec
//     run records directory trust without any prompt (verified behavior), and
//     the review is automated through tmux keys the way a user does it.
const (
	sarvamTimeoutMultiplier = 2.0
	// The TUI status bar: "─ Flow · Auto ──…" (also Ask/Yolo).
	sarvamPromptPattern = `Flow · `
	// Banner the TUI shows until every installed hook is reviewed.
	sarvamReviewBanner = "hooks need review"
	// Number of hooks entire-agent-sarvam installs.
	sarvamInstalledHooks = 5
)

func init() {
	// Sarvam Code needs a logged-in CLI with network access, so the suite
	// only runs when explicitly requested.
	if os.Getenv("E2E_AGENT") != "sarvam" {
		return
	}
	Register(&Sarvam{})
	RegisterGate("sarvam", 1)
}

type Sarvam struct{}

type sarvamSession struct {
	*TmuxSession
}

func (s *sarvamSession) WaitFor(pattern string, timeout time.Duration) (string, error) {
	return s.TmuxSession.WaitFor(pattern, time.Duration(float64(timeout)*sarvamTimeoutMultiplier))
}

func (s *Sarvam) Name() string               { return "sarvam" }
func (s *Sarvam) Binary() string             { return "sarvam-code" }
func (s *Sarvam) EntireAgent() string         { return "sarvam" }
func (s *Sarvam) PromptPattern() string      { return sarvamPromptPattern }
func (s *Sarvam) TimeoutMultiplier() float64 { return sarvamTimeoutMultiplier }
func (s *Sarvam) IsExternalAgent() bool      { return true }

func (s *Sarvam) IsTransientError(out Output, _ error) bool {
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

func (s *Sarvam) Bootstrap() error {
	if _, err := exec.LookPath(s.Binary()); err != nil {
		return fmt.Errorf("%s not in PATH: %w", s.Binary(), err)
	}
	return nil
}

// trustRepo runs a throwaway headless prompt so sarvam records directory
// trust in its user config the way first-run acceptance would. Without it the
// TUI opens with a trust dialog instead of the composer.
func (s *Sarvam) trustRepo(ctx context.Context, dir string) error {
	bin, err := exec.LookPath(s.Binary())
	if err != nil {
		return fmt.Errorf("%s not in PATH: %w", s.Binary(), err)
	}
	trustCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(trustCtx, bin, "exec", "--skip-git-repo-check", "-C", dir, "Reply with the single word: ok")
	cmd.Dir = dir
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("trust run for %s failed: %w\n%s", dir, err, out)
	}
	return nil
}

func (s *Sarvam) RunPrompt(ctx context.Context, dir string, prompt string, _ ...Option) (Output, error) {
	session, err := s.StartSession(ctx, dir)
	if err != nil {
		return Output{}, err
	}
	defer func() { _ = session.Close() }()

	sess := session.(*sarvamSession)
	display := s.Binary() + " (tmux) " + fmt.Sprintf("%q", prompt)
	if _, err := sess.SendAndWait(prompt, s.PromptPattern(), 3*time.Minute); err != nil {
		return Output{Command: display, Stdout: sess.Capture()}, err
	}
	return Output{Command: display, Stdout: sess.Capture()}, nil
}

func (s *Sarvam) StartSession(ctx context.Context, dir string) (Session, error) {
	if err := s.trustRepo(ctx, dir); err != nil {
		return nil, err
	}

	name := fmt.Sprintf("sarvam-test-%d", time.Now().UnixNano())
	sess, err := NewTmuxSession(name, dir, []string{"ENTIRE_TEST_TTY"}, s.Binary())
	if err != nil {
		return nil, err
	}
	if err := sess.SetBusyPattern(`\(esc to interrupt\)`); err != nil {
		_ = sess.Close()
		return nil, fmt.Errorf("compile sarvam busy pattern: %w", err)
	}
	if _, err := sess.WaitFor(s.PromptPattern(), 60*time.Second); err != nil {
		_ = sess.Close()
		return nil, fmt.Errorf("waiting for initial prompt: %w", err)
	}
	// Give the TUI a moment to paint any hook-review banner before deciding
	// whether the review flow is needed.
	time.Sleep(2 * time.Second)
	if strings.Contains(sess.Capture(), sarvamReviewBanner) {
		if err := reviewSarvamHooks(sess); err != nil {
			_ = sess.Close()
			return nil, err
		}
	}
	sess.stableAtSend = ""
	return &sarvamSession{TmuxSession: sess}, nil
}

// reviewSarvamHooks drives the one-time /hooks review through tmux keys:
// for each installed hook, open its row, trust it (t), enable it (space),
// and return to the table. This is the same flow a user performs by hand.
func reviewSarvamHooks(sess *TmuxSession) error {
	keys := func(keys ...string) error {
		for _, key := range keys {
			if err := sess.SendKeys(key); err != nil {
				return err
			}
			time.Sleep(400 * time.Millisecond)
		}
		return nil
	}

	if err := keys("/hooks"); err != nil {
		return err
	}
	if err := keys("Enter"); err != nil {
		return err
	}
	time.Sleep(1 * time.Second)
	for i := 0; i < sarvamInstalledHooks; i++ {
		if err := keys("Enter", "t", "Space", "Escape"); err != nil {
			return err
		}
		keys("Down")
	}
	if err := keys("Escape"); err != nil {
		return err
	}
	time.Sleep(1 * time.Second)

	if strings.Contains(sess.Capture(), sarvamReviewBanner) {
		return errors.New("sarvam hooks still need review after automated /hooks pass")
	}
	return nil
}
