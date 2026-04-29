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

    mu sync.Mutex

    screen *ansiscreen.Screen // rendered visible terminal state
    rawBuf []byte             // rolling raw transcript for debugging, capped at 64 KiB

    cleanups     []func()
    stableAtSend string
}

func NewConPTYSession(name, dir string, unsetEnv []string, command string, args ...string) (*ConPTYSession, error)
```

ConPTY is not a tmux replacement by itself. `tmux capture-pane -p` gives us the current rendered screen; a raw ConPTY byte stream gives us terminal history plus escape sequences. For `WaitFor` to remain meaningful, Windows must reproduce **visible-screen semantics**, not "somewhere in the last 64 KiB the prompt regex matched once." So the session keeps two views:

- a **rendered screen model** used by `Capture()` / `WaitFor()`
- a **raw transcript ring buffer** used only for debugging when the rendered view is insufficient

Method-by-method:

- **`NewConPTYSession`**: builds command-line string (`command arg1 "arg with space" ...`), filters env removing `unsetEnv` entries, calls `conpty.Start(cmdLine, conpty.ConPtyWorkDir(dir), conpty.ConPtyEnv(env))`. Spawns one background goroutine that reads bytes from the conpty, appends them to `rawBuf`, and feeds them into a rendered-screen parser (`github.com/charmbracelet/x/ansi` or equivalent). Spawns a second goroutine that calls `cpty.Wait(sessCtx)` and closes a `procDone` channel. (`Wait` takes a `context.Context` — confirmed against `github.com/UserExistsError/conpty v0.1.4` via `go doc`. The session stores its own `context.Background()`-derived `sessCtx` cancelled in `Close()` so `Wait` returns when the session is torn down.)
- **`Send(input)`**: mirrors `TmuxSession.Send` logically, not byte-for-byte — captures the pre-send rendered screen, writes the translated bytes for `input`, writes `Enter`, then waits for the rendered screen to change and settle.
- **`SendKeys(keys ...string)`**: per key, look up `translateKey(k)` and write the resulting bytes to the conpty.
- **`WaitFor(pattern, timeout)`**: same settle loop and `stableContent` idea as `TmuxSession.WaitFor`, but it matches against the **rendered screen** from `Capture()`. Detects "process exited" by checking `procDone`.
- **`Capture()`**: returns the current rendered visible screen, trimmed of trailing newlines. This is the Windows analogue of tmux's `capture-pane -p`.
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

#### Screen model, not regex-stripped history

The initial version of this RFD treated ANSI stripping as "good enough." It is not. `WaitFor()` today works because tmux exposes the current pane, not because the prompt regexes are simple. With a historical byte buffer, a stale `>` or `\$\d` from earlier output can satisfy the regex before the prompt has actually returned after `Send()`.

So the Windows implementation starts with a parser-backed screen model, not as a follow-up optimization. The raw transcript still exists for artifacts and debugging, but correctness-sensitive operations (`Capture`, `WaitFor`, `stableContent`) operate on the rendered screen only.

#### Buffer cap

Keep a rolling 64 KiB `rawBuf` for debugging. When append would overflow, drop the oldest 25 % so we never grow unbounded. The rendered screen keeps only terminal state, so it does not need unbounded history either.

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

### 6. Drop `/tmp` GOCACHE overrides (and the `env` prefix)

The existing `mise-tasks/build`, `mise-tasks/test`, and `mise.toml` `test:e2e:lifecycle` task all use `env GOCACHE=/tmp/go-build-cache <command>`. `/tmp` doesn't exist on `windows-latest`, **and** the `env` utility itself is unix-only (cmd.exe / PowerShell don't have it on PATH). Options considered:

- **Keep but conditionalize via shell** — messy, three files.
- **Use `${TMPDIR}`** — inconsistent across OSes, doesn't fix the `env` issue.
- **Drop the entire `env GOCACHE=...` prefix** — Go's default cache (`%LocalAppData%\go-build` on Windows, `~/.cache/go-build` on Linux, `~/Library/Caches/go-build` on macOS) is correct for each OS. Chosen.

So `mise.toml`'s `test:e2e:lifecycle` `run = "env GOCACHE=/tmp/go-build-cache go test ..."` becomes `run = "go test ..."`. Removing only the `GOCACHE=...` and leaving `env go test ...` would still break Windows because `env` is not a Windows command — the whole prefix has to go.

CI caching can still happen via `actions/cache` keyed on `${{ runner.os }}` if we want it later — orthogonal to this change.

Note: `mise-tasks/build` and `mise-tasks/test` are bash scripts (`#!/usr/bin/env bash`). They do **not** run on Windows and the e2e workflow does not invoke them — it runs `mise run test:e2e:lifecycle` directly, which is defined inline in `mise.toml` as a portable `go test ...` command. The reason we still edit the bash scripts is purely to keep `/tmp` references from going stale; if they later get invoked from a Windows context (e.g. cross-compiling), they'd fail anyway for unrelated reasons. Not strictly required for this change — included as defensive cleanup.

