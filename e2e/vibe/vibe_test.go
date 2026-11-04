//go:build e2e

package vibe

import (
	"encoding/json"
	"strings"
	"testing"

	e2e "github.com/entireio/external-agents/e2e"
)

// --- Identity ---

func TestVibe_Info(t *testing.T) {
	t.Parallel()
	env := NewVibeTestEnv(t)

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
			TokenCalculator    bool `json:"token_calculator"`
		} `json:"capabilities"`
	}
	env.Runner.RunJSON(t, &resp, "", "info")

	if resp.ProtocolVersion != 1 {
		t.Errorf("protocol_version = %d, want 1", resp.ProtocolVersion)
	}
	if resp.Name != "mistral-vibe" {
		t.Errorf("name = %q, want %q", resp.Name, "mistral-vibe")
	}
	if resp.Type != "Mistral Vibe" {
		t.Errorf("type = %q, want %q", resp.Type, "Mistral Vibe")
	}
	if !resp.Capabilities.Hooks {
		t.Error("capabilities.hooks should be true")
	}
	if !resp.Capabilities.TranscriptAnalyzer {
		t.Error("capabilities.transcript_analyzer should be true")
	}
	if !resp.Capabilities.TokenCalculator {
		t.Error("capabilities.token_calculator should be true")
	}
	if len(resp.HookNames) != 5 {
		t.Errorf("hook_names count = %d, want 5", len(resp.HookNames))
	}
	if len(resp.ProtectedDirs) != 1 || resp.ProtectedDirs[0] != ".vibe" {
		t.Errorf("protected_dirs = %v, want [.vibe]", resp.ProtectedDirs)
	}
}

func TestVibe_Detect_Present(t *testing.T) {
	t.Parallel()
	env := NewVibeTestEnv(t) // has .vibe/

	var resp struct {
		Present bool `json:"present"`
	}
	env.Runner.RunJSON(t, &resp, "", "detect")

	if !resp.Present {
		t.Error("detect should return present=true when .vibe/ exists")
	}
}

func TestVibe_Detect_Absent(t *testing.T) {
	t.Parallel()
	env := e2e.NewTestEnvWithBinary(t, vibeBinary) // no .vibe/

	var resp struct {
		Present bool `json:"present"`
	}
	env.Runner.RunJSON(t, &resp, "", "detect")

	if resp.Present {
		t.Error("detect should return present=false when .vibe/ is absent")
	}
}

// --- Session Management ---

func TestVibe_GetSessionID(t *testing.T) {
	t.Parallel()
	env := NewVibeTestEnv(t)

	input := e2e.HookInput{SessionID: "test-session-456"}

	var resp struct {
		SessionID string `json:"session_id"`
	}
	env.Runner.RunJSON(t, &resp, input.JSON(t), "get-session-id")

	if resp.SessionID != "test-session-456" {
		t.Errorf("session_id = %q, want %q", resp.SessionID, "test-session-456")
	}
}

func TestVibe_GetSessionID_Generated(t *testing.T) {
	t.Parallel()
	env := NewVibeTestEnv(t)

	input := e2e.HookInput{}

	var resp struct {
		SessionID string `json:"session_id"`
	}
	env.Runner.RunJSON(t, &resp, input.JSON(t), "get-session-id")

	if resp.SessionID == "" {
		t.Error("session_id should not be empty when no ID provided")
	}
}

func TestVibe_GetSessionDir(t *testing.T) {
	t.Parallel()
	env := NewVibeTestEnv(t)

	var resp struct {
		SessionDir string `json:"session_dir"`
	}
	env.Runner.RunJSON(t, &resp, "", "get-session-dir", "-repo-path", env.Dir)

	want := env.AbsPath(".entire/tmp")
	if resp.SessionDir != want {
		t.Errorf("session_dir = %q, want %q", resp.SessionDir, want)
	}
}

