# Validate Procedure

Run conformance tests against a built external agent binary to verify it correctly implements the external agent protocol.

## Prerequisites

Ensure the following are available:
- `AGENT_SLUG` — binary suffix (binary name is `entire-agent-<AGENT_SLUG>`)
- `PROJECT_DIR` — project directory (binary should be at `<PROJECT_DIR>/entire-agent-<AGENT_SLUG>` or buildable via `make -C <PROJECT_DIR> build`)
- The binary is built and accessible

## Phase 1: Setup

### Build the binary

```bash
cd <PROJECT_DIR> && make build
```

### Create a test environment

```bash
VALIDATE_DIR=$(mktemp -d)
cd "$VALIDATE_DIR"
git init
git config user.name "Test"
git config user.email "test@test.com"
echo "test" > README.md
git add . && git commit -m "init"
```

### Set environment variables

```bash
export ENTIRE_REPO_ROOT="$VALIDATE_DIR"
export ENTIRE_PROTOCOL_VERSION=1
```

### Define the binary path

```bash
BINARY="<PROJECT_DIR>/entire-agent-<AGENT_SLUG>"
```

## Phase 2: Required Subcommand Tests

Run each test and record PASS/FAIL. Every required subcommand must pass.

### `info`

```bash
OUTPUT=$($BINARY info)
```

Verify:
- Valid JSON (parseable)
- `protocol_version` equals `1`
- `name` is a non-empty string
- `type` is a non-empty string
- `capabilities` object exists with boolean fields for: `hooks`, `transcript_analyzer`, `transcript_preparer`, `token_calculator`, `text_generator`, `hook_response_writer`, `subagent_aware_extractor`

### `detect`

```bash
OUTPUT=$($BINARY detect)
```

Verify:
- Valid JSON
- `present` field is a boolean

### `get-session-id`

```bash
OUTPUT=$(echo '{"hook_type":"stop","session_id":"test-session-001","session_ref":"/tmp/transcript.jsonl","timestamp":"2026-01-01T00:00:00Z"}' | $BINARY get-session-id)
```

Verify:
- Valid JSON
- `session_id` is a non-empty string

### `get-session-dir`

```bash
OUTPUT=$($BINARY get-session-dir --repo-path "$VALIDATE_DIR")
```

Verify:
- Valid JSON
- `session_dir` is a non-empty string

### `resolve-session-file`

```bash
SESSION_DIR=$($BINARY get-session-dir --repo-path "$VALIDATE_DIR" | python3 -c "import json,sys; print(json.load(sys.stdin)['session_dir'])")
OUTPUT=$($BINARY resolve-session-file --session-dir "$SESSION_DIR" --session-id "test-session-001")
```

Verify:
- Valid JSON
- `session_file` is a non-empty string

### `read-session`

```bash
OUTPUT=$(echo '{"hook_type":"session_start","session_id":"test-session-001","session_ref":"/tmp/transcript.jsonl","timestamp":"2026-01-01T00:00:00Z"}' | $BINARY read-session)
```

Verify:
- Valid JSON
- Has fields: `session_id`, `agent_name`, `repo_path`, `session_ref`, `start_time`
- `modified_files`, `new_files`, `deleted_files` are arrays (may be empty or null)

### `write-session`

```bash
echo '{"session_id":"test-session-001","agent_name":"test","repo_path":"/tmp","session_ref":"/tmp/t.jsonl","start_time":"2026-01-01T00:00:00Z","native_data":null,"modified_files":[],"new_files":[],"deleted_files":[]}' | $BINARY write-session
```

Verify:
- Exit code is 0

### `read-transcript`

Create a test transcript file:
```bash
echo '{"role":"user","content":"hello"}' > "$VALIDATE_DIR/test-transcript.jsonl"
OUTPUT=$($BINARY read-transcript --session-ref "$VALIDATE_DIR/test-transcript.jsonl")
```

Verify:
- Output is non-empty
- Output contains the transcript content

### `chunk-transcript` + `reassemble-transcript` (roundtrip)

```bash
ORIGINAL="Hello, this is a test transcript with some content for chunking."
CHUNKS=$(echo -n "$ORIGINAL" | $BINARY chunk-transcript --max-size 20)
REASSEMBLED=$(echo "$CHUNKS" | $BINARY reassemble-transcript)
```

Verify:
- `CHUNKS` is valid JSON with a `chunks` array
- `REASSEMBLED` is byte-identical to `ORIGINAL`

### `format-resume-command`

```bash
OUTPUT=$($BINARY format-resume-command --session-id "test-session-001")
```

Verify:
- Valid JSON
- `command` is a non-empty string

## Phase 3: Capability-Gated Tests

Read the `info` output to determine which capabilities are declared. Only run tests for declared capabilities.

### Capability: `hooks`

#### `parse-hook`

For each hook name declared in `info.hook_names`:
```bash
OUTPUT=$(echo '{"native":"payload"}' | $BINARY parse-hook --hook "<hook-name>")
```

Verify:
- Valid JSON or `null`
- If non-null: has `type` (integer 1-7) and `session_id` (non-empty string)
- Unknown hook names return `null`

#### `install-hooks`

```bash
OUTPUT=$($BINARY install-hooks)
```

Verify:
- Valid JSON
- `hooks_installed` is an integer >= 0

#### `are-hooks-installed`

```bash
OUTPUT=$($BINARY are-hooks-installed)
```

