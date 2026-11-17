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

// Sarvam Code lifecycle adapter.
//
// Two sarvam-specific constraints shape this adapter:
//
//  1. Hooks fire only in interactive (TUI) sessions — a headless
//     `sarvam-code exec` run never invokes them — so RunPrompt drives the
//     TUI through tmux instead of calling exec directly.
//  2. Project config is trust-gated: a directory is trusted on first run,
//     and freshly installed hooks need a one-time /hooks review before they
//     fire. A trivial exec run records directory trust without any prompt
//     (verified behavior), and the review itself is pre-seeded by writing the
//     exact [hooks.state] entries the TUI's /hooks flow would persist:
//     the CLI derives each entry's trusted_hash from the hook's command
//     alone, so the five hashes below are stable per sarvam-code version.
//     If a future CLI changes the derivation, the TUI re-shows the review
//     banner and StartSession fails loudly instead of running a test with
//     dead hooks — recapture the hashes with scripts/verify-sarvam.sh.
const (
	sarvamTimeoutMultiplier = 2.0
	// The TUI status bar: "─ Flow · Auto ──…" (also Ask/Yolo).
	sarvamPromptPattern = `Flow · `
	// Banner the TUI shows while any installed hook still needs review.
	sarvamReviewBanner = "hooks need review"
)

// [hooks.state] event keys the CLI persists per project config, in the
// snake_case form the state keys use, with the trusted_hash the CLI derives
// from entire-agent-sarvam's hook commands (captured from a live
// sarvam-code 0.58.0 /hooks review, 2026-11-16).
var sarvamHookState = []struct {
	Event string
	Hash  string
}{
	{"session_start", "sha256:1cd654ae23cc41c058583eece8fe97be06bd45dcbc4bf6b897ca2d5777994f86"},
	{"user_prompt_submit", "sha256:a2cd5c4ed3c0135b417b0d5b8dda1f5a89afb1d7bd70838ad5b24579944dd2f2"},
	{"stop", "sha256:5e31e6f47e017036524da915fee1bbf8bc93eb763a038b87a9d12a74e230d114"},
	{"pre_compact", "sha256:fd54e6e411ead01e516ff1f394ca5c777a32ca53e66bcfeb20e76791a2a36aef"},
	{"post_compact", "sha256:2e69c9d8c6f029b3768b05f272f45cf23944cbe34800e813cc6d7b755655c5a3"},
}

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

	sess, ok := session.(*sarvamSession)
	if !ok {
		return Output{}, fmt.Errorf("unexpected session type %T", session)
	}
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
	if err := seedSarvamHookTrust(dir); err != nil {
		return nil, fmt.Errorf("seed hook trust: %w", err)
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
	// With trust and hook state pre-seeded, the TUI must come up review-free.
	// A lingering banner means the trusted_hash constants no longer match
	// this sarvam-code version's derivation — hooks would silently never
	// fire, so fail loudly instead of running dead tests.
	time.Sleep(2 * time.Second)
	if strings.Contains(sess.Capture(), sarvamReviewBanner) {
		_ = sess.Close()
		return nil, errors.New("sarvam hooks still need review after trust pre-seed — the e2e adapter's trusted_hash constants are stale for this sarvam-code version; recapture them via agents/entire-agent-sarvam/scripts/verify-sarvam.sh")
	}
	sess.stableAtSend = ""
	return &sarvamSession{TmuxSession: sess}, nil
}

// seedSarvamHookTrust writes the [hooks.state] entries a completed /hooks
// review would persist: enabled = true plus the CLI's trusted_hash for each
// of the five hooks entire-agent-sarvam installs. Missing entries are
// appended; existing state for other repos is left untouched.
func seedSarvamHookTrust(repoDir string) error {
	configPath := sarvamUserConfigPath()
	if configPath == "" {
		return errors.New("cannot resolve the sarvam user config for hook trust pre-seeding")
	}
	canonical, err := filepath.EvalSymlinks(repoDir)
	if err != nil {
		canonical = repoDir
	}

	data, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	content := string(data)

	var addition strings.Builder
	for _, state := range sarvamHookState {
		key := canonical + "/.sarvam/config.toml:" + state.Event + ":0:0"
		if strings.Contains(content, `"`+key+`"`) {
			continue
		}
		addition.WriteString("\n[hooks.state.\"" + key + "\"]\n")
		addition.WriteString("enabled = true\n")
		addition.WriteString("trusted_hash = \"" + state.Hash + "\"\n")
	}
	if addition.Len() == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if existing, err := os.Stat(configPath); err == nil {
		mode = existing.Mode().Perm()
	}
	if len(content) > 0 && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return os.WriteFile(configPath, []byte(content+addition.String()), mode)
}

// sarvamUserConfigPath resolves the CLI's user config carrying [hooks.state].
func sarvamUserConfigPath() string {
	if v := os.Getenv("SARVAM_HOME"); v != "" {
		return filepath.Join(v, "config.toml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".sarvam", "config.toml")
}
