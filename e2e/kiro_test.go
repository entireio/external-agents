//go:build e2e

package e2e

import (
	"encoding/json"
	"strings"
	"testing"
)

// --- Identity ---

func TestKiro_Info(t *testing.T) {
	t.Parallel()
	env := NewKiroTestEnv(t)

	var resp struct {
		ProtocolVersion int      `json:"protocol_version"`
		Name            string   `json:"name"`
		Type            string   `json:"type"`
		Description     string   `json:"description"`
		IsPreview       bool     `json:"is_preview"`
		ProtectedDirs   []string `json:"protected_dirs"`
		HookNames       []string `json:"hook_names"`
		Capabilities    struct {
			Hooks              bool `json:"hooks"`
			TranscriptAnalyzer bool `json:"transcript_analyzer"`
		} `json:"capabilities"`
	}
	env.Runner.RunJSON(t, &resp, "", "info")

	if resp.ProtocolVersion != 1 {
		t.Errorf("protocol_version = %d, want 1", resp.ProtocolVersion)
	}
	if resp.Name != "kiro" {
		t.Errorf("name = %q, want %q", resp.Name, "kiro")
	}
	if resp.Type != "Kiro" {
		t.Errorf("type = %q, want %q", resp.Type, "Kiro")
	}
	if !resp.Capabilities.Hooks {
		t.Error("capabilities.hooks should be true")
	}
	if !resp.Capabilities.TranscriptAnalyzer {
		t.Error("capabilities.transcript_analyzer should be true")
	}
	if len(resp.HookNames) != 5 {
		t.Errorf("hook_names count = %d, want 5", len(resp.HookNames))
	}
	if len(resp.ProtectedDirs) != 1 || resp.ProtectedDirs[0] != ".kiro" {
		t.Errorf("protected_dirs = %v, want [.kiro]", resp.ProtectedDirs)
	}
}

func TestKiro_Detect_Present(t *testing.T) {
	t.Parallel()
	env := NewKiroTestEnv(t) // has .kiro/

	var resp struct {
		Present bool `json:"present"`
	}
	env.Runner.RunJSON(t, &resp, "", "detect")

	if !resp.Present {
		t.Error("detect should return present=true when .kiro/ exists")
	}
}

func TestKiro_Detect_Absent(t *testing.T) {
	t.Parallel()
	env := NewTestEnv(t, "entire-agent-kiro") // no .kiro/

	var resp struct {
		Present bool `json:"present"`
	}
	env.Runner.RunJSON(t, &resp, "", "detect")

	if resp.Present {
		t.Error("detect should return present=false when .kiro/ is absent")
	}
}

// --- Session Management ---

func TestKiro_GetSessionID(t *testing.T) {
	t.Parallel()
	env := NewKiroTestEnv(t)

	input := HookInput{SessionID: "test-session-123"}

	var resp struct {
		SessionID string `json:"session_id"`
	}
	env.Runner.RunJSON(t, &resp, input.JSON(t), "get-session-id")

	if resp.SessionID != "test-session-123" {
		t.Errorf("session_id = %q, want %q", resp.SessionID, "test-session-123")
	}
}

func TestKiro_GetSessionID_Generated(t *testing.T) {
	t.Parallel()
	env := NewKiroTestEnv(t)

	input := HookInput{}

	var resp struct {
		SessionID string `json:"session_id"`
	}
	env.Runner.RunJSON(t, &resp, input.JSON(t), "get-session-id")

	if resp.SessionID == "" {
		t.Error("session_id should not be empty when no ID provided")
	}
}

func TestKiro_GetSessionDir(t *testing.T) {
	t.Parallel()
	env := NewKiroTestEnv(t)

	var resp struct {
		SessionDir string `json:"session_dir"`
	}
	env.Runner.RunJSON(t, &resp, "", "get-session-dir", "-repo-path", env.Dir)

	want := env.AbsPath(".entire/tmp")
	if resp.SessionDir != want {
		t.Errorf("session_dir = %q, want %q", resp.SessionDir, want)
	}
}

