# E2E Test Reorganization — Per-Agent Subpackages

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reorganize e2e tests into per-agent subpackages (`e2e/kiro/`, `e2e/cursor/`, etc.) so each agent's binary contract tests live in their own directory with their own `TestMain`.

**Architecture:** Extract shared binary-building logic from `setup_test.go` into an exported `build.go` helper. Move kiro-specific tests and fixtures into `e2e/kiro/` as a separate Go test package with its own `TestMain` that builds only the kiro binary. Keep cross-agent lifecycle tests in the root `e2e/` package since they use `ForEachAgent` to iterate all registered agents.

**Tech Stack:** Go 1.26, `//go:build e2e` tags, `testify`

---

## File Structure

### Current
```
e2e/
  setup_test.go              # TestMain — builds ALL agent binaries, populates agentBinaries map
  harness.go                 # AgentRunner, CommandResult (shared)
  testenv.go                 # TestEnv, NewTestEnv, NewKiroTestEnv, NewKiroGitEnv
  fixtures.go                # HookInput, ParseHookInput (shared) + KiroTranscript (kiro-specific)
  kiro_test.go               # 30 kiro binary contract tests
  kiro_lifecycle_test.go     # 7 lifecycle tests via ForEachAgent (agent-agnostic)
  agents/
    agent.go                 # Agent interface, registry
    kiro.go                  # Kiro agent impl (filtered by E2E_AGENT)
    tmux.go                  # tmux session helper
  testutil/                  # Shared assertions, artifacts, metadata types
  entire/                    # entire CLI helpers (enable, rewind, etc.)
```

### Target
```
e2e/
  build.go                   # NEW — exported BuildAgent(), RepoRoot()
  setup_test.go              # MODIFIED — uses BuildAgent(), stores binaries in exported var
  harness.go                 # UNCHANGED
  testenv.go                 # MODIFIED — add NewTestEnvWithBinary(), remove NewKiroTestEnv/NewKiroGitEnv
  fixtures.go                # MODIFIED — remove KiroTranscript (moved to kiro/)
  lifecycle_test.go          # RENAMED from kiro_lifecycle_test.go (content unchanged)
  kiro/
    setup_test.go            # NEW — TestMain builds only kiro binary
    kiro_test.go             # MOVED from e2e/kiro_test.go, adapted imports
    fixtures_test.go         # NEW — KiroTranscript + helpers (from e2e/fixtures.go)
    testenv_test.go          # NEW — NewKiroTestEnv, NewKiroGitEnv (from e2e/testenv.go)
  agents/                    # UNCHANGED
  testutil/                  # UNCHANGED
  entire/                    # UNCHANGED
```

### Key Design Decisions

1. **All kiro subpackage files are `_test.go` files with `package kiro`** — this is required because `kiroBinary` is defined in `setup_test.go`, and non-test `.go` files cannot reference variables from `_test.go` files. Using `_test.go` ensures all files compile as part of the same test binary.
2. **All files in `e2e/kiro/` use `//go:build e2e`** — prevents accidental compilation without `-tags=e2e`.
3. **Lifecycle tests stay in root `e2e/`** — they use `ForEachAgent` which iterates all registered agents. They aren't kiro-specific despite the current filename.
4. **`TestLifecycle_HooksInstalledAfterEnable`** uses `agentBinaries` from the root package — this stays in root since it needs the shared binary map.
5. **`NewTestEnvWithBinary(t, binPath)`** is the bridge — subpackages pass their locally-built binary path, root package continues using the map lookup.
6. **`go test -tags=e2e ./e2e/...`** runs everything (root + all subpackages). `go test -tags=e2e ./e2e/kiro/` runs only kiro binary tests.

---

### Task 1: Extract `BuildAgent` and `RepoRoot` into `e2e/build.go`

Extract the agent-building logic and repo root resolution from `setup_test.go` into an importable (non-`_test.go`) file so subpackages can reuse them.