### 6a. `BuildAgent` on Windows: `.exe` suffix

`e2e/build.go` `BuildAgent` produces `binPath := filepath.Join(outputDir, agentName)` and runs `go build -o binPath`. On Windows this would emit a literal extension-less file, which `exec.LookPath` and `cmd.exe`'s `PATHEXT` resolution will not find. Fix:

```go
binPath := filepath.Join(outputDir, agentName)
if runtime.GOOS == "windows" {
    binPath += ".exe"
}
```

Add a corresponding `runtime` import (already imported in this file). This is a near-certainty (not a "verify on first run" item), so it lives in the file-by-file diff rather than the risk table.

### 7. Workflow file (`.github/workflows/e2e.yml`)

```yaml
name: E2E

on:
  workflow_dispatch:
    inputs:
      mode:
        description: Which profile to run
        required: true
        default: smoke
        type: choice
        options: [smoke, full]
      pr:
        description: Optional PR number to test
        required: false
        type: string
  issue_comment:
    types: [created]

permissions:
  contents: read
  pull-requests: read

# Cancel an in-flight run when a new trigger fires for the same PR
# (or, for workflow_dispatch, the same ref). Avoids piling up Windows
# runners when a maintainer comments `/test-e2e` twice by accident or
# pushes a fixup commit during an active run.
concurrency:
  group: e2e-${{ github.event.issue.number || github.ref }}
  cancel-in-progress: true

jobs:
  # Gate: only proceed if this is a workflow_dispatch OR a PR comment
  # whose first line is exactly "/test-e2e" (optionally followed by args)
  # from a trusted author.
  #
  # Security model:
  # - `/test-e2e` is a NO-SECRETS smoke trigger only.
  # - `workflow_dispatch mode=full` is the only path allowed to use
  #   provider credentials, and only when the target ref belongs to
  #   this repository (never a fork head ref).
  #
  # This avoids the unsafe model where a maintainer comment on a fork PR
  # would run attacker-controlled code with repo secrets.
  gate:
    if: >
      github.event_name == 'workflow_dispatch' ||
      (github.event_name == 'issue_comment' &&
       github.event.issue.pull_request != null &&
       (github.event.comment.body == '/test-e2e' ||
        startsWith(github.event.comment.body, '/test-e2e ')) &&
       (github.event.comment.author_association == 'OWNER' ||
        github.event.comment.author_association == 'MEMBER' ||
        github.event.comment.author_association == 'COLLABORATOR'))
    runs-on: ubuntu-latest
    outputs:
      ref: ${{ steps.resolve.outputs.ref }}
      mode: ${{ steps.resolve.outputs.mode }}
      allow_auth: ${{ steps.resolve.outputs.allow_auth }}
    steps:
      - id: resolve
        shell: bash
        run: |
          if [[ "${{ github.event_name }}" == "issue_comment" ]]; then
            # PR-comment trigger: always smoke-only, never secrets.
            ref=$(gh api "repos/${{ github.repository }}/pulls/${{ github.event.issue.number }}" --jq .head.sha)
            echo "mode=smoke" >> "$GITHUB_OUTPUT"
            echo "allow_auth=false" >> "$GITHUB_OUTPUT"
          else
            mode="${{ inputs.mode }}"
            if [[ -n "${{ inputs.pr }}" ]]; then
              ref=$(gh api "repos/${{ github.repository }}/pulls/${{ inputs.pr }}" --jq .head.sha)
              head_repo=$(gh api "repos/${{ github.repository }}/pulls/${{ inputs.pr }}" --jq .head.repo.full_name)
            else
              ref="${{ github.sha }}"
              head_repo="${{ github.repository }}"
            fi

            allow_auth=false
            if [[ "$mode" == "full" ]]; then
              if [[ "$head_repo" != "${{ github.repository }}" ]]; then
                echo "full mode is only allowed for refs in ${{ github.repository }}, got $head_repo" >&2
                exit 1
              fi
              allow_auth=true
            fi

            echo "mode=$mode" >> "$GITHUB_OUTPUT"
            echo "allow_auth=$allow_auth" >> "$GITHUB_OUTPUT"
          fi
          echo "ref=$ref" >> "$GITHUB_OUTPUT"
        env:
          GH_TOKEN: ${{ github.token }}

  e2e-smoke:
    needs: gate
    if: needs.gate.outputs.mode == 'smoke'
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
        # Smoke profile: no secrets, safe for fork PR refs.
        env:
          E2E_NO_AUTH: "1"
        run: mise run test:e2e:lifecycle

  e2e-full:
    needs: gate
    if: needs.gate.outputs.allow_auth == 'true'
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

      - name: Bootstrap agent auth/config
        run: mise run test:e2e:bootstrap

      - name: Run full e2e lifecycle tests
        env:
          # Follow-up commit wires actual provider secrets here.
        run: mise run test:e2e:lifecycle
```

