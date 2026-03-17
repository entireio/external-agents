# External Agent Cross-Platform Support Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add Codex, OpenCode, and Cursor support around the existing external-agent skill and document how each platform installs and discovers it.

**Architecture:** Keep the current Claude skill and command files as the source of truth. Add thin adapter files for each additional platform, and verify the expected repository surface with a lightweight shell script.

**Tech Stack:** Markdown, JSON, JavaScript, shell

---

### Task 1: Define the expected repository surface

**Files:**
- Create: `tests/verify-cross-platform-support.sh`

**Step 1: Write the failing test**

Create a shell verification script that checks for:
- `.codex/INSTALL.md`
- `.opencode/INSTALL.md`
- `.opencode/plugins/entire-external-agent.js`
- `.cursor-plugin/plugin.json`
- updated `README.md` references

**Step 2: Run test to verify it fails**

Run: `bash tests/verify-cross-platform-support.sh`

Expected: FAIL because the new adapter files do not exist yet.

**Step 3: Write minimal implementation**

Add the missing platform adapter files and README updates.

**Step 4: Run test to verify it passes**

Run: `bash tests/verify-cross-platform-support.sh`

Expected: PASS with a short success message.

### Task 2: Add Codex support

**Files:**
- Create: `.codex/INSTALL.md`

**Step 1: Document installation**

Write install steps that:
- clone the repo under `~/.codex/external-agents`
- symlink `~/.agents/skills/external-agents` to `~/.codex/external-agents/.claude/skills`
- restart Codex

**Step 2: Verify link target is explicit**

Ensure the document makes clear that the shared skill lives under `.claude/skills/`.

### Task 3: Add OpenCode support

**Files:**
- Create: `.opencode/INSTALL.md`
- Create: `.opencode/plugins/entire-external-agent.js`

**Step 1: Document installation**

Write install steps that:
- clone the repo under `~/.config/opencode/external-agents`
- symlink the plugin into `~/.config/opencode/plugins/`
- symlink the shared skill directory into `~/.config/opencode/skills/external-agents`

**Step 2: Add minimal bootstrap plugin**

Implement a small system-prompt transform that announces the installed skill and maps tool references to OpenCode equivalents without auto-injecting the whole workflow.

### Task 4: Add Cursor support

**Files:**
- Create: `.cursor-plugin/plugin.json`

**Step 1: Add plugin manifest**

Point Cursor at:
- `./.claude/skills/`
- `./.claude/plugins/entire-external-agent/commands/`

Keep metadata concise and aligned with the repo purpose.

### Task 5: Update repo documentation

**Files:**
- Modify: `README.md`

**Step 1: Document supported platforms**

Add installation guidance for:
- Claude
- Codex
- OpenCode
- Cursor

**Step 2: Update layout**

Document the new adapter directories so the repo structure is clear.

### Task 6: Verify syntax and repository surface

**Files:**
- Test: `tests/verify-cross-platform-support.sh`

**Step 1: Run repository verification**

Run: `bash tests/verify-cross-platform-support.sh`

Expected: PASS

**Step 2: Check JSON and JavaScript syntax**

Run:
- `python3 -m json.tool .cursor-plugin/plugin.json >/dev/null`
- `node --check .opencode/plugins/entire-external-agent.js`

Expected: both commands succeed.
