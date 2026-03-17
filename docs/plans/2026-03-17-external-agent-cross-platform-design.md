# External Agent Cross-Platform Support Design

## Goal

Add the same platform surfaces that `superpowers` uses for Codex, OpenCode, and Cursor while preserving the existing Claude standalone plugin workflow.

## Recommended Shape

Use the current Claude skill and command files as the canonical implementation for now, then add thin platform adapters around them:

- Codex: native skill discovery via `.codex/INSTALL.md`
- OpenCode: native skill discovery via `.opencode/INSTALL.md` plus a small bootstrap plugin
- Cursor: plugin manifest via `.cursor-plugin/plugin.json`
- Claude: keep the existing standalone plugin in `.claude/plugins/entire-external-agent/`

This avoids forking the workflow into multiple copies while keeping the repo small.

## File Strategy

- Keep `.claude/skills/entire-external-agent/` as the source of truth for the workflow text.
- Keep `.claude/plugins/entire-external-agent/commands/` as the source of truth for Claude command wrappers.
- Point Codex and OpenCode skill installation at `.claude/skills/`.
- Point Cursor's plugin manifest at the existing hidden skill and command directories.

## OpenCode Bootstrap

Unlike `superpowers`, this repo should not inject the full workflow into every session. The OpenCode plugin should only add a short system note that:

- the `entire-external-agent` skill is installed
- the skill should be used when the user wants to build or validate Entire CLI external agents
- tool names in the skill should be mapped to OpenCode equivalents

## Verification

Use a simple repository-level verification script to assert that:

- the Codex install document exists and points to the shared skill directory
- the OpenCode install document exists and registers both the plugin and the skill directory
- the OpenCode plugin exists and references the external-agent skill
- the Cursor plugin manifest exists and points to the existing skill and command directories
- the README documents the new cross-platform support
