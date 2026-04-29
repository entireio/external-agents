# Windows E2E Tests — Design

## Goal

Run the existing `e2e/lifecycle_test.go` suite on Windows in CI so we can reproduce and verify Windows-specific bugs (like the one this branch is named for) without leaving the local machine.

The same suite already runs on Linux locally. We want:

1. A new on-demand workflow (`workflow_dispatch` + `/test-e2e` PR-comment trigger) so e2e doesn't run on every PR.
2. An OS matrix of `ubuntu-latest` and `windows-latest`.
3. The interactive `TestLifecycle_InteractiveSession` test to actually work on Windows (not skip), via ConPTY instead of tmux.

## Why this isn't a one-line workflow change

The e2e package today has three Linux-only assumptions baked in:

1. **`e2e/agents/tmux.go`** — drives Kiro/Pi interactive sessions via the `tmux` CLI. Windows has no tmux.
2. **`e2e/agents/kiro.go` and `pi.go`** — set `syscall.SysProcAttr{Setpgid: true}` and call `syscall.Kill(-pid, ...)` on cancellation. Both are POSIX-only — `Setpgid` doesn't exist on `windows/syscall` and `syscall.Kill` doesn't either, so the package won't even **compile** on `GOOS=windows`.
3. **`e2e/setup_test.go`** — preflight checks for `tmux` in `PATH`, and sets `GIT_CONFIG_GLOBAL=/dev/null`. Neither is correct on Windows.

So Windows support is: cross-platform refactor of the agent runner + a real ConPTY session impl + a couple of small test-setup fixes + the new workflow.

## ConPTY in one paragraph

**ConPTY** (Pseudo Console) is the native Windows pseudo-terminal API, available since Windows 10 1809. It's the OS-level thing that makes interactive TUIs work — the equivalent role tmux plays for us on Linux. PowerShell is *not* a substitute: it's a shell that runs *inside* a terminal, not a terminal provider, so child processes started under PowerShell with `Start-Process -RedirectStandardInput` see a pipe (not a TTY) and switch off interactive mode. We need a real PTY, and on Windows that means ConPTY. We'll use [`github.com/UserExistsError/conpty`](https://github.com/UserExistsError/conpty) (MIT, ~470⭐, actively used) as the Go wrapper.

ConPTY is lower-level than tmux: it gives us a process attached to a pseudo-console with raw read/write pipes. There's no built-in scrollback buffer and no named keys — we provide both ourselves.

## Component plan

### 1. Cross-platform process attributes

New build-tagged helpers in `e2e/agents/`:

- `proc_unix.go` (`//go:build !windows`):
  ```go
  func configureCmdProcAttr(cmd *exec.Cmd) {
      cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
      cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
  }
  ```
- `proc_windows.go` (`//go:build windows`):
  ```go
  func configureCmdProcAttr(cmd *exec.Cmd) {
      cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000200} // CREATE_NEW_PROCESS_GROUP
      cmd.Cancel = func() error { return cmd.Process.Kill() }
  }
  ```

`kiro.go` and `pi.go` `RunPrompt` switch from inline syscall code to `configureCmdProcAttr(cmd)`.

Trade-off on Windows: we lose the `kill -pid` semantics that nuke the whole process group. `cmd.Process.Kill()` only kills the leader. For long-lived background TUIs this can leak children; for our short non-interactive `RunPrompt` invocations under context timeout, the child process is the agent itself and that's what we kill.

### 2. tmux.go gets `//go:build !windows`

Pure compile-out — tmux driver only exists on unix. Single-line change at the top of the file.

### 3. ConPTY session

New file `e2e/agents/conpty_windows.go` (`//go:build windows`).

```go
type ConPTYSession struct {
    cpty     *conpty.ConPty
    procDone <-chan struct{}
    exitErr  error

    bufMu sync.Mutex
    buf   []byte // ANSI-stripped rolling buffer, capped at 64 KiB

    cleanups     []func()
    stableAtSend string
}

func NewConPTYSession(name, dir string, unsetEnv []string, command string, args ...string) (*ConPTYSession, error)
```

