# Aider feasibility spike

## Verdict: PARTIAL

Aider is integrable as an external-agent adapter, but it does not expose the lifecycle-hook mechanism used by the existing adapters. A first implementation should therefore use Aider's durable git commits and markdown chat history rather than hook callbacks.

## Static Checks

| Check | Result | Notes |
| --- | --- | --- |
| Binary present | TBD | Run `aider --version` in the target environment. |
| Help available | TBD | Run `aider --help`. |
| Lifecycle hooks | NOT FOUND | The feasibility investigation found no Aider-native lifecycle hook or callback API. |
| Automatic commits | PASS | Aider supports automatic git commits and attribution options, including an `(aider)` marker when configured. |
| Chat history | PASS | Aider can write markdown chat history to `.aider.chat.history.md`; an optional LLM history file is also supported. |
| Session identifier | TBD | Aider has no native stable session identifier; a persisted launch-time repository HEAD SHA is a candidate for v1. |
| Token data | PASS | Each turn in `.aider.chat.history.md` includes `> Tokens: … Cost: …`, so token and cost extraction is feasible from the transcript. |

## Proposed v1 Boundary

Use each Aider-attributed git commit as a checkpoint. Aider's default attribution can be detected through the stable `aider@aider.chat` commit-trailer identity. For each checkpoint, collect the commit's modified files with `git show --name-only` and attach the relevant slice of `.aider.chat.history.md` as the transcript. The transcript is markdown, with each prompt represented by a `#### <prompt>` block and turn-level token/cost lines. This is durable and works with ordinary Aider invocation, but its granularity is per commit rather than per turn.

A wrapper or launcher can be considered later if per-turn lifecycle boundaries are required. That approach would only capture sessions launched through the wrapper and should not be the first implementation unless maintainers prefer it.

## Capability Declaration

| Capability | Declared | Rationale |
| --- | --- | --- |
| hooks | false | No native lifecycle-hook mechanism was found. |
| transcript_analyzer | true | Markdown chat history and git diffs are parseable inputs. |
| token_calculator | true | Turn-level token and cost data is persisted in the markdown history. |
| transcript_preparer | TBD | Only needed if the markdown history requires normalization. |
| text_generator | false | Aider is the external coding agent, not a summary service. |
| hook_response_writer | false | There are no native hooks through which to write responses. |
| subagent_aware_extractor | false | No native subagent transcript model was identified. |

## Open Design Questions

Maintainers should confirm whether a `hooks: false` adapter is acceptable, whether commit-driven capture is preferred to a wrapper, and how `get-session-id` should synthesize a stable identifier. The verification run confirms markdown transcript parsing, `aider@aider.chat` commit attribution, git-based modified-file extraction, and persisted token/cost data. The initial PR should remain a research spike containing this document and a safe verifier before binary adapter code is added.

## E2E Test Prerequisites

The eventual end-to-end test will require the Entire CLI, the Aider CLI, a disposable git repository, and a non-interactive Aider invocation using `--message` or `--message-file` with an appropriate confirmation option. The test must not run against a user's working repository.