Notes:

- `issue_comment` fires for both issues and PR comments; the `github.event.issue.pull_request != null` check filters to PR-only.
- The `author_association` gate still matters, but it is no longer treated as the only boundary. It prevents drive-by workflow spam; it does **not** make fork PR refs safe for secrets.
- `actions/checkout` with the resolved PR head SHA is correct for smoke testing PR code. It is **not** used as the basis for secret-bearing runs unless the ref is confirmed to belong to the same repository.
- `fail-fast: false` so a Windows failure doesn't cancel Linux and vice-versa.
- The workflow does **not** add `permissions: pull-requests: write` or any write scopes; the gate token only needs to read PRs (`gh api repos/.../pulls/N`), which the default `github.token` can do.
- mise `tools` block: `tmux = "latest"` will fail to install on Windows. We scope it with mise's documented platform filter so the tmux install is skipped on `windows`:
  ```toml
  [tools]
  tmux = { version = "latest", os = ["linux", "macos"] }
  ```
  This is the standard mise syntax for OS-conditional tools and avoids relying on aliases like `'unix'` which are not part of mise's documented schema.

### Permissions, secrets, auth

The agents (Kiro, Pi) need API credentials to actually run. `e2e/agents/kiro.go` `Bootstrap()` and `e2e/agents/pi.go` `Bootstrap()` are both no-ops today. Six of the eight `TestLifecycle_*` tests call `RunPrompt` (or `StartSession` + `Send`), all of which spawn the agent binary against the real provider API. Without credentials, those six fail. Only `TestLifecycle_DetectAndEnable` and `TestLifecycle_HooksInstalledAfterEnable` would pass — they don't invoke the agent binary at all.

This design splits CI into two explicit profiles:

- **Smoke**: no secrets, safe to run against fork PR code, skips auth-gated tests via `E2E_NO_AUTH=1`.
- **Full**: secret-bearing, maintainer-invoked only, allowed only for refs in this repository, and runs bootstrap before tests.

This is stricter than the previous design on purpose. A maintainer comment on a fork PR is not a safe occasion to expose provider credentials to that PR's code.

The skip gate remains:

```go
// requireAgentAuth skips the test only in smoke mode.
// Local runs and full CI runs do not skip.
func requireAgentAuth(t *testing.T) {
    t.Helper()
    if os.Getenv("E2E_NO_AUTH") == "1" {
        t.Skip("skipping auth-gated test: E2E_NO_AUTH=1")
    }
}
```

The critical change is that `Bootstrap()` is now part of the **actual execution path**, not a hand-waved follow-up. We add a dedicated task:

```toml
[tasks."test:e2e:bootstrap"]
description = "Run agent bootstrap hooks (auth/config warmup) before full e2e"
dir = "e2e"
run = "go run ./bootstrap"
```

The full workflow runs that task before `mise run test:e2e:lifecycle`. `Bootstrap()` implementations stay idempotent and provider-specific, but the design no longer assumes some later reader will remember to invoke them manually.

Local full-run sequence becomes:

```sh
export <provider secrets>
mise run test:e2e:bootstrap
mise run test:e2e:lifecycle
```

Follow-up commit on this branch: wire actual provider secrets into the `e2e-full` job env and implement `Bootstrap()` for each agent to write the appropriate config file. The exact secret names and config paths still get discovered in that follow-up, but the control flow for using them is now fixed in this RFD.

## Risks and open questions

| Risk | Likelihood | Mitigation |
|---|---|---|
| Full authenticated runs accidentally target fork PR code | Medium | Full profile is `workflow_dispatch` only and explicitly rejects refs whose head repo is not `${{ github.repository }}`. |
| Rendered-screen parser still diverges from agent TUI behavior | Medium | Make screen rendering part of v1, keep raw transcript artifacts, and add unit tests around CR/LF, cursor movement, and prompt redraw cases. |
| Agents need API keys, but bootstrap task/config drifts from workflow expectations | Medium | Full profile runs `mise run test:e2e:bootstrap` in the same workflow immediately before tests. |
| `cmd.Process.Kill()` on Windows leaks child processes | Low | Acceptable for short-lived agent calls; can upgrade to Job Objects later. |
| ConPTY `Send` 200ms-then-Enter timing is wrong for a non-tmux PTY (no tmux input buffering) | Low | If we see truncated input, drop the sleep — `\r` can be appended to the same write. |
| `conpty.Start` / `ConPty.Wait(ctx)` API drift between v0.1.4 and a later release | Low | API verified against v0.1.4 (`go doc`); pin in `go.mod`. |