Method-by-method:

- **`NewConPTYSession`**: builds command-line string (`command arg1 "arg with space" ...`), filters env removing `unsetEnv` entries, calls `conpty.Start(cmdLine, conpty.ConPtyWorkDir(dir), conpty.ConPtyEnv(env))`. Spawns one background goroutine reading from the conpty into the buffer (with mutex), and a second goroutine that calls `cpty.Wait()` and closes a `procDone` channel.
- **`Send(input)`**: mirrors `TmuxSession.Send` exactly — captures pre-state, calls `SendKeys(input)`, sleeps 200ms, calls `SendKeys("Enter")`, then runs the same 5-second "wait for content to change" loop using `stableContent`.
- **`SendKeys(keys ...string)`**: per key, look up `translateKey(k)` and write the resulting bytes to the conpty.
- **`WaitFor(pattern, timeout)`**: same loop and same `stableContent` settle logic as `TmuxSession.WaitFor`. Detects "process exited" by checking `procDone`.
- **`Capture()`**: returns the current ANSI-stripped buffer, trimmed of trailing newlines (matches tmux's `capture-pane -p` shape closely enough for the prompt-pattern regexes the tests use).
- **`Close()`**: runs cleanups, closes the conpty (terminates the process).

#### Key translator (shared by tmux SendKeys vocabulary)

```go
func translateKey(k string) string {
    switch k {
    case "Enter":              return "\r"
    case "Tab":                return "\t"
    case "Escape", "Esc":      return "\x1b"
    case "BSpace", "Backspace": return "\x7f"
    case "Up":                 return "\x1b[A"
    case "Down":               return "\x1b[B"
    case "Right":              return "\x1b[C"
    case "Left":               return "\x1b[D"
    case "Space":              return " "
    }
    if len(k) == 3 && (k[0] == 'C' || k[0] == 'c') && k[1] == '-' {
        c := k[2]
        if c >= 'a' && c <= 'z' { return string([]byte{c - 'a' + 1}) }
        if c >= 'A' && c <= 'Z' { return string([]byte{c - 'A' + 1}) }
    }
    return k // literal text
}
```

Vocabulary chosen to match the tmux key names already used by `TmuxSession.SendKeys`, so test code stays portable.

#### ANSI stripping

Tests look for prompt patterns like `>` (Kiro) and `\$\d` (Pi) — simple regex matches. We don't need a full terminal emulator. A regex strip of CSI sequences (`\x1b\[[0-9;?]*[a-zA-Z]`) plus OSC sequences (`\x1b\][^\x07]*\x07`) plus stripping `\r` (so CR-rewrites collapse) is sufficient. If we discover flakiness from TUI redraws ghosting in the buffer, we can upgrade to a virtual screen later (`charmbracelet/x/ansi` has one).

#### Buffer cap

Rolling 64 KiB. When append would overflow, drop the oldest 25 % so we never grow unbounded. Tests don't need long history — only the recent prompt area matters for regex matching.

### 4. Session factory

Tiny build-tagged constructor so `kiro.go` and `pi.go` don't need build tags themselves:

- `e2e/agents/session_unix.go` (`//go:build !windows`):
  ```go
  func newInteractiveSession(name, dir string, unsetEnv []string, cmd string, args ...string) (Session, error) {
      return NewTmuxSession(name, dir, unsetEnv, cmd, args...)
  }
  ```
- `e2e/agents/session_windows.go` (`//go:build windows`):
  ```go
  func newInteractiveSession(name, dir string, unsetEnv []string, cmd string, args ...string) (Session, error) {
      return NewConPTYSession(name, dir, unsetEnv, cmd, args...)
  }
  ```

`Pi.StartSession` / `Kiro.StartSession` switch from `NewTmuxSession(...)` to `newInteractiveSession(...)`. The redundant `s.stableAtSend = ""` lines (it's already the zero value after construction) get dropped — that removes the only direct field access through the concrete tmux type.

### 5. setup_test.go cross-platform fixes

Two small changes:

```go
// Old:
if _, err := exec.LookPath("tmux"); err != nil { /* warn */ }

// New:
if runtime.GOOS != "windows" {
    if _, err := exec.LookPath("tmux"); err != nil { /* warn */ }
}
```

```go
// Old:
os.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")

// New:
gc, _ := os.CreateTemp("", "e2e-gitconfig-*")
gc.Close()
os.Setenv("GIT_CONFIG_GLOBAL", gc.Name())
// (cleanup on exit via t.Cleanup or defer in TestMain via os.Remove)
```

Empty temp file is functionally equivalent to `/dev/null` for git's purposes (an empty file = no global config) and works on every OS.

### 6. Drop `/tmp` GOCACHE overrides

The existing `mise-tasks/build`, `mise-tasks/test`, and `mise.toml` `test:e2e:lifecycle` task all set `env GOCACHE=/tmp/go-build-cache`. `/tmp` doesn't exist on `windows-latest`. Options considered:

- **Keep but conditionalize via shell** — messy, three files.
- **Use `${TMPDIR}`** — inconsistent across OSes.
- **Drop the override entirely** — Go's default cache (`%LocalAppData%\go-build` on Windows, `~/.cache/go-build` on Linux, `~/Library/Caches/go-build` on macOS) is correct for each OS. Chosen.

CI caching can still happen via `actions/cache` keyed on `${{ runner.os }}` if we want it later — orthogonal to this change.

### 7. Workflow file (`.github/workflows/e2e.yml`)

```yaml
name: E2E

on:
  workflow_dispatch:
  issue_comment:
    types: [created]

permissions:
  contents: read
  pull-requests: read

jobs:
  # Gate: only proceed if this is a workflow_dispatch OR a PR comment containing "/test-e2e".
  gate:
    if: >
      github.event_name == 'workflow_dispatch' ||
      (github.event_name == 'issue_comment' &&
       github.event.issue.pull_request != null &&
       contains(github.event.comment.body, '/test-e2e'))
    runs-on: ubuntu-latest
    outputs:
      ref: ${{ steps.resolve.outputs.ref }}
    steps:
      - id: resolve
        shell: bash
        run: |
          if [[ "${{ github.event_name }}" == "issue_comment" ]]; then
            # Resolve PR head SHA so we test the PR's actual code.
            ref=$(gh api "repos/${{ github.repository }}/pulls/${{ github.event.issue.number }}" --jq .head.sha)
          else
            ref="${{ github.sha }}"
          fi
          echo "ref=$ref" >> "$GITHUB_OUTPUT"
        env:
          GH_TOKEN: ${{ github.token }}

  e2e:
    needs: gate
    strategy:
      fail-fast: false
      matrix:
        os: [ubuntu-latest, windows-latest]
    runs-on: ${{ matrix.os }}
    steps:
      - uses: actions/checkout@de0fac2e4500dabe0009e67214ff5f5447ce83dd # v6.0.2
        with:
          ref: ${{ needs.gate.outputs.ref }}

      - uses: jdx/mise-action@c37c93293d6b742fc901e1406b8f764f6fb19dac # v2.4.4

      - name: Run e2e lifecycle tests
        run: mise run test:e2e:lifecycle
```

Notes:

- `issue_comment` fires for both issues and PR comments; the `github.event.issue.pull_request != null` check filters to PR-only.
- `actions/checkout` with the resolved PR head SHA — without this, `issue_comment` checkouts default to `main`, which would test the wrong code.
- `fail-fast: false` so a Windows failure doesn't cancel Linux and vice-versa.
- The mise `tools` block in `mise.toml` already lists `tmux = "latest"`. mise will install tmux on the runners (works for Linux; on Windows mise should ignore the tmux install since the tmux task target won't be hit there because our Go code uses ConPTY instead, so we never invoke a tmux binary on Windows). If mise errors trying to install tmux on Windows, fix is: scope the tool with `[tools.'unix']` or skip — to be confirmed during first CI run.

### Permissions, secrets, auth

The agents (Kiro, Pi) likely need API credentials to actually run. `e2e/agents/kiro.go` `Bootstrap()` is currently a no-op with a comment "On CI, write config for non-interactive auth if needed" — meaning auth isn't yet wired up for CI.

This design intentionally doesn't tackle CI auth — that's a separate concern. First CI run will likely fail with "no credentials" on prompt execution. Follow-up work: set agent API keys as repo secrets, wire them into the workflow `env`, and update `Bootstrap()` to write the appropriate config file.

We'll discover the exact secret names and config paths on the first failed run and add them as a follow-up commit on this branch.

## Risks and open questions

| Risk | Likelihood | Mitigation |
|---|---|---|
| ConPTY raw stream has TUI redraw artifacts that break prompt-pattern regex | Medium | Start simple. If flaky, swap to `charmbracelet/x/ansi` virtual screen. |
| Agent binaries fail because path resolution differs (`.exe` suffix) | High | `e2e/setup_test.go BuildAgent` will need to produce `.exe` on Windows. Verify on first run. |
| `mise` tries to install tmux on Windows and fails | Medium | Move tmux tool into a unix-only spec, or wrap the install. To be confirmed. |
| Agents need API keys, no secrets set up | High | Out of scope for this PR. Follow-up. |
| `cmd.Process.Kill()` on Windows leaks child processes | Low | Acceptable for short-lived agent calls; can upgrade to Job Objects later. |

## Out of scope (explicitly)

- macOS runner (`macos-latest`). Mac uses the existing tmux path; can be added to the matrix trivially later.
- Caching Go build cache via `actions/cache`.
- CI auth wiring for the agents.
- A virtual screen / full terminal emulator. Plain ANSI-strip first; upgrade only if needed.
- Refactoring `kiro.go` / `pi.go` `RunPrompt` to share logic. They're near-duplicates but that's a different cleanup.

## File-by-file diff summary

| File | Change |
|---|---|
| `e2e/go.mod` / `go.sum` | Add `github.com/UserExistsError/conpty` |
| `e2e/agents/tmux.go` | Add `//go:build !windows` |
| `e2e/agents/proc_unix.go` | **NEW** — `configureCmdProcAttr` (POSIX) |
| `e2e/agents/proc_windows.go` | **NEW** — `configureCmdProcAttr` (Windows) |
| `e2e/agents/conpty_windows.go` | **NEW** — `ConPTYSession`, `translateKey`, ANSI stripper |
| `e2e/agents/conpty_windows_test.go` | **NEW** — unit tests for `translateKey` |
| `e2e/agents/session_unix.go` | **NEW** — `newInteractiveSession` → tmux |
| `e2e/agents/session_windows.go` | **NEW** — `newInteractiveSession` → ConPTY |
| `e2e/agents/kiro.go` | Use `configureCmdProcAttr`; use `newInteractiveSession`; drop `s.stableAtSend = ""` |
| `e2e/agents/pi.go` | Use `configureCmdProcAttr`; use `newInteractiveSession`; drop `s.stableAtSend = ""` |
| `e2e/setup_test.go` | Skip tmux preflight on Windows; `GIT_CONFIG_GLOBAL` → empty temp file |
| `mise.toml` | Drop `GOCACHE=/tmp/...` from `test:e2e:lifecycle` |
| `mise-tasks/build` | Drop `GOCACHE=/tmp/...` |
| `mise-tasks/test` | Drop `GOCACHE=/tmp/...` |
| `.github/workflows/e2e.yml` | **NEW** — workflow_dispatch + /test-e2e PR comment trigger, OS matrix |

## Order of work

1. Cross-platform process attrs (`proc_unix.go`, `proc_windows.go`) — unblocks Windows compilation.
2. Build-tag tmux.go.
3. Add ConPTY dependency.
4. ConPTY session + tests for `translateKey`.
5. Session factory + wire up Pi/Kiro StartSession.
6. setup_test.go fixes.
7. Drop GOCACHE overrides.
8. Workflow file.
9. Push branch, manually trigger workflow once via `workflow_dispatch`, iterate on whatever breaks first.