func TestKiro_ResolveSessionFile(t *testing.T) {
	t.Parallel()
	env := NewKiroTestEnv(t)

	sessionDir := env.AbsPath(".entire/tmp")

	var resp struct {
		SessionFile string `json:"session_file"`
	}
	env.Runner.RunJSON(t, &resp, "", "resolve-session-file",
		"-session-dir", sessionDir,
		"-session-id", "abc-123")

	want := sessionDir + "/abc-123.json"
	if resp.SessionFile != want {
		t.Errorf("session_file = %q, want %q", resp.SessionFile, want)
	}
}

func TestKiro_WriteAndReadSession(t *testing.T) {
	t.Parallel()
	env := NewKiroTestEnv(t)

	sessionRef := env.AbsPath(".entire/tmp/sess-write-test.json")

	// Write a session
	writeInput := map[string]interface{}{
		"session_id":  "sess-write-test",
		"agent_name":  "kiro",
		"repo_path":   env.Dir,
		"session_ref": sessionRef,
		"start_time":  "2026-01-01T00:00:00Z",
		"native_data": []byte(`{"hello":"world"}`),
	}
	writeJSON, _ := json.Marshal(writeInput)
	env.Runner.MustSucceed(t, string(writeJSON), "write-session")

	// Verify the file was written
	if !env.FileExists(".entire/tmp/sess-write-test.json") {
		t.Fatal("session file was not written")
	}

	// Read it back
	readInput := HookInput{
		SessionID:  "sess-write-test",
		SessionRef: sessionRef,
	}
	var resp struct {
		SessionID  string `json:"session_id"`
		AgentName  string `json:"agent_name"`
		NativeData []byte `json:"native_data"`
	}
	env.Runner.RunJSON(t, &resp, readInput.JSON(t), "read-session")

	if resp.SessionID != "sess-write-test" {
		t.Errorf("session_id = %q, want %q", resp.SessionID, "sess-write-test")
	}
	if resp.AgentName != "kiro" {
		t.Errorf("agent_name = %q, want %q", resp.AgentName, "kiro")
	}
}

// --- Transcript ---

func TestKiro_ReadTranscript(t *testing.T) {
	t.Parallel()
	env := NewKiroTestEnv(t)

	transcript := NewKiroTranscript("conv-1").AddPrompt("hello").AddResponse("summarize", "done")
	transcriptPath := transcript.WriteToFile(t, env, "transcript.json")

	result := env.Runner.MustSucceed(t, "", "read-transcript", "-session-ref", transcriptPath)
	if len(result.Stdout) == 0 {
		t.Error("read-transcript returned empty stdout")
	}

	// Should be valid JSON
	var parsed map[string]interface{}
	if err := json.Unmarshal(result.Stdout, &parsed); err != nil {
		t.Fatalf("read-transcript output is not valid JSON: %v", err)
	}
}

func TestKiro_ChunkTranscript(t *testing.T) {
	t.Parallel()
	env := NewKiroTestEnv(t)

	content := strings.Repeat("abcdefghij", 10) // 100 bytes

	var resp struct {
		Chunks [][]byte `json:"chunks"`
	}
	env.Runner.RunJSON(t, &resp, content, "chunk-transcript", "-max-size", "30")

	if len(resp.Chunks) < 3 {
		t.Errorf("expected at least 3 chunks for 100 bytes with max-size 30, got %d", len(resp.Chunks))
	}
}

func TestKiro_ReassembleTranscript(t *testing.T) {
	t.Parallel()
	env := NewKiroTestEnv(t)

	// First chunk the content
	content := "hello world this is a test transcript"
	var chunkResp struct {
		Chunks [][]byte `json:"chunks"`
	}
	env.Runner.RunJSON(t, &chunkResp, content, "chunk-transcript", "-max-size", "10")

	// Now reassemble
	reassembleInput, _ := json.Marshal(chunkResp)
	result := env.Runner.MustSucceed(t, string(reassembleInput), "reassemble-transcript")

	if string(result.Stdout) != content {
		t.Errorf("reassembled = %q, want %q", result.Stdout, content)
	}
}