## Out of scope (explicitly)

- macOS runner (`macos-latest`). Mac uses the existing tmux path; can be added to the matrix trivially later.
- Caching Go build cache via `actions/cache`.
- Exact provider secret names and provider-specific auth file contents.
- Refactoring `kiro.go` / `pi.go` `RunPrompt` to share logic. They're near-duplicates but that's a different cleanup.

## File-by-file diff summary

| File | Change |
|---|---|
| `e2e/go.mod` / `go.sum` | Add `github.com/UserExistsError/conpty` |
| `e2e/agents/tmux.go` | Add `//go:build !windows` |
| `e2e/agents/proc_unix.go` | **NEW** — `configureCmdProcAttr` (POSIX) |
| `e2e/agents/proc_windows.go` | **NEW** — `configureCmdProcAttr` (Windows) |
| `e2e/agents/conpty_windows.go` | **NEW** — `ConPTYSession`, `translateKey`, rendered-screen capture, raw transcript buffer |
| `e2e/agents/conpty_windows_test.go` | **NEW** — unit tests for `translateKey` plus screen-rendering edge cases |
| `e2e/agents/session_unix.go` | **NEW** — `newInteractiveSession` → tmux |
| `e2e/agents/session_windows.go` | **NEW** — `newInteractiveSession` → ConPTY |
| `e2e/agents/kiro.go` | Use `configureCmdProcAttr`; use `newInteractiveSession`; drop `s.stableAtSend = ""` |
| `e2e/agents/pi.go` | Use `configureCmdProcAttr`; use `newInteractiveSession`; drop `s.stableAtSend = ""` |
| `e2e/build.go` | Append `.exe` to `binPath` when `runtime.GOOS == "windows"` |
| `e2e/setup_test.go` | Skip tmux preflight on Windows; `GIT_CONFIG_GLOBAL` → empty temp file; add `requireAgentAuth(t)` helper that `t.Skip`s when `E2E_NO_AUTH=1` |
| `e2e/lifecycle_test.go` | Call `requireAgentAuth(t)` at the top of each prompt-running test (all except `DetectAndEnable` and `HooksInstalledAfterEnable`) |
| `mise.toml` | Drop the entire `env GOCACHE=/tmp/...` prefix from `test:e2e:lifecycle` (just `go test ...`); add `test:e2e:bootstrap`; scope `tmux` tool to `os = ["linux", "macos"]` |
| `mise-tasks/build` | Drop `GOCACHE=/tmp/...` (defensive only — script is bash-only) |
| `mise-tasks/test` | Drop `GOCACHE=/tmp/...` (defensive only — script is bash-only) |
| `.github/workflows/e2e.yml` | **NEW** — smoke/full split: `/test-e2e` PR-comment smoke trigger, same-repo-only authenticated `workflow_dispatch`, OS matrix |

## Order of work

1. Cross-platform process attrs (`proc_unix.go`, `proc_windows.go`) — unblocks Windows compilation.
2. Build-tag tmux.go.
3. Add ConPTY dependency.
4. ConPTY session + rendered-screen tests.
5. Session factory + wire up Pi/Kiro StartSession.
6. `setup_test.go` fixes (tmux preflight + `GIT_CONFIG_GLOBAL` + `requireAgentAuth(t)` helper).
7. `lifecycle_test.go` add `requireAgentAuth(t)` calls at top of prompt-running tests.
8. `e2e/build.go` `.exe` suffix on Windows.
9. Drop GOCACHE overrides; add `test:e2e:bootstrap`; scope mise `tmux` tool to unix.
10. Workflow file with explicit smoke/full profiles.
11. Push branch, manually trigger `workflow_dispatch mode=smoke`, iterate on whatever breaks first.
12. Follow-up commit on this branch: wire agent API secrets into the `e2e-full` job and implement agent `Bootstrap()` config writing.

Step 1 is the hardest-to-reverse single decision (cross-platform refactor pattern) and it gates everything else, so it goes first. Step 4 is intentionally earlier and stricter than before: if the screen model is wrong, the interactive test semantics are wrong, so we should prove that before wiring workflow triggers around it. Step 10 lands the security boundary in the same change that introduces the trigger, rather than treating secrets as an afterthought.
