# entire-agent-qwen

External agent binary that teaches the Entire CLI how to work with Qwen Code.

## Capabilities

| Capability | Status |
|------------|--------|
| hooks | Yes, command hooks in `.qwen/settings.json` for Qwen Code lifecycle, tool, compact, notification, permission, and subagent events |
| transcript_analyzer | Yes, via Entire sidecar JSONL |
| compact_transcript | Yes, emits Entire compact transcript JSONL |
| transcript_preparer | No |
| token_calculator | No |

## Installation

```bash
cd agents/entire-agent-qwen
mise run build
cp entire-agent-qwen /usr/local/bin/
```

Qwen Code itself must also be installed as `qwen` for real sessions:

```bash
npm install -g @qwen-code/qwen-code
```

## Enable In A Repo

```bash
cd /path/to/repo
entire enable --agent qwen --telemetry=false
```

This writes command hooks to `.qwen/settings.json`. Hooks call:

```bash
entire hooks qwen <hook-name>
```

The hook commands are wrapped so a missing `entire` binary never breaks a Qwen session. Existing user hook fields such as `env`, `statusMessage`, HTTP hook `url`, `headers`, `allowedEnvVars`, and unknown future Qwen fields are preserved when Entire installs or uninstalls its entries.

## Approve The Hooks In Qwen Code

Since Qwen Code 0.26.7, Qwen skips the hooks in a project's `.qwen/settings.json` until you allow them. Until you do, no Entire hook runs, so Entire records nothing for the session: no prompts, no turns, no checkpoint. Qwen only prints a warning that starts with `Project hooks in .qwen/settings.json were not loaded`. Entire does not approve or trust the hooks for you, because Qwen leaves that decision to you.

There are two ways to allow them:

- **Approve them** (Qwen Code 0.26.8 and later). After `entire enable`, start `qwen` interactively in the repo and approve the project hooks in the dialog Qwen shows. Qwen saves the decision in `~/.qwen/hookApprovals.json` against a hash of the hooks block and the project's environment (its `.env` files and the `env` setting), so it asks again whenever those change, for example when `entire enable --force` or an upgrade rewrites Entire's hook commands. If you reject them, Qwen keeps them off and does not ask again until they change; delete the repo's entry from that file to be asked again.
- **Trust the folder.** Turn on folder trust in `~/.qwen/settings.json`:

  ```json
  {"security": {"folderTrust": {"enabled": true}}}
  ```

  Then trust the repo with `/trust` in an interactive session, or add it to `~/.qwen/trustedFolders.json`:

  ```json
  {"/path/to/repo": "TRUST_FOLDER"}
  ```

  This is the only way on Qwen Code 0.26.7. Once folder trust is on, Qwen never runs project hooks in a folder you have not trusted, even approved ones.

A headless run (`qwen -p`, with or without `--yolo`) never shows the dialog. It runs the hooks only if you approved them in an interactive session in that repo and they have not changed since, or if the folder is trusted. For CI and other runs where nobody can answer the dialog, trust the folder.

These files live under `$QWEN_HOME` instead of `~/.qwen` when that variable is set.

## Usage

```bash
qwen   # first time only, unless the folder is trusted: approve the project hooks, then /quit
qwen -p "Create hello.txt with hello world" --yolo
git add .
git commit -m "qwen checkpoint test"
entire checkpoint list
```

Entire stores Qwen sidecar transcripts in a repo-scoped OS temp directory such as `/tmp/entire-qwen/<repo-hash>/<session_id>.jsonl`, preserving Qwen's native `transcript_path` as metadata. A small `.entire/tmp/<session_id>.json` marker is also written so Entire's shared session persistence tooling can discover the session without making the transcript path collide with other external agents.

The adapter records Qwen's supported hook payload shapes, including `tool_input`, `inputs`, `input`, `tool_response`, `response`, and subagent/permission metadata. Local models must still make Qwen Code execute real tools; if a model only prints XML-style tool tags as text, Qwen Code will not fire `PostToolUse` and there is no file change for Entire to checkpoint.

## Development

```bash
mise run build
mise run test
external-agents-tests verify ./entire-agent-qwen

cd ../../
QWEN_E2E=1 E2E_AGENT=qwen mise run test:e2e:lifecycle
```

The lifecycle tests run `qwen -p --yolo` in new temporary repos (`$TMPDIR/e2e-repo-*`), where no dialog can approve the hooks. With Qwen Code 0.26.7 or later, turn on folder trust, point `TMPDIR` at a directory of its own, and trust that directory with `TRUST_FOLDER`. The rule covers the repos the tests create inside it.
