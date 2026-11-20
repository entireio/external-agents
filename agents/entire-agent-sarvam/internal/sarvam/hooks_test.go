package sarvam

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entireio/external-agents/agents/entire-agent-sarvam/internal/protocol"
)

// capturedPayload returns a hook payload captured from a live sarvam-code
// session (testdata/hook-payloads, gathered 2026-11-16 against 0.58.0).
func capturedPayload(t *testing.T, event string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "hook-payloads", event+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read captured payload %s: %v (rerun scripts/verify-sarvam.sh to regenerate)", event, err)
	}
	return data
}

func TestParseHook_NativePayloads(t *testing.T) {
	agent := New()
	cases := []struct {
		event      string
		wantType   int
		wantPrompt string
		wantResp   string
	}{
		{"SessionStart", 1, "", ""},
		{"UserPromptSubmit", 2, "Use the shell to run the command: echo hi > hi.txt and then reply done", ""},
		{"Stop", 3, "", "done"},
		{"PostCompact", 4, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.event, func(t *testing.T) {
			event, err := agent.ParseHook(tc.event, capturedPayload(t, tc.event))
			if err != nil {
				t.Fatal(err)
			}
			if event == nil {
				t.Fatal("expected a lifecycle event, got nil")
			}
			if event.Type != tc.wantType {
				t.Errorf("type = %d, want %d", event.Type, tc.wantType)
			}
			if event.SessionID != "01a1e2d3-8a2b-7dc2-b125-885a43ce43f5" {
				t.Errorf("session_id = %q", event.SessionID)
			}
			if event.SessionRef == "" || filepath.Base(event.SessionRef) != "rollout-2026-11-16T06-27-56-01a1e2d3-8a2b-7dc2-b125-885a43ce43f5.jsonl" {
				t.Errorf("session_ref = %q, want the native rollout path", event.SessionRef)
			}
			if event.Model != "glm-5.3" {
				t.Errorf("model = %q", event.Model)
			}
			if event.Timestamp == "" {
				t.Error("timestamp must be filled in (sarvam payloads carry none)")
			}
			if _, err := time.Parse(time.RFC3339, event.Timestamp); err != nil {
				t.Errorf("timestamp %q is not RFC3339: %v", event.Timestamp, err)
			}
			if tc.wantPrompt != "" && event.Prompt != tc.wantPrompt {
				t.Errorf("prompt = %q, want %q", event.Prompt, tc.wantPrompt)
			}
			if tc.wantResp != "" && event.ResponseMessage != tc.wantResp {
				t.Errorf("response_message = %q, want %q", event.ResponseMessage, tc.wantResp)
			}
		})
	}
}

func TestParseHook_ToolAndPermissionEventsAreNull(t *testing.T) {
	agent := New()
	for _, event := range []string{"PreToolUse", "PostToolUse", "PermissionRequest"} {
		t.Run(event, func(t *testing.T) {
			eventJSON, err := agent.ParseHook(event, capturedPayload(t, event))
			if err != nil {
				t.Fatal(err)
			}
			if eventJSON != nil {
				t.Fatalf("expected nil event for %s, got %+v", event, eventJSON)
			}
		})
	}
}

func TestParseHook_PreCompactIsANoOp(t *testing.T) {
	// PreCompact is no longer installed: compaction is reported once, from
	// PostCompact. A config carrying the legacy entry may still fire the
	// hook, and it must degrade to a non-lifecycle no-op rather than emit a
	// second Compaction event.
	agent := New()
	event, err := agent.ParseHook("pre-compact", capturedPayload(t, "PreCompact"))
	if err != nil {
		t.Fatal(err)
	}
	if event != nil {
		t.Fatalf("legacy pre-compact must not emit a lifecycle event, got %+v", event)
	}

	// The kebab-case flag alone must degrade the same way.
	event, err = agent.ParseHook("pre-compact", []byte(`{"session_id":"s1","hook_event_name":"PreCompact"}`))
	if err != nil {
		t.Fatal(err)
	}
	if event != nil {
		t.Fatalf("legacy pre-compact without a payload event name must not emit, got %+v", event)
	}
}

func TestParseHook_ProtocolHookInputShape(t *testing.T) {
	agent := New()
	input, err := json.Marshal(protocol.HookInputJSON{
		HookType:   "user-prompt-submit",
		SessionID:  "test-session-abc",
		SessionRef: "/tmp/transcript.jsonl",
		Timestamp:  "2026-01-01T00:00:00Z",
		UserPrompt: "test prompt",
	})
	if err != nil {
		t.Fatal(err)
	}

	event, err := agent.ParseHook("user-prompt-submit", input)
	if err != nil {
		t.Fatal(err)
	}
	if event == nil || event.Type != 2 {
		t.Fatalf("expected TurnStart (type 2), got %+v", event)
	}
	if event.SessionID != "test-session-abc" {
		t.Errorf("session_id = %q", event.SessionID)
	}
	if event.SessionRef != "/tmp/transcript.jsonl" {
		t.Errorf("session_ref = %q", event.SessionRef)
	}
	if event.Prompt != "test prompt" {
		t.Errorf("prompt = %q", event.Prompt)
	}
}

func TestParseHook_EmptyAndInvalidInput(t *testing.T) {
	agent := New()

	event, err := agent.ParseHook("session-start", nil)
	if err != nil || event != nil {
		t.Fatalf("empty input: got (%+v, %v), want (nil, nil)", event, err)
	}

	if _, err := agent.ParseHook("session-start", []byte(`{"session_id":`)); err == nil {
		t.Fatal("invalid JSON must error")
	}
}

func TestParseHook_EmptyPromptIsNull(t *testing.T) {
	agent := New()
	// Sarvam never emits an empty prompt, but the protocol allows it; empty
	// prompts are not lifecycle events.
	stripped := []byte(`{"session_id":"s1","hook_event_name":"UserPromptSubmit"}`)
	event, err := agent.ParseHook("user-prompt-submit", stripped)
	if err != nil {
		t.Fatal(err)
	}
	if event != nil {
		t.Fatalf("empty prompt should produce nil event, got %+v", event)
	}
}

func TestHookCommand_ResolvesEntireViaPath(t *testing.T) {
	command := hookCommand("session-start")
	want := "sh -c 'if command -v entire >/dev/null 2>&1; then entire hooks sarvam session-start >/dev/null 2>&1 || true; fi'"
	if command != want {
		t.Fatalf("hookCommand = %q, want %q", command, want)
	}
	if !strings.Contains(command, "entire hooks sarvam session-start") {
		t.Fatal("command must carry the entire marker for install detection")
	}
}
