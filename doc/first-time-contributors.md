# First-Time Contributors

Welcome. This guide is for people who are new to Entire, new to open source,
or just want a careful path through their first contribution to this
repository.

If the [Entire CLI](https://github.com/entireio/cli) doesn't already work
with the AI coding agent you use, this repo is where you can add that
support. The [README](../README.md) goes into more detail on what that
involves. You can still pick up a small task here without understanding the
whole system first.

Good to know up front: this repo ships a built-in skill that walks an AI
coding tool (Claude Code, Codex, Cursor, or OpenCode) through scaffolding
a new agent. It gets you most of the way to a working integration. Adding
a brand-new agent still isn't a first-PR shape (the work tends to span
multiple pull requests), but it's useful to know the skill exists. See
[Adding a New External Agent](../CONTRIBUTING.md#adding-a-new-external-agent)
in CONTRIBUTING.md.

The main contribution rules live in [CONTRIBUTING.md](../CONTRIBUTING.md).
Treat that file as the source of truth. This page slows the process down and
explains where to start, what to say, what to run, and what to expect after
you open a pull request.

## Who This Is For

Start here if any of these are true:

- You have never opened a pull request before.
- You have contributed to other projects, but not to Entire.
- You want help finding a contribution that is small enough to finish.
- You are unsure how to ask maintainers for the right amount of context.

If you are already comfortable with this repo, use the shorter workflow in
[CONTRIBUTING.md](../CONTRIBUTING.md).

## Choose a First Contribution

Start by browsing
[GitHub Issues](https://github.com/entireio/external-agents/issues) for issues
that describe behavior you can reproduce, mention a command or workflow you
can try locally, or feel concrete enough that you can explain the problem
back in your own words. You can also check the `#looking-for-contributors`
channel in [Discord](https://discord.gg/jZJs3Tue4S). You do not need to
understand the whole project before asking about an issue.

Good first contributions are small, clear, and easy to review. For this repo,
that usually means one of these:

- Reproducing a bug in one of the existing agents under [`agents/`](../agents/)
  and adding the missing test.
- Clarifying a confusing message in an agent's CLI output.
- Adding a focused test around existing protocol or lifecycle behavior.
- Tightening a lifecycle assertion in `e2e/` that is currently lax.
- Improving README, agent README, or contributor instructions after you hit
  real friction.

When looking at an issue, favor ones where you can explain the problem back
in your own words. It is okay if you do not know the fix yet. If an issue is
broad, comment with the part you think you can tackle and ask whether that
slice would be useful.

If you do not already have a specific problem in mind, ask in
`#looking-for-contributors` and mention what you are comfortable with, such
as Go tests, CLI output, protocol JSON, lifecycle assertions, or README
wording. A maintainer can usually point you toward a safer slice than
browsing alone can.

**Not a first-PR shape:** adding support for a brand-new agent. That work is
long-running, involves protocol design choices, and almost always spans
multiple pull requests. If that is what you ultimately want to do, get
comfortable with the repo on something smaller first, then open an issue to
scope the new agent.

Also avoid, for a first PR: changes to the lifecycle harness's `TestMain` /
agent discovery, changes to the protocol-compliance workflow, and anything
that changes how checkpoints or hooks are installed, unless a maintainer
has already helped define the scope.

## Comment Before You Start

Before starting non-trivial code work, leave a short comment on the issue or
start a new issue. This helps avoid duplicated effort and gives maintainers
a chance to redirect you before you spend time on the wrong approach.

A good first comment is specific:

```text
Hi, I am new to Entire external agents and would like to work on this.

I was able to reproduce the issue by running:

  E2E_AGENT=kiro mise run test:e2e

My plan is to <describe the change> in
agents/entire-agent-kiro/<file or area> and add a focused test for the
behavior. Does that sound like the right direction?
```

If you are not starting from an existing issue, open one with the smallest
concrete problem you can describe:

```text
I hit confusing behavior while running `entire-agent-kiro <subcommand>` on
macOS.

Expected: <what you thought would happen>
Actual: <what actually happened, with the JSON output if relevant>

I can reproduce this locally. Would a small PR that fixes the message and
adds a focused unit test be useful?
```

You do not need a perfect plan. Showing what you have tried and where you are
headed is usually enough.

## Set Up the Repository

Fork the repository on GitHub, then clone your fork:

```bash
git clone git@github.com:YOUR-USERNAME/external-agents.git
cd external-agents
```

Add the upstream repository so you can pull in maintainer changes later:

```bash
git remote add upstream git@github.com:entireio/external-agents.git
git fetch upstream
```

Install the local toolchain:

```bash
mise trust
mise install
mise run build
```

`mise run build` builds every agent binary into `./bin`. Verify the setup
with the unit test suite:

```bash
mise run test
```

If you plan to touch the lifecycle harness, also install the relevant
agent's own CLI so the harness can drive it. Each agent's README under
[`agents/`](../agents/) lists its requirements.

If setup fails, include the command you ran, the full error text, your OS,
and the output of `go version` and `mise --version` when asking for help.

## Create a Branch

Create a branch from the latest `main`:

```bash
git switch main
git pull upstream main
git switch -c fix/short-description
```

Use a branch name that describes the change. Examples:

- `fix/kiro-clearer-missing-config-error`
- `test/amp-transcript-token-edge-case`
- `docs/lifecycle-setup-troubleshooting`

## Make a Small Change

Keep your first pull request narrow. A focused PR is easier to review and
easier to finish.

Good first PR shapes for this repo:

- One failing behavior in a single agent, plus its test.
- One small CLI message improvement in a single agent.
- One test-only clarification or assertion tightening.
- One README or setup clarification based on a real problem you hit.

Avoid bundling unrelated cleanup with your first PR. If you notice extra
things while working, leave yourself a note and open a follow-up issue or PR
later.

If you find yourself touching more than one agent, or both the agent and the
shared lifecycle harness, pause and ask whether the change should be split.

## Use AI Carefully

This repository is, in part, about coding-agent integration, so it is
completely normal to use a coding agent while contributing here. There is no
need to tell us that you used AI.

Use whatever agent and workflow you like. The important rule is that you are
responsible for the final code, tests, and docs. Before submitting a PR,
review the diff yourself. PRs that look generated, unreviewed, or "vibe
coded" without a human pass may be closed.

Quick responsible-AI tips:

- **Think first.** Agents tend to jump straight to code. Ask the agent to
  explore the codebase first, explain the relevant architecture (especially
  the [external agent protocol spec](https://github.com/entireio/cli/blob/main/docs/architecture/external-agent-protocol.md)),
  and propose an approach before it edits files.
- **Push back on shortcuts.** Watch for trivial tests, overly wide types,
  optional fields added just to satisfy the compiler, swallowed errors, and
  patterns copied from one agent into another that do not actually fit.
- **Notice uncertainty.** If the agent keeps declaring it understands the
  issue but the patches are not converging, stop and reframe. Use what you
  learned to start a smaller attempt.
- **Cut the bloat.** Remove redundant comments, just-in-case logging,
  over-defensive branches, and tests that only check implementation details
  instead of user-visible or protocol-visible behavior.

## Run the Right Checks

For wording-only changes, run a lightweight sanity check:

```bash
git diff --check
```

For Go code changes in a single agent, run:

```bash
mise run fmt
mise run test
```

For changes that touch the shared lifecycle harness or protocol surface, also
run:

```bash
mise run test:e2e
```

You can narrow lifecycle runs while iterating:

```bash
E2E_AGENT=kiro mise run test:e2e
```

Protocol compliance runs in CI against every built binary, so you do not
normally need to run it locally. If you want to mirror it, build first
(`mise run build`) and then run `external-agents-tests --binary-path
./bin/entire-agent-<name>` against the binary you care about.

Do not assume tests passing locally is enough on its own. The GitHub Actions
workflow runs the protocol compliance suite against every agent on every PR,
so watch the PR checks after you push.

## Commit Your Work

Commit with a short, descriptive message. When your change touches a single
agent, prefix the subject with the agent name:

```bash
git add <files you changed>
git commit -m "kiro: clarify error when config is missing"
```

Cross-cutting changes (the lifecycle harness, shared docs, CI) can omit the
prefix.

Entire is used to dogfood Entire. If you have the Entire CLI installed and
enabled in this repository, the git hook may add an `Entire-Checkpoint`
trailer to your commit message automatically. That is expected. See
[Using Entire While Contributing](../CONTRIBUTING.md#using-entire-while-contributing)
for the full details.

## Open a Pull Request

Push your branch to your fork:

```bash
git push -u origin fix/short-description
```

Then open a pull request against `entireio/external-agents` on GitHub.

In the PR description, include:

- The issue it addresses, if there is one.
- Which agent(s) are affected (e.g. `entire-agent-kiro`) or "shared
  lifecycle harness".
- A short summary of what changed.
- The checks you ran, such as `mise run test`, `mise run test:e2e`, or
  `git diff --check`.
- Anything you want reviewers to pay special attention to.

If your PR is not ready for final review yet, open it as a draft. Draft PRs
are useful when you want early feedback on direction.

## Respond to Review

Review is part of the contribution, not a sign that you did something wrong.
PRs here may receive automated Copilot comments and maintainer comments.

For each review comment:

- If it is right, push a follow-up commit that addresses it.
- If you are unsure, ask a question on the PR.
- If you disagree, explain your reasoning briefly and respectfully.

After pushing updates, leave a short comment saying what changed. GitHub does
not always make it obvious which comments were addressed by a new commit.

## If You Get Stuck

Ask early, and include context. Good help requests include:

- What you are trying to do.
- The command you ran (and `E2E_AGENT=...` if relevant).
- The exact error or output, including the raw JSON line if a protocol check
  failed.
- What you already checked.
- Your OS, Go version, and `mise --version` when setup or tests are
  involved.

Useful places to ask:

- The GitHub issue you are working on.
- The pull request, if one is already open.
- [Discord](https://discord.gg/jZJs3Tue4S) for general questions.

## Quick Checklist

Before opening your first PR:

- You picked a small, focused change in a single area.
- You asked for maintainer scope before non-trivial code work.
- You created a branch from current `main`.
- You ran the checks that match your change (`mise run test`, and
  `mise run test:e2e` if lifecycle-relevant).
- Your PR description names the affected agent(s) and explains what changed
  and how you tested it.

Thank you for taking the time to contribute. Small, careful improvements
make the project easier for the next person too.
