package grok

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entireio/external-agents/agents/entire-agent-grok/internal/protocol"
)

func TestDetectUsesGrokBinary(t *testing.T) {
	agent := New()
	agent.LookPath = func(name string) (string, error) {
		if name != grokBinary {
			t.Fatalf("unexpected binary lookup: %s", name)
		}
		return "/usr/local/bin/grok", nil
	}
	if !agent.Detect().Present {
		t.Fatal("expected grok to be detected")
	}

	agent.LookPath = func(string) (string, error) {
		return "", errors.New("not found")
	}
	if agent.Detect().Present {
		t.Fatal("expected grok to be absent")
	}
}

func testGrokHome(t *testing.T) string {
	home := filepath.Join(t.TempDir(), "grok-home")
	t.Setenv("GROK_HOME", home)
	return home
}

func writeNativeTranscript(t *testing.T, repo, sessionID, content string) string {
	testGrokHome(t)
	path := nativeTranscriptPath(repo, sessionID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestGetSessionDirUsesNativeGrokSessions(t *testing.T) {
	repo := t.TempDir()
	testGrokHome(t)
	agent := New()

	sessionDir, err := agent.GetSessionDir(repo)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sessionDir, filepath.Join("sessions", encodeRepoCWD(repo))) {
		t.Fatalf("expected native grok session dir, got %q", sessionDir)
	}
}

func TestInstallHooksIdempotentAndUninstallPreservesUserSettings(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repo)

	hooksPath := hooksFilePath(repo)
	if err := os.MkdirAll(filepath.Dir(hooksPath), 0o700); err != nil {
		t.Fatal(err)
	}
	initial := `{
  "hooks": {
    "SessionStart": [
      {
        "hooks": [
          {"type": "command", "name": "custom", "command": "echo custom"}
        ]
      }
    ],
    "PostToolUse": [
      {
        "matcher": "^WriteFile$",
        "sequential": true,
        "scope": "project",
        "hooks": [
          {
            "type": "http",
            "name": "audit",
            "url": "https://audit.example.com/tool",
            "headers": {"Authorization": "Bearer ${AUDIT_TOKEN}"},
            "allowedEnvVars": ["AUDIT_TOKEN"],
            "once": true,
            "timeout": 10
          },
          {
            "type": "command",
            "name": "custom-command",
            "command": "echo custom",
            "env": {"CUSTOM": "1"},
            "statusMessage": "checking"
          }
        ]
      }
    ]
  }
}
`
	if err := os.WriteFile(hooksPath, []byte(initial), 0o600); err != nil {
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

	data, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"custom"`) {
		t.Fatalf("custom hook was not preserved:\n%s", data)
	}
	for _, want := range []string{
		`"url": "https://audit.example.com/tool"`,
		`"allowedEnvVars": [`,
		`"once": true`,
		`"scope": "project"`,
		`"env": {`,
		`"statusMessage": "checking"`,
	} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("Grok hook field %s was not preserved:\n%s", want, data)
		}
	}

	if err := agent.UninstallHooks(); err != nil {
		t.Fatal(err)
	}
	if agent.AreHooksInstalled() {
		t.Fatal("expected hooks uninstalled")
	}
	data, err = os.ReadFile(hooksPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"custom"`) {
		t.Fatalf("custom hook was removed:\n%s", data)
	}
	if strings.Contains(string(data), "entire hooks grok") {
		t.Fatalf("Entire hook command still present:\n%s", data)
	}
	for _, want := range []string{
		`"url": "https://audit.example.com/tool"`,
		`"allowedEnvVars": [`,
		`"once": true`,
		`"scope": "project"`,
		`"env": {`,
		`"statusMessage": "checking"`,
	} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("Grok hook field %s was not preserved after uninstall:\n%s", want, data)
		}
	}
}

