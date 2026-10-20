# Aider external agent research


## Verdict: PARTIAL (preview)

Aider has no native lifecycle hook system. This adapter exposes transcript and
Git attribution analysis, not automatic Entire checkpoint triggering. A caller
must initiate commit-driven capture; no Git hooks are installed or replaced.

## Sources and verification

- Protocol: https://github.com/entireio/cli/blob/main/docs/architecture/external-agent-protocol.md
- Native history writer: https://github.com/Aider-AI/aider/blob/main/aider/io.py
- User-supplied contract: Markdown history, token reports, Git attribution and
  `aider --restore-chat-history`.
- Aider, Entire and external-agents-tests were not found on PATH during research.
  Live content, authentication and lifecycle behavior are UNVERIFIED. No native
  hooks exist to probe; no hook-capture script or hook-based e2e adapter is added.

## Mapping

- `info`: aider / Aider, preview; hooks=false, transcript_analyzer=true,
  token_calculator=true; other optional capabilities=false.
- `detect`: aider executable or repository `.aider.chat.history.md`.
- `get-session-id`: persisted random 16-byte hex ID in `.entire/tmp/aider-session`.
  Explicitly addresses @ashtom's feedback: never derive identity from HEAD.
- `get-session-dir`, `resolve-session-file`: repo root and Markdown history.
- `read-session`, `write-session`, `read-transcript`: native history bytes.
- `chunk-transcript`, `reassemble-transcript`: raw-byte chunks serialized as base64.
- `format-resume-command`: `aider --restore-chat-history`.
- Analyzer offsets: byte offsets; position is native transcript byte size.
- `extract-prompts`: `#### ` user headers outside fenced code blocks.
- `extract-summary`: no dedicated summary available; reports has_summary=false.
- `calculate-tokens`: sent/received token reports; k/m suffixes; per-call costs
  exposed as optional `cost_usd` estimate (not a standard protocol field).
- `extract-modified-files`: native `> Commit <sha>` records scoped by transcript
  offset, verified against Git attribution before collecting changed paths.
  Also accepts a commit/revision range as `--path`, or explicit `--commit` /
  `--range` for callers performing commit-driven capture.
- Attribution: author/committer email or name and Co-authored-by trailers;
  arbitrary message prose is not attribution.
- Hook commands: safe no-ops, false installation status, null parse result.

## Identity limitations

The ID remains stable until the state file is removed. Aider provides no launch
hook to distinguish concurrent chats or reset identity automatically. To start a
new logical session, archive the history as needed and remove the state file
while no adapter invocation is active. Git commits, rebases and squash merges
never change identity. One default history file is shared per repository;
custom Aider history-file settings are supported via explicit session references.

## Validation and lifecycle prerequisites

Build with `go build ./cmd/entire-agent-aider`; run `go test ./...`.
Shared compliance: `external-agents-tests verify <absolute-binary-path>`.
Live testing requires Aider, Entire, Git and model credentials. Aider's
noninteractive entry point is `aider --yes-always --message <prompt>` (unverified
locally). Existing hook-driven lifecycle scenarios cannot prove automatic
capture for hooks=false; caller-side commit capture integration is still needed.
