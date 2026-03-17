# Scaffold Procedure

Generate a complete project skeleton for an external agent binary with compilable/runnable stubs.

## Prerequisites

Ensure the following are available:
- `AGENT_NAME`, `AGENT_SLUG`, `LANGUAGE`, `PROJECT_DIR` — from orchestrator or user
- `<PROJECT_DIR>/AGENT.md` — research one-pager (from research phase)

## Phase 1: Read Source Material at Runtime

**Do not use static templates.** Read the following files at runtime to generate code that matches the current protocol version:

**If inside the Entire CLI repo:**
1. Read `docs/architecture/external-agent-protocol.md` — subcommand specs, JSON schemas, capabilities
2. Read `cmd/entire/cli/agent/external/types.go` — JSON response struct definitions
3. Read `cmd/entire/cli/agent/external/external.go` — how the CLI calls each subcommand (args, stdin format, expected stdout)
4. Optionally read `cmd/entire/cli/agent/external/capabilities.go` — how capabilities gate subcommand invocation

**If standalone:**
Read the protocol spec from the location provided in the research phase.

**Always read:**
5. Read `<PROJECT_DIR>/AGENT.md` — agent-specific decisions (capabilities, hook format, transcript location)

## Phase 2: Generate Project Structure

### For Go (recommended)

```
<PROJECT_DIR>/
  go.mod                    # Module: github.com/<user>/entire-agent-<slug>
  main.go                   # Subcommand dispatch switch
  cmd/
    info.go                 # Required: info subcommand
    detect.go               # Required: detect subcommand
    session.go              # Required: get-session-id, get-session-dir, resolve-session-file, read-session, write-session
    transcript.go           # Required: read-transcript, chunk-transcript, reassemble-transcript
    resume.go               # Required: format-resume-command
    hooks.go                # Capability: hooks (parse-hook, install-hooks, uninstall-hooks, are-hooks-installed)
    analyzer.go             # Capability: transcript_analyzer (get-transcript-position, extract-modified-files, extract-prompts, extract-summary)
    preparer.go             # Capability: transcript_preparer (prepare-transcript)
    tokens.go               # Capability: token_calculator (calculate-tokens)
    generator.go            # Capability: text_generator (generate-text)
    hook_response.go        # Capability: hook_response_writer (write-hook-response)
    subagent.go             # Capability: subagent_aware_extractor (extract-all-modified-files, calculate-total-tokens)
  internal/
    types.go                # Response types translated from external/types.go
    protocol.go             # Env var helpers, constants (ENTIRE_REPO_ROOT, ENTIRE_PROTOCOL_VERSION)
  AGENT.md                  # Research one-pager (already exists)
  README.md                 # Usage, installation, development
  Makefile                  # build, install, test, validate
```

**Only create capability files for capabilities declared in AGENT.md.** Skip files for capabilities marked `false`.

### For Python

```
<PROJECT_DIR>/
  pyproject.toml
  entire_agent_<slug>/
    __init__.py
    __main__.py             # Subcommand dispatch (argparse)
    cmd/
      __init__.py
      info.py               # Required subcommands
      detect.py
      session.py
      transcript.py
      resume.py
      hooks.py              # If hooks capability
      analyzer.py           # If transcript_analyzer capability
      [other capabilities]
    types.py                # Response dataclasses
    protocol.py             # Env var helpers
  AGENT.md
  README.md
  Makefile
```

### For TypeScript

```
<PROJECT_DIR>/
  package.json
  tsconfig.json
  src/
    index.ts                # Subcommand dispatch
    cmd/
      info.ts
      detect.ts
      session.ts
      transcript.ts
      resume.ts
      hooks.ts              # If hooks capability
      analyzer.ts           # If transcript_analyzer capability
      [other capabilities]
    types.ts                # Response interfaces
    protocol.ts             # Env var helpers
  AGENT.md
  README.md
  Makefile
```

### For Rust