func TestParseHookLifecycleEvents(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repo)

	agent := New()
	input := []byte(`{
  "session_id": "grok-test-session",
  "transcript_path": "/tmp/grok-native.jsonl",
  "cwd": "/repo",
  "timestamp": "2026-05-20T12:00:00Z",
  "prompt": "write a file",
  "model": "qwen3-coder-plus",
  "last_assistant_message": "done",
  "error": "server_error",
  "error_type": "api",
  "error_details": "api failed",
  "agent_id": "agent-1",
  "agent_type": "Explorer",
  "agent_transcript_path": "/tmp/grok-agent.jsonl",
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
		{HookNamePreToolUse, 0, true},
		{HookNameStop, 3, false},
		{HookNameStopFailure, 3, false},
		{HookNameSessionEnd, 5, false},
		{HookNamePreCompact, 4, false},
		{HookNamePostCompact, 4, false},
		{HookNamePostToolUse, 0, true},
		{HookNamePostToolUseFailure, 0, true},
		{HookNameNotification, 0, true},
		{HookNamePermissionRequest, 0, true},
		{HookNameSubagentStart, 0, true},
		{HookNameSubagentStop, 0, true},
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
			if event.SessionID != "grok-test-session" {
				t.Fatalf("unexpected session id %q", event.SessionID)
			}
			if !strings.HasSuffix(event.SessionRef, "grok-test-session/chat_history.jsonl") {
				t.Fatalf("unexpected session ref %q", event.SessionRef)
			}
			if event.Metadata["native_transcript_path"] != "/tmp/grok-native.jsonl" {
				t.Fatalf("native transcript path missing from metadata: %#v", event.Metadata)
			}
			if event.Metadata["agent_type"] != "Explorer" {
				t.Fatalf("agent metadata missing: %#v", event.Metadata)
			}
		})
	}
}

func TestParseHookIgnoresEmptyUserPromptSubmit(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repo)

	agent := New()
	event, err := agent.ParseHook(HookNameUserPromptSubmit, []byte(`{
  "session_id": "grok-empty-prompt",
  "timestamp": "2026-05-20T12:00:00Z"
}`))
	if err != nil {
		t.Fatal(err)
	}
	if event != nil {
		t.Fatalf("expected empty prompt submit to be ignored, got %#v", event)
	}

	transcript := writeNativeTranscript(t, repo, "grok-empty-prompt", `{"type":"user","content":[{"type":"text","text":"<user_info>ignored</user_info>"}]}`+"\n")
	prompts, err := agent.ExtractPrompts(transcript, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 0 {
		t.Fatalf("unexpected prompts: %#v", prompts)
	}
}

func TestParseHookNormalizesGrokPayloadAliases(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repo)
	agent := New()

	sessionRef := writeNativeTranscript(t, repo, "alias-test", strings.Join([]string{
		`{"type":"user","content":[{"type":"text","text":"<user_query>\nCreate aliased files\n</user_query>"}]}`,
		`{"type":"assistant","content":"Done","tool_calls":[{"id":"tool-1","name":"Write","arguments":"{\"path\":\"src/alias.txt\"}"}]}`,
		`{"type":"assistant","content":"Done"}`,
	}, "\n")+"\n")

	prompts, err := agent.ExtractPrompts(sessionRef, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 1 || prompts[0] != "Create aliased files" {
		t.Fatalf("unexpected prompts: %#v", prompts)
	}

	files, _, err := agent.ExtractModifiedFiles(sessionRef, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != "src/alias.txt" {
		t.Fatalf("unexpected modified files: %#v", files)
	}
}

func TestExtractModifiedFilesNestedAndShellInputs(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repo)
	agent := New()

	sessionRef := writeNativeTranscript(t, repo, "nested-test", strings.Join([]string{
		`{"type":"assistant","content":"","tool_calls":[{"id":"tool-1","name":"search_replace","arguments":"{\"path\":\"src/nested.txt\"}"}]}`,
		`{"type":"assistant","content":"","tool_calls":[{"id":"tool-2","name":"Shell","arguments":"{\"command\":\"printf hi > shell.txt && tee -a log.txt\"}"}]}`,
	}, "\n")+"\n")

	files, _, err := agent.ExtractModifiedFiles(sessionRef, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"log.txt", "shell.txt", "src/nested.txt"}
	if strings.Join(files, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected modified files: got %#v want %#v", files, want)
	}
}

func TestTranscriptAnalysisAndCompactTranscript(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repo)
	agent := New()

	sessionRef := writeNativeTranscript(t, repo, "grok-test", strings.Join([]string{
		`{"type":"user","content":[{"type":"text","text":"<user_query>\nCreate hello.txt\n</user_query>"}]}`,
		`{"type":"assistant","content":"","tool_calls":[{"id":"tool-1","name":"Write","arguments":"{\"path\":\"hello.txt\"}"}]}`,
		`{"type":"assistant","content":"Created hello.txt"}`,
	}, "\n")+"\n")
	if _, err := agent.ParseHook(HookNameSessionStart, []byte(`{"session_id":"grok-test","cwd":"`+repo+`"}`)); err != nil {
		t.Fatal(err)
	}
	markerPath := filepath.Join(repo, ".entire", "tmp", "grok-test.json")
	if _, err := os.Stat(markerPath); err != nil {
		t.Fatalf("expected session marker %s: %v", markerPath, err)
	}

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
	sessionRef := writeNativeTranscript(t, repo, "roundtrip", `{"type":"assistant","content":"ok"}`+"\n")
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