// --- Hooks ---

func TestKiro_ParseHook_Spawn(t *testing.T) {
	t.Parallel()
	env := NewKiroTestEnv(t)

	var resp struct {
		Type      int    `json:"type"`
		SessionID string `json:"session_id"`
		Timestamp string `json:"timestamp"`
	}
	env.Runner.RunJSON(t, &resp, "{}", "parse-hook", "-hook", "agent-spawn")

	if resp.Type != 1 {
		t.Errorf("type = %d, want 1", resp.Type)
	}
	if resp.SessionID == "" {
		t.Error("session_id should not be empty")
	}
	if resp.Timestamp == "" {
		t.Error("timestamp should not be empty")
	}
}

func TestKiro_ParseHook_PromptSubmit(t *testing.T) {
	t.Parallel()
	env := NewKiroTestEnv(t)

	input := ParseHookInput{Prompt: "do the thing"}

	var resp struct {
		Type      int    `json:"type"`
		SessionID string `json:"session_id"`
		Prompt    string `json:"prompt"`
	}
	env.Runner.RunJSON(t, &resp, input.JSON(t), "parse-hook", "-hook", "user-prompt-submit")

	if resp.Type != 2 {
		t.Errorf("type = %d, want 2", resp.Type)
	}
	if resp.Prompt != "do the thing" {
		t.Errorf("prompt = %q, want %q", resp.Prompt, "do the thing")
	}
}

func TestKiro_ParseHook_PreToolUse(t *testing.T) {
	t.Parallel()
	env := NewKiroTestEnv(t)

	result := env.Runner.MustSucceed(t, "{}", "parse-hook", "-hook", "pre-tool-use")

	if got := strings.TrimSpace(string(result.Stdout)); got != "null" {
		t.Errorf("pre-tool-use should return null, got %q", got)
	}
}

func TestKiro_ParseHook_Stop(t *testing.T) {
	t.Parallel()
	env := NewKiroTestEnv(t)

	input := ParseHookInput{CWD: env.Dir}

	var resp struct {
		Type      int    `json:"type"`
		SessionID string `json:"session_id"`
	}
	env.Runner.RunJSON(t, &resp, input.JSON(t), "parse-hook", "-hook", "stop")

	if resp.Type != 3 {
		t.Errorf("type = %d, want 3", resp.Type)
	}
	if resp.SessionID == "" {
		t.Error("session_id should not be empty")
	}
}

func TestKiro_InstallHooks(t *testing.T) {
	t.Parallel()
	env := NewKiroTestEnv(t)

	var resp struct {
		HooksInstalled int `json:"hooks_installed"`
	}
	env.Runner.RunJSON(t, &resp, "", "install-hooks")

	if resp.HooksInstalled == 0 {
		t.Error("hooks_installed should be > 0")
	}

	// Verify files were created
	if !env.FileExists(".kiro/agents/entire.json") {
		t.Error(".kiro/agents/entire.json should exist after install")
	}
	if !env.FileExists(".kiro/hooks/entire-stop.kiro.hook") {
		t.Error(".kiro/hooks/entire-stop.kiro.hook should exist after install")
	}
	if !env.FileExists(".kiro/hooks/entire-prompt-submit.kiro.hook") {
		t.Error(".kiro/hooks/entire-prompt-submit.kiro.hook should exist after install")
	}
}

func TestKiro_UninstallHooks(t *testing.T) {
	t.Parallel()
	env := NewKiroTestEnv(t)

	// Install first
	env.Runner.MustSucceed(t, "", "install-hooks")

	// Verify installed
	if !env.FileExists(".kiro/agents/entire.json") {
		t.Fatal("hooks should be installed before uninstall test")
	}

	// Uninstall
	env.Runner.MustSucceed(t, "", "uninstall-hooks")

	if env.FileExists(".kiro/agents/entire.json") {
		t.Error(".kiro/agents/entire.json should be removed after uninstall")
	}
	if env.FileExists(".kiro/hooks/entire-stop.kiro.hook") {
		t.Error(".kiro/hooks/entire-stop.kiro.hook should be removed after uninstall")
	}
}

