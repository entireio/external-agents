package omp

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseHookLifecycle(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", root)
	t.Setenv("HOME", root)
	t.Setenv("PI_CODING_AGENT_DIR", "")
	t.Setenv("XDG_DATA_HOME", "")
	sessionDir, err := New().GetSessionDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(sessionDir, "2026-07-26T00-00-00-000Z_file-id.jsonl")
	original := []byte(
		"{\"type\":\"title\"}\n" +
			"{\"type\":\"session\",\"version\":3,\"id\":\"payload-id\"}\n" +
			"{\"type\":\"message\",\"id\":\"m1\",\"parentId\":null," +
			"\"message\":{\"role\":\"assistant\",\"model\":\"model-x\",\"content\":[]}}\n",
	)
	if err := os.WriteFile(source, original, 0o644); err != nil {
		t.Fatal(err)
	}
	agent := &Agent{}

	start, err := agent.ParseHook(HookNameSessionStart, []byte(`{
		"type":"session_start",
		"cwd":"`+root+`",
		"session_id":"payload-id",
		"session_file":"`+source+`"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if start == nil || start.Type != 1 || start.SessionID != "payload-id" || start.SessionRef != source {
		t.Fatalf("session_start event = %+v", start)
	}

	turnStart, err := agent.ParseHook(HookNameAgentStart, []byte(`{
		"type":"agent_start",
		"cwd":"`+root+`",
		"session_id":"payload-id",
		"session_file":"`+source+`",
		"prompt":"keep this prompt"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if turnStart == nil || turnStart.Type != 2 || turnStart.SessionID != "payload-id" ||
		turnStart.SessionRef != source || turnStart.Prompt != "keep this prompt" {
		t.Fatalf("agent_start event = %+v", turnStart)
	}

	turnEnd, err := agent.ParseHook(HookNameAgentEnd, []byte(`{
		"type":"agent_end",
		"cwd":"`+root+`",
		"session_id":"payload-id",
		"session_file":"`+source+`"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	wantSnapshot := filepath.Join(filepath.Dir(source), ".entire", "payload-id.jsonl")
	if turnEnd == nil || turnEnd.Type != 3 || turnEnd.SessionID != "payload-id" ||
		turnEnd.SessionRef != wantSnapshot || turnEnd.Model != "model-x" {
		t.Fatalf("agent_end event = %+v", turnEnd)
	}
	snapshot, err := os.ReadFile(wantSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	if string(snapshot) != string(original) {
		t.Fatalf("snapshot = %q, want %q", snapshot, original)
	}

	cache := cachedSessionPath()
	if _, err := os.Stat(cache); err != nil {
		t.Fatalf("active session cache missing: %v", err)
	}
	shutdown, err := agent.ParseHook(HookNameSessionShutdown, []byte(`{malformed`))
	if err != nil {
		t.Fatal(err)
	}
	if shutdown != nil {
		t.Fatalf("session_shutdown event = %+v, want nil", shutdown)
	}
	if _, err := os.Stat(cache); !os.IsNotExist(err) {
		t.Fatalf("active session cache remains after shutdown: %v", err)
	}
}

func TestParseHookEmptyMalformedAndUnknown(t *testing.T) {
	t.Setenv("ENTIRE_REPO_ROOT", t.TempDir())
	agent := &Agent{}
	for _, input := range [][]byte{nil, {}, []byte(" \n\t")} {
		event, err := agent.ParseHook(HookNameSessionStart, input)
		if err != nil || event != nil {
			t.Fatalf("empty payload: event=%+v err=%v", event, err)
		}
	}
	if event, err := agent.ParseHook(HookNameSessionStart, []byte(`{`)); err == nil || event != nil {
		t.Fatalf("malformed payload: event=%+v err=%v", event, err)
	}
	if event, err := agent.ParseHook("unknown", []byte(`{"session_id":"id"}`)); err != nil || event != nil {
		t.Fatalf("unknown hook: event=%+v err=%v", event, err)
	}
	if event, err := agent.ParseHook(HookNameAgentStart, []byte(`{}`)); err != nil || event != nil {
		t.Fatalf("missing identity: event=%+v err=%v", event, err)
	}
}

func TestParseHookSessionIDPrecedenceAndFallbacks(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", root)
	agent := &Agent{}
	path := filepath.Join(root, "2026-07-26T03-14-06-592Z_filename-id.jsonl")

	event, err := agent.ParseHook(HookNameAgentStart, []byte(`{
		"session_id":"payload-id",
		"session_file":"`+path+`",
		"prompt":"one"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if event.SessionID != "payload-id" {
		t.Fatalf("payload ID did not win: %+v", event)
	}

	event, err = agent.ParseHook(HookNameAgentStart, []byte(`{
		"session_file":"`+path+`",
		"prompt":"two"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if event.SessionID != "filename-id" {
		t.Fatalf("filename fallback = %+v", event)
	}

	event, err = agent.ParseHook(HookNameAgentStart, []byte(`{"prompt":"three"}`))
	if err != nil {
		t.Fatal(err)
	}
	if event.SessionID != "filename-id" {
		t.Fatalf("cache fallback = %+v", event)
	}
}

func TestParseHookRejectsUnsafeSessionIDs(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", root)
	agent := &Agent{}
	for _, id := range []string{"../escape", "nested/id", `nested\id`, ".", "..", "white space"} {
		event, err := agent.ParseHook(HookNameSessionStart, []byte(`{"session_id":"`+id+`"}`))
		if err == nil || event != nil {
			t.Fatalf("unsafe ID %q: event=%+v err=%v", id, event, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".entire", "tmp", "escape.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("unsafe ID created a file: %v", err)
	}
}

func TestTurnEndSnapshotIsStableAndPrivate(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", root)
	t.Setenv("HOME", root)
	t.Setenv("PI_CODING_AGENT_DIR", "")
	t.Setenv("XDG_DATA_HOME", "")
	sessionDir, err := New().GetSessionDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(sessionDir, "live.jsonl")
	snapshotDir := filepath.Join(filepath.Dir(source), ".entire")
	if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(snapshotDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("first\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	event, err := (&Agent{}).ParseHook(HookNameAgentEnd, []byte(`{
		"cwd":"`+root+`",
		"session_id":"stable-id",
		"session_file":"`+source+`",
		"model":"payload-model"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if event.Model != "payload-model" {
		t.Fatalf("agent_end model = %q, want payload-model", event.Model)
	}
	if err := os.WriteFile(source, []byte("second\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(event.SessionRef)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "first\n" {
		t.Fatalf("snapshot changed with live transcript: %q", data)
	}
	assertMode(t, filepath.Dir(event.SessionRef), 0o700)
	assertMode(t, event.SessionRef, 0o600)
	assertMode(t, cachedSessionPath(), 0o600)
	if strings.HasPrefix(event.SessionRef, filepath.Join(root, ".entire", "tmp")) {
		t.Fatalf("snapshot overlaps another agent's session directory: %s", event.SessionRef)
	}
}

func TestTurnEndWithoutLiveSessionFileStillEmits(t *testing.T) {
	t.Setenv("ENTIRE_REPO_ROOT", t.TempDir())
	event, err := (&Agent{}).ParseHook(HookNameAgentEnd, []byte(`{"session_id":"safe-id"}`))
	if err != nil || event == nil || event.Type != 3 || event.SessionID != "safe-id" ||
		event.SessionRef != "" || event.Model != "" {
		t.Fatalf("agent_end without session file: event=%+v err=%v", event, err)
	}
}

func TestTurnEndMissingLiveSessionFileDoesNotLeakRef(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", root)
	t.Setenv("HOME", root)
	t.Setenv("PI_CODING_AGENT_DIR", "")
	t.Setenv("XDG_DATA_HOME", "")
	outside := t.TempDir()
	missing := filepath.Join(outside, "missing.jsonl")
	event, err := (&Agent{}).ParseHook(HookNameAgentEnd, []byte(`{
		"cwd":"`+root+`",
		"session_id":"safe-id",
		"session_file":"`+missing+`"
	}`))
	if err != nil || event == nil || event.Type != 3 || event.SessionRef != "" || event.Model != "" {
		t.Fatalf("agent_end with missing session file: event=%+v err=%v", event, err)
	}
	if _, err := os.Stat(filepath.Join(outside, ".entire")); !os.IsNotExist(err) {
		t.Fatalf("agent_end wrote outside the managed session directory: %v", err)
	}
}

func TestTurnEndRejectsSymlinkSnapshotDirectory(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", root)
	t.Setenv("HOME", root)
	t.Setenv("PI_CODING_AGENT_DIR", "")
	t.Setenv("XDG_DATA_HOME", "")
	sessionDir, err := New().GetSessionDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(sessionDir, "session.jsonl")
	if err := os.WriteFile(source, []byte(ompHeader()), 0o600); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err := os.Chmod(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(sessionDir, ".entire")); err != nil {
		t.Fatal(err)
	}

	event, err := (&Agent{}).ParseHook(HookNameAgentEnd, []byte(`{
		"cwd":"`+root+`",
		"session_id":"safe-id",
		"session_file":"`+source+`"
	}`))
	if err != nil || event == nil || event.SessionRef != "" {
		t.Fatalf("agent_end with symlink snapshot dir: event=%+v err=%v", event, err)
	}
	assertMode(t, target, 0o755)
}

func TestParseHookAcceptsGenericProtocolAliases(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", root)
	source := filepath.Join(root, "generic-id.jsonl")
	if err := os.WriteFile(source, []byte(ompHeader()), 0o600); err != nil {
		t.Fatal(err)
	}

	start, err := (&Agent{}).ParseHook(HookNameAgentStart, []byte(`{
		"session_id":"generic-id",
		"session_ref":"`+source+`",
		"user_prompt":"generic prompt"
	}`))
	if err != nil || start == nil || start.SessionRef != source || start.Prompt != "generic prompt" {
		t.Fatalf("generic hook aliases: event=%+v err=%v", start, err)
	}
}

func TestInstallHooksIdempotentForceAndUninstall(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", root)
	agent := &Agent{}
	path := filepath.Join(root, extensionRelFile)

	count, err := agent.InstallHooks(false, false)
	if err != nil {
		t.Fatal(err)
	}
	if count != 4 || !agent.AreHooksInstalled() {
		t.Fatalf("install count=%d installed=%v", count, agent.AreHooksInstalled())
	}
	assertMode(t, filepath.Dir(path), 0o700)
	assertMode(t, path, 0o600)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	count, err = agent.InstallHooks(true, false)
	if err != nil || count != 0 {
		t.Fatalf("repeated install count=%d err=%v", count, err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("repeated install changed the extension")
	}

	const oldExtension = `import type { ExtensionAPI } from "@oh-my-pi/pi-coding-agent";
import { execFileSync } from "node:child_process";

// entire-agent-omp: project-local Entire integration for OMP.
export default function entireAgentOMP(pi: ExtensionAPI): void {
  pi.on("before_agent_start", (event, ctx) => {
    execFileSync("entire", ["hooks", "omp", "agent_start"], {
      input: JSON.stringify({
        type: "agent_start",
        cwd: ctx.cwd,
        session_id: ctx.sessionManager.getSessionId(),
        session_file: ctx.sessionManager.getSessionFile(),
        prompt: event.prompt,
      }),
    });
  });
}
`
	if err := os.WriteFile(path, []byte(oldExtension), 0o600); err != nil {
		t.Fatal(err)
	}
	count, err = agent.InstallHooks(false, false)
	if err != nil || count != 4 {
		t.Fatalf("owned upgrade count=%d err=%v", count, err)
	}
	upgraded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(upgraded) != generatedExtension {
		t.Fatal("owned extension was not upgraded to the current content")
	}
	if !bytes.Contains(upgraded, []byte(`pi.on("agent_start"`)) ||
		!bytes.Contains(upgraded, []byte(`type: "agent_start"`)) {
		t.Fatal("owned upgrade dropped the Type2 agent_start hook")
	}

	if err := os.WriteFile(path, []byte("foreign extension\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	count, err = agent.InstallHooks(false, false)
	if err == nil || count != 0 {
		t.Fatalf("foreign install count=%d err=%v", count, err)
	}
	foreign, readErr := os.ReadFile(path)
	if readErr != nil || string(foreign) != "foreign extension\n" {
		t.Fatalf("foreign extension was changed: %q err=%v", foreign, readErr)
	}
	count, err = agent.InstallHooks(false, true)
	if err != nil || count != 4 || !agent.AreHooksInstalled() {
		t.Fatalf("forced install count=%d installed=%v err=%v", count, agent.AreHooksInstalled(), err)
	}

	sibling := filepath.Join(root, ".omp", "extensions", "keep.ts")
	if err := os.WriteFile(sibling, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := agent.UninstallHooks(); err != nil {
		t.Fatal(err)
	}
	if agent.AreHooksInstalled() {
		t.Fatal("hooks remain installed")
	}
	if _, err := os.Stat(filepath.Join(root, extensionRelDir)); !os.IsNotExist(err) {
		t.Fatalf("owned extension directory remains: %v", err)
	}
	if data, err := os.ReadFile(sibling); err != nil || string(data) != "keep" {
		t.Fatalf("uninstall changed sibling extension: %q err=%v", data, err)
	}
	if err := agent.UninstallHooks(); err != nil {
		t.Fatalf("repeated uninstall: %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(oldExtension), 0o600); err != nil {
		t.Fatal(err)
	}
	if !agent.AreHooksInstalled() {
		t.Fatal("legacy marker was not recognized")
	}
	if err := agent.UninstallHooks(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("legacy extension remains after uninstall: %v", err)
	}
}

func TestAreHooksInstalledRequiresMarker(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", root)
	path := filepath.Join(root, extensionRelFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("export default function foreign() {}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if (&Agent{}).AreHooksInstalled() {
		t.Fatal("foreign extension reported as installed")
	}
	if err := (&Agent{}).UninstallHooks(); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "export default function foreign() {}" {
		t.Fatalf("uninstall changed foreign extension: %q err=%v", data, err)
	}
}

func TestGeneratedExtensionLifecyclePrivacyAndBashGuard(t *testing.T) {
	for _, snippet := range []string{
		`from "@oh-my-pi/pi-coding-agent"`,
		`"entire",`,
		`["hooks", "omp", name]`,
		`windowsHide: true`,
		`maxBuffer: 4 * 1024 * 1024`,
		`async function startSession(_event: unknown, ctx: ExtensionContext): Promise<void>`,
		`pi.on("session_start", startSession)`,
		`pi.on("session_switch", startSession)`,
		`pi.on("session_branch", startSession)`,
		`sessionGeneration++`,
		`const previousEnd = turnOpen ? armRunEnd() : undefined`,
		`const willContinue = await previousEnd`,
		`turnOpen = false`,
		`pendingPrompt = undefined`,
		`runEnd = undefined`,
		`pi.on("before_agent_start"`,
		`pendingPrompt = event.prompt`,
		`pi.on("agent_start"`,
		`if (pendingPrompt === undefined) {`,
		`const willContinue = await crossRunEnd()`,
		`const prompt = pendingPrompt`,
		`runHookSync("agent_start"`,
		`pi.on("agent_end"`,
		`if (!turnOpen) return`,
		`const willContinue = event.willContinue === true`,
		`event.messages.findLast((message) => message.role === "assistant")`,
		`{ model?: string } | undefined`,
		`runHookSync("agent_end"`,
		`settleRunEnd(willContinue)`,
		`execFileSync(`,
		`{ input: body, timeout, windowsHide: true, maxBuffer: 4 * 1024 * 1024 }`,
		`pi.on("session_shutdown"`,
		`}, 1_500)`,
		`await runHook`,
		`child.stdin?.on("error"`,
		`if (event.toolName !== "bash" || typeof event.input?.command !== "string") return`,
		`GIT_TERMINAL_PROMPT\s*=`,
		"export GIT_TERMINAL_PROMPT=0\n${event.input.command}",
	} {
		if !strings.Contains(generatedExtension, snippet) {
			t.Errorf("generated extension missing %q", snippet)
		}
	}
	for _, forbidden := range []string{
		"SessionEnd",
		"session_end",
		`pi.on("session_stop"`,
		"systemPrompt",
		"messages:",
		"modified_files",
		"shell:",
		"env:",
	} {
		if strings.Contains(generatedExtension, forbidden) {
			t.Errorf("generated extension leaks or configures forbidden %q", forbidden)
		}
	}
	if strings.Count(generatedExtension, "timeout = 10_000") != 2 {
		t.Fatal("hook timeout defaults are not 10 seconds")
	}
}

func TestGeneratedExtensionPairsLogicalTurnsAcrossContinuations(t *testing.T) {
	if got := strings.Count(generatedExtension, `pi.on("agent_end"`); got != 1 {
		t.Fatalf("agent_end handler count = %d, want 1", got)
	}
	if got := strings.Count(generatedExtension, `runHookSync("agent_end"`); got != 2 {
		t.Fatalf("synchronous agent_end hook count = %d, want 2 lifecycle paths", got)
	}

	handlerStart := strings.Index(generatedExtension, `pi.on("agent_end"`)
	if handlerStart < 0 {
		t.Fatal("agent_end handler not found")
	}
	handlerEnd := strings.Index(generatedExtension[handlerStart:], "\n\n  pi.on(\"session_shutdown\"")
	if handlerEnd < 0 {
		t.Fatal("agent_end handler not found")
	}
	handler := generatedExtension[handlerStart : handlerStart+handlerEnd]
	guard := strings.Index(handler, `if (!turnOpen || !turnIdentity) return;`)
	invoke := strings.Index(handler, `runHookSync("agent_end"`)
	closeTurn := strings.Index(handler, `turnOpen = false;`)
	settle := strings.Index(handler, `settleRunEnd(willContinue);`)
	if guard < 0 || invoke < 0 || closeTurn < 0 || settle < 0 ||
		guard > invoke || invoke > closeTurn || closeTurn > settle {
		t.Fatal("agent_end must synchronously close a final turn before releasing its run barrier")
	}
	if strings.Contains(handler, "async") || strings.Contains(handler, "await ") {
		t.Fatal("agent_end handler must finish its hook before returning")
	}

	beforeStart := strings.Index(generatedExtension, `pi.on("before_agent_start"`)
	if beforeStart < 0 {
		t.Fatal("before_agent_start handler not found")
	}
	beforeEnd := strings.Index(generatedExtension[beforeStart:], "\n\n  pi.on(\"agent_start\"")
	if beforeEnd < 0 {
		t.Fatal("before_agent_start handler not found")
	}
	before := generatedExtension[beforeStart : beforeStart+beforeEnd]
	savePrompt := strings.Index(before, `pendingPrompt = event.prompt;`)
	if savePrompt < 0 || strings.Contains(before, `if (turnOpen) return;`) {
		t.Fatal("before_agent_start must retain a prompt that races the preceding agent_end")
	}
	for _, forbidden := range []string{`turnOpen = true`, `runHook(`, `runHookSync(`} {
		if strings.Contains(before, forbidden) {
			t.Fatalf("before_agent_start must not open or emit a turn; found %q", forbidden)
		}
	}

	sessionStart := strings.Index(generatedExtension, `async function startSession(`)
	if sessionStart < 0 {
		t.Fatal("shared session-start handler not found")
	}
	sessionEnd := strings.Index(generatedExtension[sessionStart:], "\n\n  pi.on(\"session_start\"")
	if sessionEnd < 0 {
		t.Fatal("shared session-start handler end not found")
	}
	session := generatedExtension[sessionStart : sessionStart+sessionEnd]
	capturePrevious := strings.Index(session, `const previousEnd = turnOpen ? armRunEnd() : undefined;`)
	invalidateWaiters := strings.Index(session, `sessionGeneration++;`)
	resetOpen := strings.Index(session, `turnOpen = false;`)
	resetPending := strings.Index(session, `pendingPrompt = undefined;`)
	waitPrevious := strings.Index(session, `const willContinue = await previousEnd;`)
	flushDeferred := strings.Index(session, `if (willContinue && deferredEnd) runHookSync("agent_end", deferredEnd);`)
	resetBarrier := strings.Index(session, `runEnd = undefined;`)
	resetResolver := strings.Index(session, `resolveRunEnd = undefined;`)
	resetDeferred := strings.Index(session, `deferredEnd = undefined;`)
	resetIdentity := strings.Index(session, `turnIdentity = undefined;`)
	emit := strings.Index(session, `await runHook("session_start"`)
	if capturePrevious < 0 || invalidateWaiters < 0 || resetOpen < 0 || resetPending < 0 ||
		waitPrevious < 0 || flushDeferred < 0 || resetBarrier < 0 || resetResolver < 0 ||
		resetDeferred < 0 || resetIdentity < 0 || emit < 0 ||
		capturePrevious > invalidateWaiters || invalidateWaiters > resetPending ||
		resetPending > waitPrevious || waitPrevious > flushDeferred || flushDeferred > resetOpen ||
		resetOpen > resetBarrier || resetBarrier > resetResolver || resetResolver > resetDeferred ||
		resetDeferred > resetIdentity || resetIdentity > emit {
		t.Fatal("session changes must drain the old run before resetting and emitting the new session")
	}
}

func TestGeneratedExtensionSharesSessionChangeHandler(t *testing.T) {
	for _, event := range []string{"session_start", "session_switch", "session_branch"} {
		subscription := `pi.on("` + event + `", startSession);`
		if got := strings.Count(generatedExtension, subscription); got != 1 {
			t.Fatalf("%s subscription count = %d, want 1 shared handler", event, got)
		}
	}
	if got := strings.Count(generatedExtension, `async function startSession(`); got != 1 {
		t.Fatalf("shared session-start implementation count = %d, want 1", got)
	}
	if got := strings.Count(generatedExtension, `await runHook("session_start"`); got != 1 {
		t.Fatalf("external session_start emission count = %d, want 1 shared implementation", got)
	}
	if strings.Contains(generatedExtension, `runHook("session_switch"`) ||
		strings.Contains(generatedExtension, `runHook("session_branch"`) {
		t.Fatal("native session changes must retain the external session_start hook contract")
	}
}

func TestGeneratedExtensionStartsOnlyDeliveredPrompts(t *testing.T) {
	start := extensionHandler(t, "agent_start", "agent_end")
	guard := strings.Index(start, `if (pendingPrompt === undefined) {`)
	copyPrompt := strings.Index(start, `const prompt = pendingPrompt;`)
	clearPending := strings.Index(start, `pendingPrompt = undefined;`)
	wait := strings.LastIndex(start, `const willContinue = await crossRunEnd();`)
	resetBarrier := strings.Index(start, `runEnd = undefined;`)
	emit := strings.Index(start, `runHookSync("agent_start"`)
	open := strings.Index(start, `turnOpen = true;`)
	if guard < 0 || copyPrompt < 0 || clearPending < 0 || wait < 0 || resetBarrier < 0 || emit < 0 || open < 0 ||
		guard > copyPrompt || copyPrompt > clearPending || clearPending > wait ||
		wait > resetBarrier || resetBarrier > emit || emit > open {
		t.Fatal("agent_start must consume one prompt, cross the preceding run barrier, then synchronously open")
	}
	if !strings.Contains(start, `pi.on("agent_start", async`) {
		t.Fatal("agent_start must be awaitable while the preceding agent_end notification catches up")
	}
	noPrompt := strings.Index(start, `if (pendingPrompt === undefined) {`)
	directWait := strings.Index(start, `const willContinue = await crossRunEnd();`)
	directRearm := strings.Index(start, `armRunEnd();`)
	if noPrompt < 0 || directWait < noPrompt || directRearm < directWait || directRearm > copyPrompt {
		t.Fatal("a direct continuation agent_start must consume and rearm the physical-run barrier")
	}
}

func TestGeneratedExtensionCancellationContinuationAndOrphanContracts(t *testing.T) {
	before := extensionHandler(t, "before_agent_start", "agent_start")
	start := extensionHandler(t, "agent_start", "agent_end")
	end := extensionHandler(t, "agent_end", "session_shutdown")

	// A second pre-send before_agent_start overwrites the cancelled generation's
	// prompt; no open-turn flag can cause the replacement agent_start to be lost.
	if strings.Count(before, `pendingPrompt = event.prompt;`) != 1 || strings.Contains(before, `turnOpen = true`) {
		t.Fatal("pre-send cancellation must leave only the latest prompt pending and no turn open")
	}
	// The prompt must survive until the delayed end result decides whether this
	// physical run continues the open logical turn or starts a new one.
	if strings.Contains(before, `if (turnOpen) return;`) {
		t.Fatal("a racing before_agent_start still drops its prompt")
	}
	if !strings.Contains(start, `if (pendingPrompt === undefined) {`) ||
		!strings.Contains(start, `if (willContinue) {`) {
		t.Fatal("agent_start does not reject orphans and fold confirmed continuations")
	}
	if !strings.Contains(end, `if (!turnOpen || !turnIdentity) return;`) ||
		!strings.Contains(end, `if (!willContinue) {`) {
		t.Fatal("agent_end does not reject orphans and close only final runs")
	}
	if !strings.Contains(end, `...turnIdentity`) || strings.Contains(end, `ctx.sessionManager`) {
		t.Fatal("agent_end must use the identity captured when its logical turn opened")
	}
}

func TestGeneratedExtensionRunEndBarrierOrdering(t *testing.T) {
	start := extensionHandler(t, "agent_start", "agent_end")
	end := extensionHandler(t, "agent_end", "session_shutdown")
	wait := strings.LastIndex(start, `const willContinue = await crossRunEnd();`)
	fold := strings.LastIndex(start, `if (willContinue) {`)
	startHook := strings.Index(start, `runHookSync("agent_start"`)
	endHook := strings.Index(end, `runHookSync("agent_end"`)
	release := strings.Index(end, `settleRunEnd(willContinue);`)
	if wait < 0 || fold < wait || startHook < fold || endHook < 0 || release < endHook {
		t.Fatal("generated extension does not wait, fold, and release in lifecycle order")
	}

	for _, tc := range []struct {
		name         string
		willContinue bool
		want         string
	}{
		{name: "final", want: "T2(A),T3(A),T2(B),T3(B)"},
		{name: "continuation", willContinue: true, want: "T2(A),T3(A)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actions := []string{"T2(A)"}
			barrier := make(chan bool, 1)
			waiting := make(chan struct{})
			started := make(chan string, 1)
			done := make(chan struct{})

			// omp awaits B's agent_start handler, but A's fire-and-forget
			// agent_end emit may enter this extension concurrently and release it.
			go func() {
				close(waiting)
				if willContinue := <-barrier; !willContinue {
					started <- "T2(B)"
				}
				close(done)
			}()
			<-waiting
			select {
			case <-done:
				t.Fatal("B agent_start crossed the barrier before A agent_end arrived")
			default:
			}

			if !tc.willContinue {
				actions = append(actions, "T3(A)")
			}
			barrier <- tc.willContinue
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("B agent_start deadlocked waiting for A agent_end")
			}
			select {
			case action := <-started:
				actions = append(actions, action)
			default:
			}
			if tc.willContinue {
				actions = append(actions, "T3(A)")
			} else {
				actions = append(actions, "T3(B)")
			}
			if got := strings.Join(actions, ","); got != tc.want {
				t.Fatalf("lifecycle = %s, want %s", got, tc.want)
			}
		})
	}
}

func extensionHandler(t *testing.T, name, next string) string {
	t.Helper()
	start := strings.Index(generatedExtension, `pi.on("`+name+`"`)
	if start < 0 {
		t.Fatalf("%s handler not found", name)
	}
	end := strings.Index(generatedExtension[start:], "\n\n  pi.on(\""+next+"\"")
	if end < 0 {
		t.Fatalf("%s handler end not found", name)
	}
	return generatedExtension[start : start+end]
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %#o, want %#o", path, got, want)
	}
}