**Files:**
- Create: `e2e/build.go`
- Modify: `e2e/setup_test.go`

- [ ] **Step 1: Create `e2e/build.go` with exported helpers**

```go
//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// RepoRoot returns the absolute path to the repository root.
// Uses runtime.Caller to locate the source file at compile time, which is
// reliable regardless of the working directory at runtime (go test runs
// from a temp dir, not the source dir).
func RepoRoot() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ".."
	}
	// file = /absolute/path/to/e2e/build.go → up one level to repo root
	return filepath.Dir(filepath.Dir(file))
}

// BuildAgent compiles a single agent binary from agents/<agentName>/cmd/<agentName>/
// into the given output directory. Returns the absolute path to the built binary.
func BuildAgent(agentName, outputDir string) (string, error) {
	agentDir := filepath.Join(RepoRoot(), "agents", agentName)
	mainPkg := "./cmd/" + agentName
	binPath := filepath.Join(outputDir, agentName)

	cmd := exec.Command("go", "build", "-o", binPath, mainPkg)
	cmd.Dir = agentDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("build %s: %w", agentName, err)
	}
	return binPath, nil
}

// DiscoverAgents returns relative paths (e.g. "agents/entire-agent-kiro") for
// all agent directories that have a cmd/<name>/main.go.
func DiscoverAgents() ([]string, error) {
	agentsDir := filepath.Join(RepoRoot(), "agents")
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return nil, fmt.Errorf("read agents dir: %w", err)
	}

	var agentDirs []string
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "entire-agent-") {
			continue
		}
		mainFile := filepath.Join(agentsDir, entry.Name(), "cmd", entry.Name(), "main.go")
		if _, err := os.Stat(mainFile); err != nil {
			continue
		}
		agentDirs = append(agentDirs, filepath.Join("agents", entry.Name()))
	}
	return agentDirs, nil
}
```

- [ ] **Step 2: Update `e2e/setup_test.go` to use the exported helpers**