func TestKiro_AreHooksInstalled_No(t *testing.T) {
	t.Parallel()
	env := NewKiroTestEnv(t)

	var resp struct {
		Installed bool `json:"installed"`
	}
	env.Runner.RunJSON(t, &resp, "", "are-hooks-installed")

	if resp.Installed {
		t.Error("hooks should not be installed in fresh env")
	}
}

func TestKiro_AreHooksInstalled_Yes(t *testing.T) {
	t.Parallel()
	env := NewKiroTestEnv(t)

	env.Runner.MustSucceed(t, "", "install-hooks")

	var resp struct {
		Installed bool `json:"installed"`
	}
	env.Runner.RunJSON(t, &resp, "", "are-hooks-installed")

	if !resp.Installed {
		t.Error("hooks should be installed after install-hooks")
	}
}

// --- Transcript Analysis ---

func TestKiro_GetTranscriptPosition(t *testing.T) {
	t.Parallel()
	env := NewKiroTestEnv(t)

	transcript := NewKiroTranscript("conv-pos").
		AddPrompt("first").
		AddPrompt("second").
		AddResponse("third", "response")
	path := transcript.WriteToFile(t, env, "pos-transcript.json")

	var resp struct {
		Position int `json:"position"`
	}
	env.Runner.RunJSON(t, &resp, "", "get-transcript-position", "-path", path)

	if resp.Position != 3 {
		t.Errorf("position = %d, want 3", resp.Position)
	}
}

func TestKiro_GetTranscriptPosition_Missing(t *testing.T) {
	t.Parallel()
	env := NewKiroTestEnv(t)

	var resp struct {
		Position int `json:"position"`
	}
	env.Runner.RunJSON(t, &resp, "", "get-transcript-position", "-path", env.AbsPath("nonexistent.json"))

	if resp.Position != 0 {
		t.Errorf("position for missing file = %d, want 0", resp.Position)
	}
}

func TestKiro_ExtractModifiedFiles(t *testing.T) {
	t.Parallel()
	env := NewKiroTestEnv(t)

	transcript := NewKiroTranscript("conv-files").
		AddPromptWithFileEdit("create file", "/tmp/foo.go").
		AddPromptWithFileEdit("edit file", "/tmp/bar.go").
		AddPrompt("no edits here")
	path := transcript.WriteToFile(t, env, "files-transcript.json")

	var resp struct {
		Files           []string `json:"files"`
		CurrentPosition int      `json:"current_position"`
	}
	env.Runner.RunJSON(t, &resp, "", "extract-modified-files", "-path", path, "-offset", "0")

	if len(resp.Files) != 2 {
		t.Errorf("files count = %d, want 2: %v", len(resp.Files), resp.Files)
	}
	if resp.CurrentPosition != 3 {
		t.Errorf("current_position = %d, want 3", resp.CurrentPosition)
	}
}

func TestKiro_ExtractPrompts(t *testing.T) {
	t.Parallel()
	env := NewKiroTestEnv(t)

	transcript := NewKiroTranscript("conv-prompts").
		AddPrompt("first prompt").
		AddResponse("second prompt", "some response").
		AddPrompt("third prompt")
	path := transcript.WriteToFile(t, env, "prompts-transcript.json")

	var resp struct {
		Prompts []string `json:"prompts"`
	}
	env.Runner.RunJSON(t, &resp, "", "extract-prompts", "-session-ref", path, "-offset", "0")

	if len(resp.Prompts) != 3 {
		t.Errorf("prompts count = %d, want 3: %v", len(resp.Prompts), resp.Prompts)
	}
}