func TestVibe_ResolveSessionFile(t *testing.T) {
	t.Parallel()
	env := NewVibeTestEnv(t)

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

func TestVibe_WriteAndReadSession(t *testing.T) {
	t.Parallel()
	env := NewVibeTestEnv(t)

	sessionRef := env.AbsPath(".entire/tmp/sess-write-test.json")

	writeInput := map[string]interface{}{
		"session_id":  "sess-write-test",
		"agent_name":  "mistral-vibe",
		"repo_path":   env.Dir,
		"session_ref": sessionRef,
		"start_time":  "2026-01-01T00:00:00Z",
		"native_data": []byte(`{"hello":"world"}`),
	}
	writeJSON, _ := json.Marshal(writeInput)
	env.Runner.MustSucceed(t, string(writeJSON), "write-session")

	if !env.FileExists(".entire/tmp/sess-write-test.json") {
		t.Fatal("session file was not written")
	}

	readInput := e2e.HookInput{
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
	if resp.AgentName != "mistral-vibe" {
		t.Errorf("agent_name = %q, want %q", resp.AgentName, "mistral-vibe")
	}
}

// --- Transcript ---

func TestVibe_ReadTranscript(t *testing.T) {
	t.Parallel()
	env := NewVibeTestEnv(t)

	transcript := NewVibeTranscript().AddResponse("hello", "Hello! How can I help?")
	path := transcript.WriteToFile(t, env, "transcript.jsonl")

	result := env.Runner.MustSucceed(t, "", "read-transcript", "-session-ref", path)
	if len(result.Stdout) == 0 {
		t.Error("read-transcript returned empty stdout")
	}
}

func TestVibe_ChunkTranscript(t *testing.T) {
	t.Parallel()
	env := NewVibeTestEnv(t)

	content := strings.Repeat("abcdefghij", 10) // 100 bytes

	var resp struct {
		Chunks [][]byte `json:"chunks"`
	}
	env.Runner.RunJSON(t, &resp, content, "chunk-transcript", "-max-size", "30")

	if len(resp.Chunks) < 3 {
		t.Errorf("expected at least 3 chunks for 100 bytes with max-size 30, got %d", len(resp.Chunks))
	}
}

func TestVibe_ReassembleTranscript(t *testing.T) {
	t.Parallel()
	env := NewVibeTestEnv(t)

	content := "hello world this is a test transcript"
	var chunkResp struct {
		Chunks [][]byte `json:"chunks"`
	}
	env.Runner.RunJSON(t, &chunkResp, content, "chunk-transcript", "-max-size", "10")

	reassembleInput, _ := json.Marshal(chunkResp)
	result := env.Runner.MustSucceed(t, string(reassembleInput), "reassemble-transcript")

	if string(result.Stdout) != content {
		t.Errorf("reassembled = %q, want %q", result.Stdout, content)
	}
}

// --- Hooks ---

func TestVibe_ParseHook_SessionStart(t *testing.T) {
	t.Parallel()
	env := NewVibeTestEnv(t)

	payload := VibeHookPayload{
		HookEventName: "session_start",
		CWD:           env.Dir,
		SessionID:     "0e9f7293-0151-4178-ba58-2c48c5abb8df",
	}

	var resp struct {
		Type      int    `json:"type"`
		SessionID string `json:"session_id"`
		Timestamp string `json:"timestamp"`
	}
	env.Runner.RunJSON(t, &resp, payload.JSON(t), "parse-hook", "-hook", "session-start")

	if resp.Type != 1 {
		t.Errorf("type = %d, want 1 (SessionStart)", resp.Type)
	}
	if resp.SessionID != "0e9f7293-0151-4178-ba58-2c48c5abb8df" {
		t.Errorf("session_id = %q, want %q", resp.SessionID, "0e9f7293-0151-4178-ba58-2c48c5abb8df")
	}
	if resp.Timestamp == "" {
		t.Error("timestamp should not be empty")
	}
}

func TestVibe_ParseHook_UserPromptSubmit(t *testing.T) {
	t.Parallel()
	env := NewVibeTestEnv(t)

	payload := VibeHookPayload{
		HookEventName: "user_prompt_submit",
		CWD:           env.Dir,
		SessionID:     "test-session-789",
		Prompt:        "fix the login bug",
	}

	var resp struct {
		Type      int    `json:"type"`
		SessionID string `json:"session_id"`
		Prompt    string `json:"prompt"`
	}
	env.Runner.RunJSON(t, &resp, payload.JSON(t), "parse-hook", "-hook", "user-prompt-submit")

	if resp.Type != 2 {
		t.Errorf("type = %d, want 2 (TurnStart)", resp.Type)
	}
	if resp.Prompt != "fix the login bug" {
		t.Errorf("prompt = %q, want %q", resp.Prompt, "fix the login bug")
	}
}

func TestVibe_ParseHook_PreToolUse(t *testing.T) {
	t.Parallel()
	env := NewVibeTestEnv(t)

	payload := VibeHookPayload{
		HookEventName: "pre_tool_use",
		CWD:           env.Dir,
		SessionID:     "test-session",
		ToolName:      "write_file",
	}

	result := env.Runner.MustSucceed(t, payload.JSON(t), "parse-hook", "-hook", "pre-tool-use")

	if got := strings.TrimSpace(string(result.Stdout)); got != "null" {
		t.Errorf("pre-tool-use should return null, got %q", got)
	}
}

func TestVibe_ParseHook_PostToolUse(t *testing.T) {
	t.Parallel()
	env := NewVibeTestEnv(t)

	payload := VibeHookPayload{
		HookEventName: "post_tool_use",
		CWD:           env.Dir,
		SessionID:     "test-session",
		ToolName:      "write_file",
		ToolOutcome:   "success",
	}

	result := env.Runner.MustSucceed(t, payload.JSON(t), "parse-hook", "-hook", "post-tool-use")

	if got := strings.TrimSpace(string(result.Stdout)); got != "null" {
		t.Errorf("post-tool-use should return null, got %q", got)
	}
}

func TestVibe_ParseHook_TurnEnd(t *testing.T) {
	t.Parallel()
	env := NewVibeTestEnv(t)

	payload := VibeHookPayload{
		HookEventName: "turn_end",
		CWD:           env.Dir,
		SessionID:     "0e9f7293-0151-4178-ba58-2c48c5abb8df",
	}

	var resp struct {
		Type      int    `json:"type"`
		SessionID string `json:"session_id"`
	}
	env.Runner.RunJSON(t, &resp, payload.JSON(t), "parse-hook", "-hook", "turn-end")

	if resp.Type != 3 {
		t.Errorf("type = %d, want 3 (TurnEnd)", resp.Type)
	}
	if resp.SessionID != "0e9f7293-0151-4178-ba58-2c48c5abb8df" {
		t.Errorf("session_id = %q, want %q", resp.SessionID, "0e9f7293-0151-4178-ba58-2c48c5abb8df")
	}
}

func TestVibe_InstallHooks(t *testing.T) {
	t.Parallel()
	env := NewVibeTestEnv(t)

	var resp struct {
		HooksInstalled int `json:"hooks_installed"`
	}
	env.Runner.RunJSON(t, &resp, "", "install-hooks")

	if resp.HooksInstalled == 0 {
		t.Error("hooks_installed should be > 0")
	}

	if !env.FileExists(".vibe/config.toml") {
		t.Error(".vibe/config.toml should exist after install")
	}

	content := env.ReadFile(".vibe/config.toml")
	if !strings.Contains(content, "entire hooks mistral-vibe") {
		t.Error("config.toml should contain 'entire hooks mistral-vibe'")
	}
	if !strings.Contains(content, "[[hooks.session_start]]") {
		t.Error("config.toml should contain [[hooks.session_start]]")
	}
	if !strings.Contains(content, "[[hooks.turn_end]]") {
		t.Error("config.toml should contain [[hooks.turn_end]]")
	}
}

func TestVibe_UninstallHooks(t *testing.T) {
	t.Parallel()
	env := NewVibeTestEnv(t)

	env.Runner.MustSucceed(t, "", "install-hooks")

	if !env.FileExists(".vibe/config.toml") {
		t.Fatal("hooks should be installed before uninstall test")
	}

	env.Runner.MustSucceed(t, "", "uninstall-hooks")

	if env.FileExists(".vibe/config.toml") {
		content := env.ReadFile(".vibe/config.toml")
		if strings.Contains(content, "entire hooks mistral-vibe") {
			t.Error("config.toml should not contain 'entire hooks mistral-vibe' after uninstall")
		}
	}
}

func TestVibe_AreHooksInstalled_No(t *testing.T) {
	t.Parallel()
	env := NewVibeTestEnv(t)

	var resp struct {
		Installed bool `json:"installed"`
	}
	env.Runner.RunJSON(t, &resp, "", "are-hooks-installed")

	if resp.Installed {
		t.Error("hooks should not be installed in fresh env")
	}
}

func TestVibe_AreHooksInstalled_Yes(t *testing.T) {
	t.Parallel()
	env := NewVibeTestEnv(t)

	env.Runner.MustSucceed(t, "", "install-hooks")

	var resp struct {
		Installed bool `json:"installed"`
	}
	env.Runner.RunJSON(t, &resp, "", "are-hooks-installed")

	if !resp.Installed {
		t.Error("hooks should be installed after install-hooks")
	}
}

func TestVibe_InstallHooks_Idempotent(t *testing.T) {
	t.Parallel()
	env := NewVibeTestEnv(t)

	var resp1 struct {
		HooksInstalled int `json:"hooks_installed"`
	}
	env.Runner.RunJSON(t, &resp1, "", "install-hooks")
	if resp1.HooksInstalled == 0 {
		t.Fatal("first install should install hooks")
	}

	var resp2 struct {
		HooksInstalled int `json:"hooks_installed"`
	}
	env.Runner.RunJSON(t, &resp2, "", "install-hooks")
	if resp2.HooksInstalled != 0 {
		t.Errorf("second install should be idempotent (0 hooks), got %d", resp2.HooksInstalled)
	}
}

// --- Transcript Analysis ---

func TestVibe_GetTranscriptPosition(t *testing.T) {
	t.Parallel()
	env := NewVibeTestEnv(t)

	transcript := NewVibeTranscript().
		AddResponse("hello", "Hi there!").
		AddResponse("what is 2+2", "4")
	path := transcript.WriteToFile(t, env, "pos-transcript.jsonl")

	var resp struct {
		Position int `json:"position"`
	}
	env.Runner.RunJSON(t, &resp, "", "get-transcript-position", "-path", path)

	// 2 user + 2 assistant = 4 messages
	if resp.Position != 4 {
		t.Errorf("position = %d, want 4", resp.Position)
	}
}

func TestVibe_GetTranscriptPosition_Missing(t *testing.T) {
	t.Parallel()
	env := NewVibeTestEnv(t)

	var resp struct {
		Position int `json:"position"`
	}
	env.Runner.RunJSON(t, &resp, "", "get-transcript-position", "-path", env.AbsPath("nonexistent.jsonl"))

	if resp.Position != 0 {
		t.Errorf("position for missing file = %d, want 0", resp.Position)
	}
}

func TestVibe_ExtractModifiedFiles(t *testing.T) {
	t.Parallel()
	env := NewVibeTestEnv(t)

	transcript := NewVibeTranscript().
		AddToolUse("create file", "write_file", "/tmp/foo.go").
		AddToolUse("edit file", "search_replace", "/tmp/bar.go").
		AddResponse("no edits here", "Sure, no problem")
	path := transcript.WriteToFile(t, env, "files-transcript.jsonl")

	var resp struct {
		Files           []string `json:"files"`
		CurrentPosition int      `json:"current_position"`
	}
	env.Runner.RunJSON(t, &resp, "", "extract-modified-files", "-path", path, "-offset", "0")

	if len(resp.Files) != 2 {
		t.Errorf("files count = %d, want 2: %v", len(resp.Files), resp.Files)
	}
}

func TestVibe_ExtractPrompts(t *testing.T) {
	t.Parallel()
	env := NewVibeTestEnv(t)

	transcript := NewVibeTranscript().
		AddResponse("first prompt", "response 1").
		AddResponse("second prompt", "response 2").
		AddPrompt("third prompt")
	path := transcript.WriteToFile(t, env, "prompts-transcript.jsonl")

	var resp struct {
		Prompts []string `json:"prompts"`
	}
	env.Runner.RunJSON(t, &resp, "", "extract-prompts", "-session-ref", path, "-offset", "0")

	if len(resp.Prompts) != 3 {
		t.Errorf("prompts count = %d, want 3: %v", len(resp.Prompts), resp.Prompts)
	}
}

func TestVibe_ExtractPrompts_WithOffset(t *testing.T) {
	t.Parallel()
	env := NewVibeTestEnv(t)

	transcript := NewVibeTranscript().
		AddResponse("first", "resp1").
		AddResponse("second", "resp2").
		AddResponse("third", "resp3")
	path := transcript.WriteToFile(t, env, "prompts-offset.jsonl")

	var resp struct {
		Prompts []string `json:"prompts"`
	}
	// Offset 4 = skip first 4 lines (user, assistant, user, assistant)
	env.Runner.RunJSON(t, &resp, "", "extract-prompts", "-session-ref", path, "-offset", "4")

	if len(resp.Prompts) != 1 {
		t.Errorf("prompts count = %d, want 1: %v", len(resp.Prompts), resp.Prompts)
	}
}

func TestVibe_ExtractSummary(t *testing.T) {
	t.Parallel()
	env := NewVibeTestEnv(t)

	transcript := NewVibeTranscript().
		AddResponse("do the thing", "I completed the task successfully")
	path := transcript.WriteToFile(t, env, "summary-transcript.jsonl")

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

func TestVibe_ExtractSummary_Empty(t *testing.T) {
	t.Parallel()
	env := NewVibeTestEnv(t)

	transcript := NewVibeTranscript().AddPrompt("hello")
	path := transcript.WriteToFile(t, env, "no-summary.jsonl")

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

func TestVibe_FormatResumeCommand(t *testing.T) {
	t.Parallel()
	env := NewVibeTestEnv(t)

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
	if !strings.Contains(resp.Command, "session-xyz") {
		t.Errorf("command %q should contain session ID", resp.Command)
	}
}

func TestVibe_UnknownSubcommand(t *testing.T) {
	t.Parallel()
	env := NewVibeTestEnv(t)

	result := env.Runner.MustFail(t, "", "nonexistent-command")
	if !strings.Contains(string(result.Stderr), "unknown subcommand") {
		t.Errorf("stderr should mention 'unknown subcommand', got: %s", result.Stderr)
	}
}

func TestVibe_NoSubcommand(t *testing.T) {
	t.Parallel()
	env := NewVibeTestEnv(t)

	result := env.Runner.Run("", "")
	if result.ExitCode == 0 {
		t.Error("expected non-zero exit for empty subcommand")
	}
}