Replace the inline `discoverAgents()` and `repoRoot()` functions with calls to `DiscoverAgents()` and `RepoRoot()`. Replace the inline build logic with `BuildAgent()`. Export `AgentBinaries` so `lifecycle_test.go` can access it (it's used by `TestLifecycle_HooksInstalledAfterEnable`).

In `setup_test.go`, make these changes:

1. Replace `var agentBinaries = map[string]string{}` with `var AgentBinaries = map[string]string{}`
2. In `TestMain`, replace the build loop:
   ```go
   for _, agentDir := range discoveredAgents {
       agentName := filepath.Base(agentDir)
       fmt.Printf("Building %s...\n", agentName)
       binPath, err := BuildAgent(agentName, tmpDir)
       if err != nil {
           fmt.Fprintf(os.Stderr, "failed to build %s: %v\n", agentName, err)
           os.Exit(1)
       }
       AgentBinaries[agentName] = binPath
       fmt.Printf("Built %s -> %s\n", agentName, binPath)
   }
   ```
3. Replace `discoveredAgents, err := discoverAgents()` with `discoveredAgents, err := DiscoverAgents()`
4. Delete the `discoverAgents()` and `repoRoot()` functions

- [ ] **Step 3: Update all references from `agentBinaries` to `AgentBinaries`**

In `e2e/testenv.go`, update `NewTestEnv` to use `AgentBinaries` instead of `agentBinaries`. Same for `agentBinaryNames()`.

In `e2e/kiro_lifecycle_test.go` (soon to be `lifecycle_test.go`), update the reference in `TestLifecycle_HooksInstalledAfterEnable`:
```go
binPath, ok := AgentBinaries[agentBinName]
```

- [ ] **Step 4: Verify build**

Run: `cd /Users/alisha/Projects/external-agents/e2e && go build -tags=e2e ./...`
Expected: compiles with no errors

- [ ] **Step 5: Verify tests still pass**

Run: `cd /Users/alisha/Projects/external-agents && make test-e2e`
Expected: all existing tests pass (no behavior change)

- [ ] **Step 6: Commit**

```bash
git add e2e/build.go e2e/setup_test.go e2e/testenv.go e2e/kiro_lifecycle_test.go
git commit -m "refactor(e2e): extract BuildAgent and RepoRoot into importable build.go"
```

---

### Task 2: Add `NewTestEnvWithBinary` to `e2e/testenv.go`

Add a constructor that accepts an explicit binary path, so subpackages can create `TestEnv` without needing the `AgentBinaries` map.

**Files:**
- Modify: `e2e/testenv.go`

- [ ] **Step 1: Add `NewTestEnvWithBinary` function**

Add this function to `e2e/testenv.go` after the existing `NewTestEnv`:

```go
// NewTestEnvWithBinary creates a test environment using an explicit binary path.
// Use this from subpackages that build their own agent binary in TestMain.
func NewTestEnvWithBinary(t *testing.T, binPath string) *TestEnv {
	t.Helper()

	dir := t.TempDir()
	homeDir := t.TempDir()

	env := baseEnv(dir, homeDir)

	return &TestEnv{
		t:       t,
		Dir:     dir,
		HomeDir: homeDir,
		Runner: &AgentRunner{
			BinaryPath: binPath,
			Env:        env,
		},
	}
}
```

- [ ] **Step 2: Verify build**

Run: `cd /Users/alisha/Projects/external-agents/e2e && go build -tags=e2e ./...`
Expected: compiles with no errors

- [ ] **Step 3: Commit**

```bash
git add e2e/testenv.go
git commit -m "feat(e2e): add NewTestEnvWithBinary for subpackage use"
```

---

### Task 3: Create `e2e/kiro/` subpackage — setup and fixtures

Create the kiro subpackage with its own `TestMain`, test environment helpers, and kiro-specific fixtures.

**Files:**
- Create: `e2e/kiro/setup_test.go`
- Create: `e2e/kiro/testenv_test.go`
- Create: `e2e/kiro/fixtures_test.go`

- [ ] **Step 1: Create `e2e/kiro/setup_test.go`**

```go
//go:build e2e

package kiro

import (
	"fmt"
	"os"
	"testing"

	e2e "github.com/entireio/external-agents/e2e"
)

// kiroBinary holds the path to the built entire-agent-kiro binary.
var kiroBinary string

func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "e2e-kiro-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmpDir)

	fmt.Println("Building entire-agent-kiro...")
	binPath, err := e2e.BuildAgent("entire-agent-kiro", tmpDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to build entire-agent-kiro: %v\n", err)
		os.Exit(1)
	}
	kiroBinary = binPath
	fmt.Printf("Built entire-agent-kiro -> %s\n", binPath)

	// Isolate git config to prevent user's ~/.gitconfig from interfering.
	os.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")

	os.Exit(m.Run())
}
```

- [ ] **Step 2: Create `e2e/kiro/testenv_test.go`**

Move `NewKiroTestEnv` and `NewKiroGitEnv` from `e2e/testenv.go` here, adapted to use `kiroBinary` and `NewTestEnvWithBinary`:

```go
//go:build e2e

package kiro

import (
	"testing"

	e2e "github.com/entireio/external-agents/e2e"
)

// NewKiroTestEnv creates a test environment with .kiro/ and .entire/tmp/ directories.
func NewKiroTestEnv(t *testing.T) *e2e.TestEnv {
	t.Helper()
	te := e2e.NewTestEnvWithBinary(t, kiroBinary)
	te.MkdirAll(".kiro")
	te.MkdirAll(".entire/tmp")
	return te
}

// NewKiroGitEnv creates a Kiro test environment with git init.
func NewKiroGitEnv(t *testing.T) *e2e.TestEnv {
	t.Helper()
	te := NewKiroTestEnv(t)
	te.GitInit()
	return te
}
```

- [ ] **Step 3: Create `e2e/kiro/fixtures_test.go`**

Move `KiroTranscript` and related types/functions from `e2e/fixtures.go`:

```go
//go:build e2e

package kiro

import (
	"encoding/json"
	"testing"

	e2e "github.com/entireio/external-agents/e2e"
)

// KiroTranscript builds Kiro-format transcript files for testing.
type KiroTranscript struct {
	ConversationID string             `json:"conversation_id"`
	History        []kiroHistoryEntry `json:"history"`
}

type kiroHistoryEntry struct {
	User      kiroUserMessage `json:"user"`
	Assistant json.RawMessage `json:"assistant"`
}

type kiroUserMessage struct {
	Content   json.RawMessage `json:"content"`
	Timestamp string          `json:"timestamp,omitempty"`
}

// NewKiroTranscript creates a new transcript builder.
func NewKiroTranscript(id string) *KiroTranscript {
	return &KiroTranscript{ConversationID: id}
}

func marshalPromptContent(prompt string) json.RawMessage {
	content, _ := json.Marshal(map[string]interface{}{
		"Prompt": map[string]string{"prompt": prompt},
	})
	return content
}

// AddPrompt adds a user prompt entry with no assistant response.
func (kt *KiroTranscript) AddPrompt(prompt string) *KiroTranscript {
	kt.History = append(kt.History, kiroHistoryEntry{
		User: kiroUserMessage{Content: marshalPromptContent(prompt)},
	})
	return kt
}

// AddPromptWithFileEdit adds a user prompt paired with an assistant response that contains a file edit tool use.
func (kt *KiroTranscript) AddPromptWithFileEdit(prompt, filePath string) *KiroTranscript {
	toolUse := map[string]interface{}{
		"ToolUse": map[string]interface{}{
			"message_id": "msg-001",
			"tool_uses": []map[string]interface{}{
				{
					"id":   "tool-001",
					"name": "fs_write",
					"args": map[string]string{"path": filePath},
				},
			},
		},
	}
	assistantContent, _ := json.Marshal(toolUse)

	kt.History = append(kt.History, kiroHistoryEntry{
		User:      kiroUserMessage{Content: marshalPromptContent(prompt)},
		Assistant: assistantContent,
	})
	return kt
}

// AddResponse adds a user prompt paired with an assistant text response.
func (kt *KiroTranscript) AddResponse(prompt, response string) *KiroTranscript {
	userContent := marshalPromptContent(prompt)

	responseContent := map[string]interface{}{
		"Response": map[string]interface{}{
			"message_id": "msg-resp",
			"content":    response,
		},
	}
	assistantContent, _ := json.Marshal(responseContent)

	kt.History = append(kt.History, kiroHistoryEntry{
		User:      kiroUserMessage{Content: userContent},
		Assistant: assistantContent,
	})
	return kt
}

// JSON returns the JSON-encoded transcript string.
func (kt *KiroTranscript) JSON(t *testing.T) string {
	t.Helper()
	data, err := json.Marshal(kt)
	if err != nil {
		t.Fatalf("marshal KiroTranscript: %v", err)
	}
	return string(data)
}

// WriteToFile writes the transcript to a file and returns the absolute path.
func (kt *KiroTranscript) WriteToFile(t *testing.T, env *e2e.TestEnv, relPath string) string {
	t.Helper()
	env.WriteFile(relPath, kt.JSON(t))
	return env.AbsPath(relPath)
}
```

- [ ] **Step 4: Verify — skip compilation check**

All files in `e2e/kiro/` are `_test.go` files, so `go build` has nothing to compile. The package will be verified when tests run in Task 4.

- [ ] **Step 5: Commit**

```bash
git add e2e/kiro/setup_test.go e2e/kiro/testenv_test.go e2e/kiro/fixtures_test.go
git commit -m "feat(e2e): create kiro subpackage with setup, testenv, and fixtures"
```

---

### Task 4: Move kiro binary tests to `e2e/kiro/kiro_test.go`

Move `e2e/kiro_test.go` into the new subpackage. Update package declaration and imports.

**Files:**
- Create: `e2e/kiro/kiro_test.go` (moved from `e2e/kiro_test.go`)
- Delete: `e2e/kiro_test.go`

- [ ] **Step 1: Copy `e2e/kiro_test.go` to `e2e/kiro/kiro_test.go` and update**

Changes needed in the moved file:
1. Change `package e2e` → `package kiro`
2. Add import: `e2e "github.com/entireio/external-agents/e2e"`
3. Replace `NewKiroTestEnv(t)` → `NewKiroTestEnv(t)` (same name, now local to kiro package)
4. Replace `NewTestEnv(t, "entire-agent-kiro")` → `e2e.NewTestEnvWithBinary(t, kiroBinary)` (used in `TestKiro_Detect_Absent`)
5. Replace `HookInput{...}` → `e2e.HookInput{...}`
6. Replace `ParseHookInput{...}` → `e2e.ParseHookInput{...}`
7. Replace `NewKiroTranscript(...)` → `NewKiroTranscript(...)` (now local)
8. Replace `AgentRunner` usage (in `TestKiro_NoSubcommand`) — `AgentRunner` is `e2e.AgentRunner`, already accessible through `env.Runner`

Here are the specific import and reference changes:

```go
//go:build e2e

package kiro

import (
	"encoding/json"
	"strings"
	"testing"

	e2e "github.com/entireio/external-agents/e2e"
)
```

For `TestKiro_Detect_Absent` which currently uses `NewTestEnv(t, "entire-agent-kiro")`:
```go
func TestKiro_Detect_Absent(t *testing.T) {
	t.Parallel()
	env := e2e.NewTestEnvWithBinary(t, kiroBinary) // no .kiro/
	// ... rest unchanged
}
```

For tests using `HookInput` or `ParseHookInput`, prefix with `e2e.`:
```go
input := e2e.HookInput{SessionID: "test-session-123"}
// ...
input := e2e.ParseHookInput{Prompt: "do the thing"}
```

- [ ] **Step 2: Delete the old `e2e/kiro_test.go`**

```bash
rm e2e/kiro_test.go
```

- [ ] **Step 3: Verify the kiro subpackage tests compile**

Run: `cd /Users/alisha/Projects/external-agents/e2e && go test -tags=e2e -count=1 -run TestKiro_Info ./kiro/`
Expected: `TestKiro_Info` passes (builds kiro binary, runs single test)

- [ ] **Step 4: Run all kiro tests**

Run: `cd /Users/alisha/Projects/external-agents/e2e && go test -tags=e2e -v -count=1 ./kiro/`
Expected: all 30 kiro binary tests pass

- [ ] **Step 5: Commit**

```bash
git add e2e/kiro/kiro_test.go
git rm e2e/kiro_test.go
git commit -m "refactor(e2e): move kiro binary tests to e2e/kiro/ subpackage"
```

---

### Task 5: Clean up root `e2e/` package

Remove kiro-specific code from the root package files now that it's in `e2e/kiro/`.

**Files:**
- Modify: `e2e/testenv.go` — remove `NewKiroTestEnv`, `NewKiroGitEnv`
- Modify: `e2e/fixtures.go` — remove `KiroTranscript` and related types
- Rename: `e2e/kiro_lifecycle_test.go` → `e2e/lifecycle_test.go`

- [ ] **Step 1: Remove `NewKiroTestEnv` and `NewKiroGitEnv` from `e2e/testenv.go`**

Delete these two functions (lines 47-62 in current `testenv.go`):
```go
// DELETE: NewKiroTestEnv
// DELETE: NewKiroGitEnv
```

- [ ] **Step 2: Remove `KiroTranscript` and related types from `e2e/fixtures.go`**

Remove everything from line 52 onwards in `fixtures.go`:
- `KiroTranscript` struct
- `kiroHistoryEntry` struct
- `kiroUserMessage` struct
- `NewKiroTranscript` function
- `marshalPromptContent` function
- `AddPrompt`, `AddPromptWithFileEdit`, `AddResponse` methods
- `JSON`, `WriteToFile` methods

The file should only contain `HookInput`, `ParseHookInput`, and their `JSON()` methods.

- [ ] **Step 3: Rename lifecycle test file**

```bash
git mv e2e/kiro_lifecycle_test.go e2e/lifecycle_test.go
```

No content changes needed — the file already uses `ForEachAgent` and is agent-agnostic.

- [ ] **Step 4: Verify root package still compiles and tests pass**

Run: `cd /Users/alisha/Projects/external-agents && make test-e2e`
Expected: all tests pass (root lifecycle tests + kiro subpackage tests)

- [ ] **Step 5: Commit**

```bash
git add e2e/testenv.go e2e/fixtures.go e2e/lifecycle_test.go
git rm e2e/kiro_lifecycle_test.go
git commit -m "refactor(e2e): remove kiro-specific code from root, rename lifecycle test"
```

---

### Task 6: Update Makefile for subpackage-aware test targets

The current Makefile already uses `./...` which includes subpackages. Verify it works and optionally add a target for running only binary-level tests.

**Files:**
- Modify: `Makefile`

- [ ] **Step 1: Verify existing targets work**

Run: `make test-e2e` — should run root + kiro subpackage tests
Run: `make test-e2e AGENT=kiro` — should run root (filtered to kiro) + kiro subpackage tests
Run: `make test-e2e-lifecycle AGENT=kiro` — should run only lifecycle tests

Expected: all pass

- [ ] **Step 2: Add `test-e2e-binary` target** (optional, for running only binary contract tests)

Add to Makefile after `test-e2e-lifecycle`:

```makefile
test-e2e-binary:
ifdef AGENT
	cd e2e && go test -tags=e2e -v -count=1 ./$(AGENT)/
else
	@echo "Usage: make test-e2e-binary AGENT=kiro"
	@exit 1
endif
```

This lets you run `make test-e2e-binary AGENT=kiro` to test only the kiro binary contract, skipping lifecycle tests entirely.

- [ ] **Step 3: Update `.PHONY`**

```makefile
.PHONY: test-e2e test-unit test-all test-e2e-lifecycle test-e2e-binary
```

- [ ] **Step 4: Verify new target**

Run: `make test-e2e-binary AGENT=kiro`
Expected: runs only `e2e/kiro/` tests, builds only kiro binary

- [ ] **Step 5: Commit**

```bash
git add Makefile
git commit -m "feat: add test-e2e-binary target for agent-specific binary tests"
```

---

## Verification Checklist

After all tasks complete, verify:

1. `make test-e2e` — runs all e2e tests (root lifecycle + kiro binary)
2. `make test-e2e AGENT=kiro` — runs kiro-only lifecycle + kiro binary tests
3. `make test-e2e-lifecycle AGENT=kiro` — runs only kiro lifecycle tests
4. `make test-e2e-binary AGENT=kiro` — runs only kiro binary contract tests
5. `cd e2e && go test -tags=e2e -v -count=1 ./kiro/` — runs kiro subpackage directly
6. No kiro-specific code remains in root `e2e/` package (except `NewTestEnv` which is generic)

## Adding a New Agent (e.g. Cursor)

With this structure, adding a new agent's binary tests requires:

1. Create `e2e/cursor/setup_test.go` — `TestMain` that calls `e2e.BuildAgent("entire-agent-cursor", tmpDir)`
2. Create `e2e/cursor/testenv_test.go` — `NewCursorTestEnv` with cursor-specific dirs
3. Create `e2e/cursor/fixtures_test.go` — cursor-specific transcript builders
4. Create `e2e/cursor/cursor_test.go` — cursor binary contract tests
5. Create `e2e/agents/cursor.go` — register cursor in the agent registry (for lifecycle tests)
6. Run `make test-e2e-binary AGENT=cursor` to test just the new agent