func TestKiro_ExtractPrompts_WithOffset(t *testing.T) {
	t.Parallel()
	env := NewKiroTestEnv(t)

	transcript := NewKiroTranscript("conv-prompts-offset").
		AddPrompt("first").
		AddPrompt("second").
		AddPrompt("third")
	path := transcript.WriteToFile(t, env, "prompts-offset.json")

	var resp struct {
		Prompts []string `json:"prompts"`
	}
	env.Runner.RunJSON(t, &resp, "", "extract-prompts", "-session-ref", path, "-offset", "2")

	if len(resp.Prompts) != 1 {
		t.Errorf("prompts count = %d, want 1: %v", len(resp.Prompts), resp.Prompts)
	}
}

func TestKiro_ExtractSummary(t *testing.T) {
	t.Parallel()
	env := NewKiroTestEnv(t)

	transcript := NewKiroTranscript("conv-summary").
		AddResponse("do the thing", "I completed the task successfully")
	path := transcript.WriteToFile(t, env, "summary-transcript.json")

	var resp struct {
		Summary    string `json:"summary"`
		HasSummary bool   `json:"has_summary"`
	}
	env.Runner.RunJSON(t, &resp, "", "extract-summary", "-session-ref", path)

	if !resp.HasSummary {
		t.Error("has_summary should be true")
	}
	if resp.Summary != "I completed the task successfully" {
		t.Errorf("summary = %q, want %q", resp.Summary, "I completed the task successfully")
	}
}

func TestKiro_ExtractSummary_Empty(t *testing.T) {
	t.Parallel()
	env := NewKiroTestEnv(t)

	transcript := NewKiroTranscript("conv-no-summary").AddPrompt("hello")
	path := transcript.WriteToFile(t, env, "no-summary.json")

	var resp struct {
		Summary    string `json:"summary"`
		HasSummary bool   `json:"has_summary"`
	}
	env.Runner.RunJSON(t, &resp, "", "extract-summary", "-session-ref", path)

	if resp.HasSummary {
		t.Error("has_summary should be false for prompt-only transcript")
	}
}

// --- Other ---

func TestKiro_FormatResumeCommand(t *testing.T) {
	t.Parallel()
	env := NewKiroTestEnv(t)

	var resp struct {
		Command string `json:"command"`
	}
	env.Runner.RunJSON(t, &resp, "", "format-resume-command", "-session-id", "session-xyz")

	if resp.Command == "" {
		t.Error("command should not be empty")
	}
	if !strings.Contains(resp.Command, "resume") {
		t.Errorf("command %q should contain 'resume'", resp.Command)
	}
}

func TestKiro_UnknownSubcommand(t *testing.T) {
	t.Parallel()
	env := NewKiroTestEnv(t)

	result := env.Runner.MustFail(t, "", "nonexistent-command")
	if !strings.Contains(string(result.Stderr), "unknown subcommand") {
		t.Errorf("stderr should mention 'unknown subcommand', got: %s", result.Stderr)
	}
}

func TestKiro_NoSubcommand(t *testing.T) {
	t.Parallel()
	env := NewKiroTestEnv(t)

	// Run with no args — the binary expects at least a subcommand
	result := env.Runner.Run("", "")
	// The binary wraps os.Args[1] so passing empty string gives "unknown subcommand: "
	// which exits non-zero. Either way it should not succeed cleanly.
	// Actually passing "" as subcommand will invoke the binary with "" as the arg.
	if result.ExitCode == 0 {
		t.Error("expected non-zero exit for empty subcommand")
	}
}

// --- Install + Idempotency ---

func TestKiro_InstallHooks_Idempotent(t *testing.T) {
	t.Parallel()
	env := NewKiroTestEnv(t)

	// First install
	var resp1 struct {
		HooksInstalled int `json:"hooks_installed"`
	}
	env.Runner.RunJSON(t, &resp1, "", "install-hooks")
	if resp1.HooksInstalled == 0 {
		t.Fatal("first install should install hooks")
	}

	// Second install should be a no-op (returns 0 installed)
	var resp2 struct {
		HooksInstalled int `json:"hooks_installed"`
	}
	env.Runner.RunJSON(t, &resp2, "", "install-hooks")
	if resp2.HooksInstalled != 0 {
		t.Errorf("second install should be idempotent (0 hooks), got %d", resp2.HooksInstalled)
	}
}
