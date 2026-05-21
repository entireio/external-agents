package qwen

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entireio/external-agents/agents/entire-agent-qwen/internal/protocol"
)

func TestDetectUsesQwenBinary(t *testing.T) {
	agent := New()
	agent.LookPath = func(name string) (string, error) {
		if name != qwenBinary {
			t.Fatalf("unexpected binary lookup: %s", name)
		}
		return "/usr/local/bin/qwen", nil
	}
	if !agent.Detect().Present {
		t.Fatal("expected qwen to be detected")
	}

	agent.LookPath = func(string) (string, error) {
		return "", errors.New("not found")
	}
	if agent.Detect().Present {
		t.Fatal("expected qwen to be absent")
	}
}

func TestInstallHooksIdempotentAndUninstallPreservesUserSettings(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repo)

	settingsPath := filepath.Join(repo, ".qwen", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
		t.Fatal(err)
	}
	initial := `{
  "theme": "dark",
  "hooks": {
    "SessionStart": [
      {
        "hooks": [
          {"type": "command", "name": "custom", "command": "echo custom"}
        ]
      }
    ]
  }
}
`
	if err := os.WriteFile(settingsPath, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}

	agent := New()
	count, err := agent.InstallHooks(false, false)
	if err != nil {
		t.Fatal(err)
	}
	if count != len(hookSpecs) {
		t.Fatalf("expected %d hooks installed, got %d", len(hookSpecs), count)
	}
	if !agent.AreHooksInstalled() {
		t.Fatal("expected hooks installed")
	}

	count, err = agent.InstallHooks(false, false)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected idempotent install count 0, got %d", count)
	}

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"theme": "dark"`) {
		t.Fatalf("user setting was not preserved:\n%s", data)
	}
	if !strings.Contains(string(data), `"custom"`) {
		t.Fatalf("custom hook was not preserved:\n%s", data)
	}

	if err := agent.UninstallHooks(); err != nil {
		t.Fatal(err)
	}
	if agent.AreHooksInstalled() {
		t.Fatal("expected hooks uninstalled")
	}
	data, err = os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"custom"`) {
		t.Fatalf("custom hook was removed:\n%s", data)
	}
	if strings.Contains(string(data), "entire hooks qwen") {
		t.Fatalf("Entire hook command still present:\n%s", data)
	}
}

func TestParseHookLifecycleEvents(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repo)

	agent := New()
	input := []byte(`{
  "session_id": "qwen-test-session",
  "transcript_path": "/tmp/qwen-native.jsonl",
  "cwd": "/repo",
  "timestamp": "2026-05-20T12:00:00Z",
  "prompt": "write a file",
  "model": "qwen3-coder-plus",
  "last_assistant_message": "done",
  "error": "server_error",
  "error_details": "api failed",
  "tool_name": "write_file",
  "tool_use_id": "tool-1",
  "tool_input": {"file_path":"hello.txt"},
  "tool_response": {"ok":true}
}`)

	tests := []struct {
		hookName string
		wantType int
		wantNil  bool
	}{
		{HookNameSessionStart, 1, false},
		{HookNameUserPromptSubmit, 2, false},
		{HookNameStop, 3, false},
		{HookNameStopFailure, 3, false},
		{HookNameSessionEnd, 5, false},
		{HookNamePreCompact, 4, false},
		{HookNamePostToolUse, 0, true},
		{HookNamePostToolUseFailure, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.hookName, func(t *testing.T) {
			event, err := agent.ParseHook(tt.hookName, input)
			if err != nil {
				t.Fatal(err)
			}
			if tt.wantNil {
				if event != nil {
					t.Fatalf("expected nil event, got %#v", event)
				}
				return
			}
			if event == nil {
				t.Fatal("expected event")
			}
			if event.Type != tt.wantType {
				t.Fatalf("expected type %d, got %d", tt.wantType, event.Type)
			}
			if event.SessionID != "qwen-test-session" {
				t.Fatalf("unexpected session id %q", event.SessionID)
			}
			if !strings.Contains(event.SessionRef, ".entire/tmp/qwen/qwen-test-session.jsonl") {
				t.Fatalf("unexpected session ref %q", event.SessionRef)
			}
			if event.Metadata["native_transcript_path"] != "/tmp/qwen-native.jsonl" {
				t.Fatalf("native transcript path missing from metadata: %#v", event.Metadata)
			}
		})
	}
}