Verify:
- Valid JSON
- `installed` is a boolean

#### `uninstall-hooks`

```bash
$BINARY uninstall-hooks
```

Verify:
- Exit code is 0

#### Integration smoke test

```bash
# Install → verify installed → parse a hook → uninstall → verify not installed
$BINARY install-hooks
INSTALLED=$($BINARY are-hooks-installed | python3 -c "import json,sys; print(json.load(sys.stdin)['installed'])")
# (parse-hook test as above)
$BINARY uninstall-hooks
NOT_INSTALLED=$($BINARY are-hooks-installed | python3 -c "import json,sys; print(json.load(sys.stdin)['installed'])")
```

Verify:
- After install: `installed` is `True`
- After uninstall: `installed` is `False`

### Capability: `transcript_analyzer`

#### `get-transcript-position`

```bash
OUTPUT=$($BINARY get-transcript-position --path "$VALIDATE_DIR/test-transcript.jsonl")
```

Verify:
- Valid JSON
- `position` is a non-negative integer

#### `extract-modified-files`

```bash
OUTPUT=$($BINARY extract-modified-files --path "$VALIDATE_DIR/test-transcript.jsonl" --offset 0)
```

Verify:
- Valid JSON
- `files` is an array of strings
- `current_position` is a non-negative integer

#### `extract-prompts`

```bash
OUTPUT=$($BINARY extract-prompts --session-ref "$VALIDATE_DIR/test-transcript.jsonl" --offset 0)
```

Verify:
- Valid JSON
- `prompts` is an array of strings

#### `extract-summary`

```bash
OUTPUT=$($BINARY extract-summary --session-ref "$VALIDATE_DIR/test-transcript.jsonl")
```

Verify:
- Valid JSON
- `has_summary` is a boolean
- If `has_summary` is true, `summary` is a non-empty string

### Other Capabilities

For each remaining declared capability, test the corresponding subcommands following the same pattern:
- Invoke with valid inputs
- Verify JSON schema matches the protocol spec
- Verify exit code is 0

## Phase 4: Error Path Tests

### Missing required arguments

For subcommands that require flags, invoke without them:

```bash
$BINARY get-session-dir  # missing --repo-path
$BINARY resolve-session-file  # missing --session-dir and --session-id
$BINARY chunk-transcript  # missing --max-size
```

Verify:
- Non-zero exit code for each
- Error message on stderr

### Invalid JSON stdin

```bash
echo 'not json' | $BINARY get-session-id
echo '{broken' | $BINARY read-session
```

Verify:
- Non-zero exit code
- Error message on stderr

### Unknown subcommand

```bash
$BINARY totally-bogus-subcommand
```

Verify:
- Non-zero exit code
- Error message on stderr

## Phase 5: Conformance Report

Generate a conformance report table:

```
## Conformance Report: entire-agent-<AGENT_SLUG>

### Required Subcommands
| Subcommand | Result | Notes |
|-----------|--------|-------|
| info | PASS/FAIL | ... |
| detect | PASS/FAIL | ... |
| get-session-id | PASS/FAIL | ... |
| get-session-dir | PASS/FAIL | ... |
| resolve-session-file | PASS/FAIL | ... |
| read-session | PASS/FAIL | ... |
| write-session | PASS/FAIL | ... |
| read-transcript | PASS/FAIL | ... |
| chunk-transcript + reassemble-transcript | PASS/FAIL | ... |
| format-resume-command | PASS/FAIL | ... |

### Capability: hooks
| Subcommand | Result | Notes |
|-----------|--------|-------|
| parse-hook | PASS/FAIL/SKIP | ... |
| install-hooks | PASS/FAIL/SKIP | ... |
| uninstall-hooks | PASS/FAIL/SKIP | ... |
| are-hooks-installed | PASS/FAIL/SKIP | ... |
| Integration smoke | PASS/FAIL/SKIP | ... |

### Capability: transcript_analyzer
| Subcommand | Result | Notes |
|-----------|--------|-------|
| get-transcript-position | PASS/FAIL/SKIP | ... |
| extract-modified-files | PASS/FAIL/SKIP | ... |
| extract-prompts | PASS/FAIL/SKIP | ... |
| extract-summary | PASS/FAIL/SKIP | ... |

### Error Paths
| Test | Result | Notes |
|------|--------|-------|
| Missing required args | PASS/FAIL | ... |
| Invalid JSON stdin | PASS/FAIL | ... |
| Unknown subcommand | PASS/FAIL | ... |

### Overall Verdict: PASS / FAIL
- Required: X/10 passed
- Capability-gated: X/Y passed (Z skipped)
- Error paths: X/3 passed
```

## Phase 6: Fix and Re-validate

If any tests fail:
1. Identify the root cause from the test output
2. Fix the implementation in the appropriate source file
3. Rebuild: `cd <PROJECT_DIR> && make build`
4. Re-run only the failing tests
5. Repeat until all tests pass

## Phase 7: Cleanup and Commit

```bash
rm -rf "$VALIDATE_DIR"
```

If any fixes were made during validation, create a git commit for them.

## Constraints

- **Test in isolation.** Always use a fresh temp directory — never test against the user's real repo.
- **Clean up.** Remove the temp directory after testing.
- **Report everything.** Even SKIP results should be noted with a reason.
- **Don't modify the binary.** This phase only tests — fixes go back to the implement phase or are committed separately.
