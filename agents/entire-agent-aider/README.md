# entire-agent-aider (preview)

Standalone Go adapter for Aider's `.aider.chat.history.md` and Git attribution.
Requires Go 1.26 or later to build; Git is needed for commit analysis.

## Install and test

From this directory, run `go install ./cmd/entire-agent-aider` and put Go's binary
installation directory on PATH. Run `go test ./...` for the unit suite or
`go build ./cmd/entire-agent-aider` for a local binary (`.exe` on Windows).
For shared protocol validation run `external-agents-tests verify` with the
absolute path to the built binary. The runner is maintained separately.

Enable external-agent discovery in Entire's untracked
`.entire/settings.local.json` via `external_agents`; follow your Entire CLI's
setup instructions. This does **not** enable automatic Aider checkpoints.

## Capabilities and limitations

- Hooks: **false**. Hook commands safely do nothing. No native Aider lifecycle
  events or automatic commit-capture trigger are installed.
- Transcript analyzer and token calculator: **true**.
- Session ID: persisted random hex in `.entire/tmp/aider-session`, never HEAD.
  The ID is repository-scoped, not a unique identity for each Aider process.
  Archive history and remove this state file between logical sessions if needed.
  A stale `.lock` left by a killed process produces an error; remove it only
  after ensuring no adapter invocation is active.
- Resume: `aider --restore-chat-history`; Aider resumes the repository history,
  not an independently addressable historical session ID.
- Token reports support decimal k/m counts. `cost_usd` sums reported per-message
  costs, not the cumulative session cost. Counts/costs are estimates if rounded
  in the native log; missing reports contribute zero.
- Prompt extraction reads `#### ` headers outside fenced code. Aider Markdown
  cannot always distinguish assistant headings from user text.
- Offsets and positions are byte-based. `extract-summary` reports no summary.

## Commit-driven callers

Standard `extract-modified-files --path <history> --offset <bytes>` reads native
`> Commit <sha>` lines and checks author, committer and co-author attribution.
Additional `--commit <sha>` and `--range <base>..<tip>` options analyze Git
commits directly, returning the union of changed files in Aider-attributed
commits only. Revision inputs also work as `--path` when unambiguous. Direct
commit mode returns position zero because it has no transcript cursor.

Attribution recognizes `aider@aider.chat` and `(aider)` identities. Disabled
attribution cannot be inferred reliably. Missing/rebased-away commit references
return an error rather than silently attributing unrelated changes.

A caller must arrange capture around commits; `hooks: false` alone does not make
Entire's existing hook-driven lifecycle harness capture Aider automatically.
Live lifecycle verification requires Aider, Entire and model credentials and
was not available during implementation. See AGENT.md for the research mapping.

Parser fixtures are in `internal/aider/testdata/history.md`; they are realistic
synthetic fixtures, not claimed live captures. Tests use isolated temporary
repositories and work without Aider or API credentials.
