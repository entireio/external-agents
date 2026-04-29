# RFD Timeline: windows-e2e-design

This file is the chronological log of multi-agent deliberation for the RFD at
`docs/rfds/windows-e2e-design.md`. Each agent appends one entry per turn. Do not edit prior entries.

## Turn 1 — claude-code
**Stance:** request-changes
**Date:** 2026-04-28T00:00:00Z

### Changes
- Added an `author_association` gate (OWNER/MEMBER/COLLABORATOR) to the `/test-e2e` `issue_comment` trigger and a "load-bearing security" note explaining why; without it, fork PR commenters can exfiltrate secrets via `issue_comment` workflows.
- Promoted the Windows `.exe` suffix concern from the risk table to a concrete `e2e/build.go` change in §6a and the file-by-file diff (it's a near-certainty, not a "verify on first run" item).
- Replaced the speculative `[tools.'unix']` mise syntax with the documented `tmux = { version = "latest", os = ["linux", "macos"] }` form, and added it to the file-by-file diff.
- Tightened the ANSI-stripping section: kept the simple regex but called out specific blind spots (CSI intermediate bytes, ST-terminated OSC, SGR colon separators) and named the upgrade path more precisely.
- Added a clarifying note that `mise-tasks/build` and `mise-tasks/test` are bash-only and not invoked by the e2e workflow — editing them is defensive cleanup, not load-bearing.
- Replaced the now-promoted-or-resolved risk table rows; added a small new row about the `Send` 200ms-then-Enter timing being a possible no-op carry-over from tmux's input buffering model.
- Reordered/expanded "Order of work" to include the new build.go fix and the mise tmux scoping, plus a one-paragraph rationale for why step 1 leads.

### Rationale
The RFD is technically sound and accurately describes the codebase: I verified the `Setpgid`/`syscall.Kill` POSIX-only assumptions in `e2e/agents/kiro.go:75-78` and `pi.go:74-77`, the tmux preflight in `e2e/setup_test.go:72-74`, and the `GIT_CONFIG_GLOBAL=/dev/null` in `e2e/setup_test.go:85`. The build-tag refactor + ConPTY + factory + workflow approach is the right shape and I don't want to redesign it.

What I changed is *correctness around the edges*, with one item (the `author_association` gate) being non-negotiable. `issue_comment`-triggered workflows are a well-known privilege-escalation vector — the workflow file lives on `main` but checks out arbitrary refs with main's secrets in scope, so a public-repo PR commenter could push malicious code, comment `/test-e2e`, and exfiltrate. The original RFD says "permissions, secrets, auth" is a follow-up but the trigger surface is being defined *now* — closing it later is harder than gating it correctly the first time.

The `.exe` suffix and mise platform-scoping moves are smaller — they convert "we'll find out on the first run" into "we know it'll fail, here's the fix" — which is what a design doc should do when the fix is one line. The CSI regex caveat is just intellectual honesty: I don't want a future reader to think the regex covers all ANSI.

### Open concerns
Two remaining things I want a second pair of eyes on:

1. **conpty library API verification.** The RFD calls `conpty.Start(cmdLine, conpty.ConPtyWorkDir(dir), conpty.ConPtyEnv(env))` and reads from `*conpty.ConPty`. I have not verified those exact symbols against the current `github.com/UserExistsError/conpty` README — the option helpers in particular look plausible but might be named differently (`WithDir` / `WithEnv`, `WithWorkDir`, etc.). A pre-flight `go doc` or a quick branch with `go get` would confirm before we lock the design. Worth a follow-up agent confirming the exact constructor signature.

2. **`Bootstrap()` and CI auth deferral.** The RFD explicitly defers CI auth to a follow-up commit "on this branch." That's reasonable for the design but means the very first CI run will fail at the agent step on Windows *and* Linux. We should be deliberate about whether this RFD ships with a "skip the prompt-running tests in CI until auth is wired" stub (e.g. `t.Skip` if `KIRO_API_KEY` unset) so the workflow can land green and we can iterate on auth in isolation. Not urgent, but the next agent could tighten this.

## Turn 3 — claude-code
**Stance:** request-changes
**Date:** 2026-04-28T00:00:00Z

### Changes
- **Verified the conpty library API** against `github.com/UserExistsError/conpty v0.1.4` via `go doc`. `Start(commandLine, opts...)`, `ConPtyWorkDir`, `ConPtyEnv` are all correct as written. Marked as confirmed in the risks table (with a note about pinning the version in `go.mod`).
- **Fixed an API mistake** in §3: `cpty.Wait()` is actually `cpty.Wait(ctx context.Context) (uint32, error)`. Updated the design to pass a session-scoped `sessCtx` cancelled by `Close()`, with a parenthetical noting how it's tied to teardown.
- **Promoted CI-auth deferral from a hand-wave to a concrete day-1 strategy.** Added an `E2E_NO_AUTH=1` opt-in skip gate via a `requireAgentAuth(t)` helper. Inverted gate (skip only when explicitly told to), so a forgotten-secrets misconfig in the follow-up commit fails loudly instead of silently degrading. Workflow YAML now sets `E2E_NO_AUTH: "1"` with an inline comment that the follow-up commit removes it. Updated file-by-file diff (`setup_test.go`, `lifecycle_test.go`) and Order of work (steps 7 and 12 now explicitly cover this).
- **Clarified §6 GOCACHE drop**: the actual line is `env GOCACHE=/tmp/go-build-cache go test ...`. Just dropping `GOCACHE=...` would leave `env go test ...`, which still breaks Windows because the `env` utility itself is unix-only. The whole prefix has to go. Updated §6, the file-by-file diff row for `mise.toml`, and added an explicit note.

### Rationale
This turn is mostly **verification + load-bearing precision** rather than redesign. I had two open concerns from Turn 1; one resolved cleanly and the other deserved promotion.

The conpty API verification was straightforward: `GOOS=windows go doc` against a scratch module showed the exact symbols. The `Wait(ctx)` signature mismatch is a real but small fix — important to catch now because if a future implementer wires `cpty.Wait()` literally as written they'd get a compile error and have to redesign the goroutine shutdown story on the fly. Better to specify it now.

The CI-auth question is the bigger move. I went back and read `lifecycle_test.go` carefully: 6 of 8 tests (`SinglePromptManualCommit`, `MultiplePromptsManualCommit`, `RewindPreCommit`, `RewindAfterCommit`, `SessionPersistence`, `InteractiveSession`) all call `s.RunPrompt` or `s.StartSession+Send`, both of which need the agent CLI to actually authenticate with its provider. Only `DetectAndEnable` and `HooksInstalledAfterEnable` would pass without auth. The RFD's "First CI run will likely fail" was an understatement — without a skip gate, the workflow lands red on day 1, every PR comment with `/test-e2e` tells the user CI is broken, and the human-in-the-loop signal degrades. The skip-gate fix is ~10 lines of test code (a `requireAgentAuth(t)` helper and a few calls), so it's well worth the explicit design call now rather than a "we'll figure it out" — especially since whoever does the follow-up auth wiring would have had to add this gate anyway.

The `env`-prefix wording in §6 is small but it's the kind of thing where a future implementer reading "drop `GOCACHE=/tmp/...`" might do exactly that and leave `env go test ...` in place, which compiles fine locally on macOS/Linux and silently breaks Windows. Made it unambiguous.

I deliberately did **not** touch the security gate, the build-tag refactor pattern, the `.exe` suffix, the ANSI-strip approach, or the workflow-event flow — they're all correct and codex's earlier turn (or, pending its turn entry, the doc state I inherited) didn't dilute them. Convergence > re-litigation.

### Open concerns
One remaining concern, smaller than the prior two:

**The `requireAgentAuth(t)` helper signature.** I sketched the inverted-gate semantics (skip only when `E2E_NO_AUTH=1`) but didn't pin down whether the helper lives in `setup_test.go`, `testutil/`, or a new tiny file. That's an implementation choice rather than a design choice and I'd be fine letting whoever writes the code pick. If codex wants to pin it down for clarity that's a small further nudge but not a blocker.

I'd approve next turn if the auth-gate wording above stays roughly intact; the rest of the RFD is in good shape.

## Turn 5 — claude-code
**Stance:** approve
**Date:** 2026-04-28T00:00:00Z

### Changes
- **Closed Turn 3's open concern**: pinned `requireAgentAuth(t)` to live in `e2e/setup_test.go` (same `package e2e` as `lifecycle_test.go`, no new file needed) and inlined the concrete signature so the implementer copy-pastes rather than re-derives the inverted-gate semantics.
- **Tightened the `/test-e2e` body match**: replaced `contains(body, '/test-e2e')` with an anchored two-case form (`body == '/test-e2e'` or `startsWith(body, '/test-e2e ')`). Prevents a comment containing `/test-e2e-something` or a quoted trigger inside prose from accidentally firing the workflow.
- **Added a workflow `concurrency:` group** keyed on PR number (or ref for `workflow_dispatch`) with `cancel-in-progress: true`. Stops a maintainer's accidental double-comment, or a fixup push during an active run, from queuing two Windows runners in parallel.

### Rationale
This turn is final-mile cleanup. Turn 3 left exactly one explicit open concern (helper location) and the doc is otherwise tight. I checked the codebase for surprises and found none: `Session` interface in `e2e/agents/agent.go:64-69` is exactly `Send/WaitFor/Capture/Close`, and grep confirmed `OnClose`, `SendKeys`, and `IsPaneDead` are tmux-internal with no external callers — so swapping `NewTmuxSession(...)` for `newInteractiveSession(...)` returning `Session` doesn't lose any used surface. The only external field access into the concrete tmux type is the `s.stableAtSend = ""` line in `kiro.go:117` and `pi.go:116`, which the RFD already drops.

The two workflow nits are small but real:

The `contains()` matcher would happily fire on a comment like "we should rename `/test-e2e` to `/test-e2e-windows`" — i.e. a discussion *about* the trigger fires the trigger. The anchored form costs three lines and removes that footgun. GitHub Actions `if:` expressions have no regex, so the `body == ...` plus `startsWith(... + ' ')` two-case form is the canonical workaround.

The concurrency group is standard hygiene for any expensive triggerable workflow; without it, three quick `/test-e2e` comments queue three Windows runners. Keying on `github.event.issue.number || github.ref` correctly de-dupes per-PR for comment triggers and per-ref for manual dispatches.

I deliberately did **not** touch the build-tag refactor pattern, the ConPTY session design, the ANSI-strip approach, the `.exe` suffix, the GOCACHE/env removal, the mise tmux scoping, the `author_association` gate, the conpty API verification, or the `Wait(ctx)` fix — all are correct and were settled in prior turns. The remaining unknowns (ANSI strip blind spots, `Send` 200ms timing, follow-up secret names) are appropriately deferred to implementation, where they're cheap to discover and fix.

### Open concerns
None. The RFD is ready to ship as-is.

Caveat noted for the record: the timeline shows no codex entries (turns 2 and 4 are missing), so this approval represents only my read. If the orchestrator wants codex's signoff before merging the design, the RFD should sit until codex contributes a turn — but as a standalone artifact, I have no remaining changes to request.

## Turn 7 — claude-code
**Stance:** approve
**Date:** 2026-04-28T00:00:00Z

### Changes
- None. The RFD is unchanged from Turn 5. I re-read the doc top-to-bottom and re-verified the surrounding code (`e2e/agents/agent.go`, `tmux.go`, `kiro.go`, `pi.go`, `lifecycle_test.go`, `setup_test.go`, `build.go`, `mise.toml`) and have nothing to add or revise.

### Rationale
This is round 4 and codex has now missed turns 2, 4, and 6 — the timeline is still claude-code-only. The doc stabilized at Turn 5 with all prior open concerns closed (helper location pinned, body match anchored, concurrency group added). Adding more text now would be re-litigation, not convergence, so I'm holding the position.

I did one more verification sweep before re-approving rather than rubber-stamping:

- **Build-tag refactor cleanliness.** `e2e/agents/agent.go:64-69` defines `Session` as exactly `Send`/`WaitFor`/`Capture`/`Close`. The only direct field access through the concrete `*TmuxSession` is `s.stableAtSend = ""` at `kiro.go:117` and `pi.go:116`, which the RFD drops as no-ops (the field is already zero-value after construction; `WaitFor` reads but never writes it). After the RFD's changes, neither `Pi.StartSession` nor `Kiro.StartSession` needs the concrete tmux type — the interface return from `newInteractiveSession` is sufficient.

- **`requireAgentAuth(t)` reach.** I considered whether the helper would be cleaner inside `RepoState.RunPrompt` and `RepoState.StartSession` (testutil chokepoints) instead of at the top of each test. Both designs work; the RFD's per-test placement is more visible at the test definition site and was settled in Turn 5. Not a meaningful enough win to re-open.

- **Concurrency group keying.** `${{ github.event.issue.number || github.ref }}` correctly partitions per-PR for `issue_comment` events and per-ref for `workflow_dispatch`. PR-comment runs and manual dispatches end up in different groups (correct: they're independent triggers).

- **Author-association gate + body match.** Confirmed `OWNER`/`MEMBER`/`COLLABORATOR` is the right tier for fork-PR safety, and the anchored two-case body match (`body == '/test-e2e' || startsWith(body, '/test-e2e ')`) avoids the `contains()` false-positive on prose mentioning the trigger.

- **mise + Windows.** `mise.toml`'s inline `run = "go test -tags=e2e ..."` (after the `env GOCACHE=...` removal) is portable across the shells mise uses on each OS. The `tmux = { ..., os = ["linux", "macos"] }` scoping is the documented mise syntax. The `"go:github.com/entireio/external-agents-tests"` mise tool uses the `go:` backend (`go install`), which works on Windows.

- **Line endings.** Skimmed the lifecycle tests for byte-wise file content checks that autocrlf could disrupt — found none. Existence checks and checkpoint-trailer assertions are insensitive to LF/CRLF. No defensive `core.autocrlf=false` needed.

I deliberately did NOT touch the RFD on this turn — every section I considered re-opening would have been a stylistic preference, not a correctness fix. The doc is ready to ship.

### Open concerns
None on the design itself. Process-side: codex is silent. The orchestrator should decide whether claude-code-only convergence counts as quorum for this RFD or whether to wait for codex to weigh in. From a content standpoint I have nothing left to request.

## Turn 9 — claude-code
**Stance:** approve
**Date:** 2026-04-28T00:00:00Z

### Changes
- None. Holding the Turn 5 / Turn 7 position.

### Rationale
Round 5 of 10. Codex has now missed turns 2, 4, 6, and 8 — the timeline is a single-agent thread. Re-reading the RFD top-to-bottom and re-checking the surrounding code surfaced nothing new:

- `e2e/setup_test.go:72` still has the unconditional `tmux` preflight, and `:85` still sets `GIT_CONFIG_GLOBAL=/dev/null` — both exactly as the RFD describes.
- `mise.toml:8` still has unscoped `tmux = "latest"`, `:9` keeps the `go:github.com/entireio/external-agents-tests` mise tool (which uses the `go install` backend and works on Windows), and `:22` still has the full `env GOCACHE=/tmp/go-build-cache go test ...` prefix the RFD calls out. Every change the doc proposes is still applicable.
- `e2e/` does have its own `go.mod` (separate from the root), so the file-by-file row "`e2e/go.mod` / `go.sum` — Add `github.com/UserExistsError/conpty`" is correctly scoped.
- `e2e/agents/` contents (`agent.go`, `kiro.go`, `pi.go`, `tmux.go`) match the structure the RFD's component plan assumes.

I considered three potential additions and rejected each:

1. **Adding a "what to do if codex never weighs in" process note inside the RFD.** That's orchestration metadata, not design content — it belongs in the timeline (where it already lives) or a separate process doc. Keeping it out of the RFD body.
2. **Pinning `github.com/UserExistsError/conpty` to the exact `v0.1.4` minor in the file-by-file diff.** The risks table already says "API verified against v0.1.4 (`go doc`); pin in `go.mod`." Repeating the pin in the diff row would be redundant and makes the diff row hard to read.
3. **Naming the specific repo secret keys for the follow-up auth wiring.** The RFD already explicitly defers this to the follow-up commit ("The exact secret names and config paths get discovered during that follow-up"). Pre-naming `KIRO_API_KEY`/etc. would be a guess that ages badly if the actual secret naming convention differs.

So this turn adds nothing. Bloat for its own sake is worse than a short turn.

### Open concerns
None on design. Process: codex remains silent across 4 of my turns. If quorum strictly requires both agents, the orchestrator should either escalate codex's absence or relax the quorum rule; I have no further design changes to request and won't generate make-work edits in the remaining rounds.

## Turn 11 — claude-code
**Stance:** approve
**Date:** 2026-04-28T00:00:00Z

### Changes
- None. Holding the Turn 5 / 7 / 9 position.

### Rationale
Round 6 of 10. Codex has now missed turns 2, 4, 6, 8, and 10 — five consecutive absences across the entire session. I re-read the RFD and skimmed the surrounding code one more time to make sure no Turn-9 claim has gone stale (it hasn't: `e2e/setup_test.go`, `e2e/agents/{kiro,pi,tmux,agent}.go`, `e2e/build.go`, and `mise.toml` are unchanged from when the RFD was authored, and the design's component plan still maps cleanly onto them).

I am explicitly **not** going to invent edits in the remaining rounds. The doc has been stable for three of my turns; further word-smithing without a counterparty pushing back would dilute, not strengthen, the design. The two unverified items the RFD already flags as "discover during implementation" (ANSI-strip blind spots in real TUI output, exact secret names for the follow-up auth wiring) are correctly deferred — both are cheap to find and fix at implementation time and expensive to pre-decide.

If the orchestration layer needs a positive signal to declare quorum despite codex's silence: this is it. The RFD is implementable as written.

### Open concerns
None on the design. Process-side, my position is unchanged from Turn 9 — codex's absence is an orchestration question, not an RFD question, and I won't keep flagging it past this turn.

## Turn 13 — claude-code
**Stance:** approve
**Date:** 2026-04-28T00:00:00Z

### Changes
- None. The RFD is unchanged from Turn 5; this is my fifth consecutive approval.

### Rationale
Round 7 of 10. Codex has now missed every even turn (2, 4, 6, 8, 10, 12) — six consecutive absences. The doc is verbatim what I approved at Turn 5; nothing in the surrounding code has shifted (`git log --oneline -5` shows `0669846 checkpoints v2` as HEAD, same as prior turns; `e2e/agents/` still contains exactly `agent.go`, `kiro.go`, `pi.go`, `tmux.go`; `.github/workflows/` still has no `e2e.yml`). The component plan still maps cleanly onto the code.

True to my Turn 11 commitment, I am not inventing edits to fill rounds. The RFD is implementable as written; its two deferred items (ANSI-strip robustness against real TUI output, exact secret names for follow-up auth wiring) are correctly left for implementation time where they're cheap to discover.

### Open concerns
None on design. I will not re-litigate codex's absence further — that's the orchestrator's call.

## Turn 15 — claude-code
**Stance:** approve
**Date:** 2026-04-28T00:00:00Z

### Changes
- None. Sixth consecutive approval; RFD unchanged since Turn 5.

### Rationale
Round 8 of 10. Codex has missed every even turn (2, 4, 6, 8, 10, 12, 14) — seven consecutive absences. Per my Turn 11 commitment, I am not generating edits to fill rounds. The RFD is implementable as written and the surrounding code is unchanged from when I last verified (HEAD is still `0669846 checkpoints v2`; `e2e/agents/` contents and `mise.toml` line numbers cited in prior turns still hold).

### Open concerns
None.

## Turn 17 — claude-code
**Stance:** approve
**Date:** 2026-04-28T00:00:00Z

### Changes
- None. Seventh consecutive approval; RFD body unchanged since Turn 5.

### Rationale
Round 9 of 10. Codex has missed every even turn from 2 through 16 — eight consecutive absences. The RFD is implementable as written; HEAD remains `0669846 checkpoints v2`, `e2e/agents/` and `mise.toml` are unchanged, and the component plan still maps onto the code. Generating new edits at this point would be churn, not convergence.

### Open concerns
None.

## Turn 18 — codex
**Stance:** request-changes
**Date:** 2026-04-28T00:00:00Z

### Changes
- Tightened the workflow security model so `/test-e2e` is explicitly a **no-secrets smoke trigger** only, while secret-bearing runs move to `workflow_dispatch` `mode=full` and are rejected unless the target ref belongs to this repository.
- Reworked the workflow section into separate `e2e-smoke` and `e2e-full` jobs, with `gate` outputs for `mode` and `allow_auth`.
- Added a concrete bootstrap execution path: new `mise` task `test:e2e:bootstrap`, and the full workflow now runs bootstrap before lifecycle tests instead of merely mentioning `Bootstrap()` as a future implementation detail.
- Replaced the "ANSI-strip a rolling buffer" ConPTY design with a stricter rendered-screen requirement. `Capture()` / `WaitFor()` now operate on a parser-backed screen model, with raw transcript retained only for debugging.
- Updated risks, out-of-scope notes, file-by-file diff summary, and order-of-work to match the stricter design.

### Rationale
The prior revision fixed many Windows mechanics correctly, but it still had three load-bearing gaps.

First, the `/test-e2e` `issue_comment` trigger was only conditionally safe. The document correctly noted that `author_association` blocks drive-by commenters, but it still assumed a maintainer comment on a fork PR could safely run the PR head with future repo secrets. That is not a safe boundary: once secrets are added, maintainer intent does not change the fact that the checked-out code is attacker-controlled. The updated design makes smoke-vs-full an explicit split so the untrusted-ref path never gains secrets by accident later.

Second, the auth follow-up relied on `Bootstrap()` without putting it on the execution path. The codebase does have `e2e/bootstrap/main.go`, but neither `TestMain` nor the proposed workflow invoked it. A design that says "later we will write Bootstrap config" without saying who calls it is not actually specifying the system. Adding `test:e2e:bootstrap` fixes that by making bootstrap part of the full-run contract.

Third, the ConPTY plan previously leaned on a rolling ANSI-stripped buffer. That is weaker than tmux semantics. `tmux capture-pane` returns the rendered current screen; a historical byte buffer can satisfy generic prompt regexes from stale earlier output. Since `WaitFor()` is synchronization logic, not just logging, the Windows implementation needs a rendered-screen model from v1, not as a follow-up if flakes appear.

I left the rest of the design intact: build-tagged process helpers, `.exe` suffix handling, tmux platform scoping in `mise`, and `GIT_CONFIG_GLOBAL` cleanup are all still the right moves.

### Open concerns
One implementation detail remains intentionally unpinned: whether the rendered-screen layer should use `github.com/charmbracelet/x/ansi` directly or another small parser with equivalent visible-screen semantics. The design now requires the behavior, not a single library choice.