```
<PROJECT_DIR>/
  Cargo.toml
  src/
    main.rs                 # Subcommand dispatch (clap)
    cmd/
      mod.rs
      info.rs
      detect.rs
      session.rs
      transcript.rs
      resume.rs
      hooks.rs              # If hooks capability
      analyzer.rs           # If transcript_analyzer capability
      [other capabilities]
    types.rs                # Response structs (serde)
    protocol.rs             # Env var helpers
  AGENT.md
  README.md
  Makefile
```

## Phase 3: Implement Stubs

Each subcommand handler should:

1. **Parse arguments** — flags from `os.Args` or the language's arg parser
2. **Read stdin if required** — for subcommands that accept JSON or raw bytes on stdin
3. **Return valid JSON** — matching the exact schema from `types.go` / the protocol spec
4. **Use placeholder values** — realistic but clearly fake (e.g., `session_id: "stub-session-000"`)

### main.go (Go example)

The main dispatch should:
- Parse `os.Args[1]` as the subcommand name
- Dispatch to the appropriate handler function
- Print usage to stderr and exit 1 for unknown subcommands
- Handle `--help` and `--version` flags

```go
func main() {
    if len(os.Args) < 2 {
        fmt.Fprintln(os.Stderr, "Usage: entire-agent-<slug> <subcommand> [args]")
        os.Exit(1)
    }
    switch os.Args[1] {
    case "info":
        cmd.Info()
    case "detect":
        cmd.Detect()
    // ... all required + declared capability subcommands
    default:
        fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", os.Args[1])
        os.Exit(1)
    }
}
```

### info stub

The `info` subcommand must return valid JSON immediately — this is what the CLI calls during discovery. Use the capabilities from `AGENT.md`:

```json
{
  "protocol_version": 1,
  "name": "<AGENT_SLUG>",
  "type": "<AGENT_NAME>",
  "description": "<AGENT_NAME> - External agent plugin for Entire CLI",
  "is_preview": true,
  "protected_dirs": [],
  "hook_names": [],
  "capabilities": {
    "hooks": <from AGENT.md>,
    "transcript_analyzer": <from AGENT.md>,
    ...
  }
}
```

### internal/types.go

Translate the response types from `cmd/entire/cli/agent/external/types.go` into the target language. Include JSON tags/annotations that match the protocol exactly.

### internal/protocol.go

Helper functions for:
- `RepoRoot()` — reads `ENTIRE_REPO_ROOT` env var
- `ProtocolVersion()` — reads `ENTIRE_PROTOCOL_VERSION` env var
- `ParseArgs(args []string, flags ...string)` — simple flag parser (or use the language's standard lib)

### Makefile

```makefile
BINARY := entire-agent-<AGENT_SLUG>

.PHONY: build install test validate clean

build:
	<language-specific build command>

install: build
	cp $(BINARY) $(GOPATH)/bin/ || cp $(BINARY) /usr/local/bin/

test:
	<language-specific test command>

validate: build
	@echo "Running conformance validation..."
	./$(BINARY) info | python3 -c "import json,sys; d=json.load(sys.stdin); assert d['protocol_version']==1; print('info: PASS')"

clean:
	rm -f $(BINARY)
```

### README.md

Include:
- What the binary does
- How to build and install
- How to test
- Link to the external agent protocol spec
- Capabilities declared

## Phase 4: Verify Compilation

Build the project and verify:

1. **Compiles/builds without errors**
2. **`info` returns valid JSON:** `./entire-agent-<slug> info | python3 -c "import json,sys; print(json.dumps(json.load(sys.stdin), indent=2))"`
3. **Unknown subcommand exits non-zero:** `./entire-agent-<slug> bogus; echo "exit: $?"`

Fix any issues before proceeding.

## Phase 5: Commit

Use `/commit` to commit the entire scaffolded project.

## Constraints

- **Runtime source reading.** Generate code by reading the protocol spec and types at runtime — never embed static templates that can drift from the real protocol.
- **Only declared capabilities.** Don't create files for capabilities the agent doesn't support.
- **Valid stubs.** Every stub must return valid JSON matching the protocol schema. The binary must be usable for basic testing immediately after scaffolding.
- **Language conventions.** Follow idiomatic conventions for the chosen language (Go modules, Python packages, TypeScript with strict mode, Rust with serde).
