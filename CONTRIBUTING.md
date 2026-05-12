# Contributing to Entire External Agents

Thank you for your interest in contributing! If the
[Entire CLI](https://github.com/entireio/cli) doesn't already integrate
with the AI coding agent you use, this is the repository where you can add
that support. Each integration lives in its own directory under
[`agents/`](agents/) as a small standalone Go program that lets Entire hook
into a specific coding tool to capture checkpoints, transcripts, and
lifecycle events.

**You don't have to build it from scratch.** This repo ships a built-in
skill that walks an AI coding tool (Claude Code, Codex, Cursor, or
OpenCode) through researching the target agent, scaffolding tests, and
implementing the binary. It gets you most of the way to a working
integration. See [Adding a New External Agent](#adding-a-new-external-agent)
below for how to invoke it.

Please read the [Code of Conduct](https://github.com/entireio/cli/blob/main/CODE_OF_CONDUCT.md)
before participating.

> **New to Entire?** See the [First-Time Contributors guide](doc/first-time-contributors.md)
> for a step-by-step first contribution walkthrough, or the [README](README.md)
> for setup and architecture documentation.

---

## Before You Code: Discuss First

The fastest way to get a contribution merged is to align with maintainers before
writing code. Please **open an issue first** describing what you want to change,
and wait for maintainer feedback before starting implementation. This is
especially true if you want to add support for a brand-new agent. That work
is a long-running commitment the maintainers will need to support.

### Contribution Workflow

1. **Open an issue** describing the problem, feature, or new agent
2. **Wait for maintainer feedback**: we may have relevant context or plans
3. **Get approval** before starting implementation
4. **Submit your PR** referencing the approved issue
5. **Address all feedback** including automated Copilot comments
6. **Maintainer review and merge**

---

## First-Time Contributors

New to the project? Welcome! Start with the
[First-Time Contributors guide](doc/first-time-contributors.md). It explains
how to find a comfortable issue, ask for a scoped starter task, set up the
repo, use AI responsibly, run the right checks, and open your first pull
request.

### Starter Contributions

We recommend starting with:

- **A protocol compliance gap**: An existing agent fails a check against
  [`entireio/external-agents-tests`](https://github.com/entireio/external-agents-tests)
  on your machine; add a focused fix.
- **A lifecycle test failure you can reproduce**: The `e2e/` harness flagged
  something; capture it as a narrower test and a fix.
- **Confusing output or help text** in an existing agent's CLI.
- **Documentation friction**: Improve this repo's README, agent READMEs, or
  contributor docs after you hit a real problem.
- **An agent unit test** that covers existing behavior without changing it.

Adding support for a brand-new agent is welcome but is not a first-PR
shape. Open an issue first and expect the work to span multiple PRs.

Browse [GitHub Issues](https://github.com/entireio/external-agents/issues), or
ask in the `#looking-for-contributors` channel on the
[Entire Discord](https://discord.gg/jZJs3Tue4S) if you want help finding a
starter-sized slice.

Using a coding agent while contributing is welcome (this repo is literally
about coding-agent integration), but you are responsible for the final diff.
Review AI-generated changes yourself before opening a PR.

---

## Submitting Issues

All feature requests, bug reports, and general issues should be submitted
through [GitHub Issues](https://github.com/entireio/external-agents/issues).
Please search for existing issues before opening a new one.

For security-related issues, see the Security section below.

---

## Security

If you discover a security vulnerability, **do not report it through GitHub
Issues**. Instead, follow the disclosure process described in the
[Security Policy](https://github.com/entireio/cli/blob/main/SECURITY.md).
All security reports are kept confidential.

---

## Communication

Contributions and communications are expected to occur through:

- [GitHub Issues](https://github.com/entireio/external-agents/issues): Bug
  reports and feature requests
- [Discord](https://discord.gg/jZJs3Tue4S): Questions, general conversation,
  and real-time support

Please represent the project and community respectfully in all public and
private interactions.

---

## How to Contribute

There are many ways to contribute:

- **Fix or extend an existing agent** under [`agents/`](agents/)
- **Improve the lifecycle harness**: The shared `e2e/` tests that exercise
  every agent
- **Add a new external agent**: Discuss in an issue first; the
  [external agent builder skill](.claude/skills/entire-external-agent/) walks
  you through the full pipeline
- **Documentation**: Improve READMEs, agent docs, contributor instructions
- **Community**: Help others on Discord, answer questions, share knowledge

---

## Reporting Bugs

Good bug reports help us fix issues quickly. When reporting a bug, please
include:

### Required Information

1. **Affected agent**: which directory under `agents/`, or "the `e2e/`
   harness"
2. **Entire CLI version**: run `entire version`
3. **Operating system**
4. **Go version**: run `go version`
5. **mise version**: run `mise --version`

### What to Include

1. **What did you do?**: Include the exact commands you ran
2. **What did you expect to happen?**
3. **What actually happened?**: Include the full error message or unexpected
   output (especially the JSON returned by the agent if a protocol check
   failed)
4. **Can you reproduce it?**: Does it happen every time or intermittently?
5. **Any additional context?**: Logs, lifecycle artifacts from
   `e2e/artifacts/`, related issues

---

## Local Setup

### Prerequisites

- **Go 1.26.x**: mise will install the pinned version
- **mise**: Task runner and version manager. Install with
  `curl https://mise.run | sh`
- **The target agent's own CLI** if you plan to run lifecycle tests against
  it. Each agent's README under [`agents/`](agents/) lists what it expects.

### Clone and Install

```bash
# Clone the repository
git clone https://github.com/entireio/external-agents.git
cd external-agents

# Trust the mise configuration (required on first setup)
mise trust

# Install pinned tools (Go, golangci-lint, license_finder, ...)
mise install

# Build every agent binary into ./bin
mise run build

# Verify setup by running unit tests across all agents
mise run test
```

See the [README](README.md) for the wider repository layout and the testing
split between protocol compliance, lifecycle integration, and unit tests.

---

## Making Changes

1. **Create a branch** for your changes:

   ```bash
   git checkout -b fix/short-description
   ```

2. **Make your changes**: follow the [Code Style](#code-style) guidelines
   and keep the change narrow.

3. **Test your changes**: see [Testing](#testing).

4. **Commit** with clear, descriptive messages:

   ```bash
   git commit -m "kiro: clarify missing-config error"
   ```

   When a commit touches a single agent, prefix the subject with the agent
   name (e.g. `kiro:`). Cross-cutting changes (the lifecycle harness, shared
   docs, CI) can omit the prefix.

---

## Code Style

Follow standard Go idioms and conventions.

### Key Points

- **Error handling**: Handle all errors explicitly. Don't leave them
  unchecked.
- **Formatting**: Code must pass `gofmt` (run `mise run fmt`).
- **Linting**: Code must pass `golangci-lint`. The config lives at
  [.golangci.yaml](.golangci.yaml).
- **Naming**: Use meaningful, descriptive names following Go conventions.
- **Protocol surface**: When changing a subcommand's JSON output, keep it
  compatible with the [external agent protocol spec](https://github.com/entireio/cli/blob/main/docs/architecture/external-agent-protocol.md).
  Add or update protocol-compliance coverage in
  [`entireio/external-agents-tests`](https://github.com/entireio/external-agents-tests)
  in the same PR cycle.

---

## Testing

Testing is intentionally split into three layers (see [README](README.md) for
the full picture):

| Layer | Where it lives | When to run |
|-------|----------------|-------------|
| Protocol compliance | `entireio/external-agents-tests` (run in CI via [protocol-compliance.yml](.github/workflows/protocol-compliance.yml)) | When you change subcommand JSON, exit codes, or protocol surface |
| Lifecycle integration | `e2e/` in this repo | When you change anything that interacts with `entire enable`, hooks, checkpoints, or rewind |
| Unit | `agents/entire-agent-*/` | Always, before pushing |

```bash
# Unit tests across all agents (default suite)
mise run test

# Lifecycle integration tests against every registered agent
mise run test:e2e

# Same as test:e2e, kept as the explicit name
mise run test:e2e:lifecycle

# Limit lifecycle runs to one agent while iterating
E2E_AGENT=kiro mise run test:e2e
```

CI also runs the protocol compliance suite against every built binary. You do
not normally need to run that locally, but you can mirror it with:

```bash
# After mise run build, run the shared compliance suite against one binary
external-agents-tests --binary-path ./bin/entire-agent-kiro
```

The lifecycle harness calls the real agent CLIs, so the corresponding tool
must be on your PATH. See each agent's README for setup hints, and use
`E2E_KEEP_REPOS=1` if you need to inspect a failed run's temp repo.

---

## Adding a New External Agent

This is a substantial change. Please open an issue first and expect to split
the work across multiple PRs. When you are ready to start, the repo ships a
skill that walks through the full pipeline:

| Command | Skill file | Description |
|---------|-----------|-------------|
| Full pipeline | [`.claude/skills/entire-external-agent/SKILL.md`](.claude/skills/entire-external-agent/SKILL.md) | Run all three phases sequentially |
| Research | [`.claude/skills/entire-external-agent/research.md`](.claude/skills/entire-external-agent/research.md) | Analyze the target agent and map to the protocol |
| Write tests | [`.claude/skills/entire-external-agent/write-tests.md`](.claude/skills/entire-external-agent/write-tests.md) | Scaffold the binary, wire protocol + lifecycle coverage |
| Implement | [`.claude/skills/entire-external-agent/implement.md`](.claude/skills/entire-external-agent/implement.md) | Build the binary using protocol compliance first, lifecycle second, unit tests last |

The skill auto-discovers per AI tool:

| Tool | How it discovers | What to say |
|------|------------------|-------------|
| Claude Code | `.claude/skills/` | `/entire-external-agent` |
| Codex | `AGENTS.md` | "Build an external agent" |
| Cursor | `.cursor/rules/` | "Build an external agent" |
| OpenCode | `.opencode/plugins/` | "Build an external agent" |

For a newly added agent to be picked up automatically by local tasks and CI,
make sure your `agents/entire-agent-<name>/mise.toml` defines `build` and
`test` tasks. The shared runner falls back to Go defaults when only `go.mod`
exists, but the `mise` tasks are the contract for anything non-standard.

---

## Submitting a Pull Request

### Before You Submit

- **Related issue exists and is approved**: Your PR references an issue
  where a maintainer has acknowledged the approach. (Exceptions: typo
  corrections, wording-only updates, and maintainer-scoped starter tasks.)
- **Formatting passes**: `mise run fmt` leaves the tree clean.
- **Linting passes**: `golangci-lint` clean.
- **Tests pass**: `mise run test` for unit tests; `mise run test:e2e` if
  your change is lifecycle-relevant.
- **Tests included**: New Go code and protocol behavior should ship with
  accompanying tests at the right layer.
- **Entire checkpoint trailers included**: See
  [Using Entire While Contributing](#using-entire-while-contributing).

PRs that skip these steps are likely to be closed without merge.

### Submitting

1. **Push** your branch to your fork.
2. **Open a PR** against the `main` branch.
3. **Describe your changes**: Link the related issue, name which agent(s)
   are affected, summarize what changed and what testing you did.
4. **Address Copilot feedback**: See [Responding to Automated Review](#responding-to-automated-review).
5. **Wait for maintainer review.**

---

## Responding to Automated Review

A Copilot reviewer comments on every PR with feedback on code quality,
potential bugs, and project conventions.

**Read and respond to every Copilot comment.** PRs with unaddressed Copilot
feedback will not move to maintainer review.

- **Fixed**: Push a commit addressing the issue.
- **Disagree**: Reply explaining your reasoning. Copilot isn't always right.
- **Question**: Ask for clarification.

Addressing Copilot feedback upfront is the fastest path to maintainer review.

---

## Using Entire While Contributing

We use Entire on Entire. When contributing to this repo, install the Entire
CLI and let it capture your coding sessions. This gives us valuable
dogfooding data and helps improve the tool.

### Setup

Install the latest version of the Entire CLI (see
[installation docs](https://docs.entire.io/cli/installation)) and verify with
`entire version`. Entire is already configured in this repository, so there is
no need to run `entire enable`.

### Checkpoint Trailers

All commits should include `Entire-Checkpoint` trailers from your sessions.
These are added automatically by the `prepare-commit-msg` hook when Entire is
enabled. The trailers link your commits to session metadata on the
`entire/checkpoints/v1` branch.

### Sessions Branch

When you push your PR branch, Entire can automatically push the
`entire/checkpoints/v1` branch alongside it (if `push_sessions` is enabled in
your settings). Including it lets maintainers see the session context behind
your changes.

---

## Troubleshooting

### Common Setup Issues

**`mise install` fails**

```bash
# Ensure mise is properly installed
curl https://mise.run | sh

# Reload your shell
source ~/.zshrc  # or ~/.bashrc
```

**`go mod download` fails with timeout**

```bash
# Try using direct mode
GOPROXY=direct go mod download
```

**Lifecycle test cannot find the agent CLI**

The lifecycle harness invokes the real agent CLI. Make sure the tool is
installed and on your PATH, or set the corresponding override described in
the agent's README.

**Lifecycle test passes locally but fails in CI (or vice versa)**

Set `E2E_KEEP_REPOS=1` and re-run with `E2E_AGENT=<name>` to capture the temp
repo for inspection. Lifecycle artifacts are written under `e2e/artifacts/`;
attach the relevant ones to the issue or PR.

---

## Community

Join the Entire community:

- **Discord**: [Join our server][discord] for discussions and support

[discord]: https://discord.gg/jZJs3Tue4S

---

## Additional Resources

- [README](README.md): Setup, architecture, and the testing split
- [AGENTS.md](AGENTS.md): Agent-builder skill entry point (Codex/Cursor/OpenCode)
- [External agent protocol spec](https://github.com/entireio/cli/blob/main/docs/architecture/external-agent-protocol.md): The contract every agent binary implements
- [Code of Conduct](https://github.com/entireio/cli/blob/main/CODE_OF_CONDUCT.md): Community guidelines
- [Security Policy](https://github.com/entireio/cli/blob/main/SECURITY.md): Reporting security vulnerabilities

---

Thank you for contributing!