func TestTranscriptAnalysisAndCompactTranscript(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repo)
	agent := New()

	inputs := map[string]string{
		HookNameUserPromptSubmit: `{"session_id":"qwen-test","timestamp":"2026-05-20T12:00:00Z","prompt":"Create hello.txt"}`,
		HookNamePostToolUse:      `{"session_id":"qwen-test","timestamp":"2026-05-20T12:00:01Z","tool_name":"write_file","tool_use_id":"tool-1","tool_input":{"file_path":"hello.txt"},"tool_response":{"ok":true}}`,
		HookNameStop:             `{"session_id":"qwen-test","timestamp":"2026-05-20T12:00:02Z","last_assistant_message":"Created hello.txt"}`,
	}
	for _, hook := range []string{HookNameUserPromptSubmit, HookNamePostToolUse, HookNameStop} {
		if _, err := agent.ParseHook(hook, []byte(inputs[hook])); err != nil {
			t.Fatal(err)
		}
	}

	sessionRef := agent.sidecarPath("qwen-test")
	position, err := agent.GetTranscriptPosition(sessionRef)
	if err != nil {
		t.Fatal(err)
	}
	if position != 3 {
		t.Fatalf("expected 3 records, got %d", position)
	}

	prompts, err := agent.ExtractPrompts(sessionRef, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 1 || prompts[0] != "Create hello.txt" {
		t.Fatalf("unexpected prompts: %#v", prompts)
	}

	files, current, err := agent.ExtractModifiedFiles(sessionRef, 0)
	if err != nil {
		t.Fatal(err)
	}
	if current != 3 {
		t.Fatalf("expected current position 3, got %d", current)
	}
	if len(files) != 1 || files[0] != "hello.txt" {
		t.Fatalf("unexpected modified files: %#v", files)
	}

	summary, ok, err := agent.ExtractSummary(sessionRef)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || summary != "Created hello.txt" {
		t.Fatalf("unexpected summary %q ok=%v", summary, ok)
	}

	compacted, err := agent.CompactTranscript(sessionRef)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(compacted.Transcript)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(decoded), `"type":"user"`) ||
		!strings.Contains(string(decoded), `"tool_use"`) ||
		!strings.HasSuffix(string(decoded), "\n") {
		t.Fatalf("unexpected compact transcript:\n%s", decoded)
	}
}

func TestSessionReadWriteAndChunking(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repo)
	agent := New()
	sessionRef := agent.sidecarPath("roundtrip")
	nativeData := []byte(`{"v":1}` + "\n")

	if err := agent.WriteSession(protocol.AgentSessionJSON{
		SessionID:  "roundtrip",
		AgentName:  AgentName,
		SessionRef: sessionRef,
		NativeData: nativeData,
	}); err != nil {
		t.Fatal(err)
	}

	session, err := agent.ReadSession(&protocol.HookInputJSON{SessionID: "roundtrip", SessionRef: sessionRef})
	if err != nil {
		t.Fatal(err)
	}
	if string(session.NativeData) != string(nativeData) {
		t.Fatalf("native data mismatch: %q", session.NativeData)
	}

	chunks, err := agent.ChunkTranscript([]byte("abcdef"), 2)
	if err != nil {
		t.Fatal(err)
	}
	joined, err := agent.ReassembleTranscript(chunks)
	if err != nil {
		t.Fatal(err)
	}
	if string(joined) != "abcdef" {
		t.Fatalf("unexpected reassembled content %q", joined)
	}

	var decoded protocol.AgentSessionJSON
	data, _ := json.Marshal(session)
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
}
