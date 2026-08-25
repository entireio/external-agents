package goose

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/entireio/external-agents/agents/entire-agent-goose/internal/protocol"
)

func TestDetectUsesPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	if New().Detect().Present {
		t.Fatal("Detect().Present = true without goose on PATH")
	}
	if err := os.WriteFile(filepath.Join(dir, "goose"), []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if !New().Detect().Present {
		t.Fatal("Detect().Present = false with goose on PATH")
	}
}

func TestGetSessionID(t *testing.T) {
	agent := New()
	if got := agent.GetSessionID(nil); got != "" {
		t.Fatalf("GetSessionID(nil) = %q, want empty", got)
	}
	if got := agent.GetSessionID(&protocol.HookInputJSON{SessionID: "20260520_1"}); got != "20260520_1" {
		t.Fatalf("GetSessionID() = %q, want %q", got, "20260520_1")
	}
}

func TestFormatResumeCommand(t *testing.T) {
	tests := []struct{ id, want string }{
		{"20260520_1", "goose session --resume --session-id 20260520_1"},
		{"20260101_42", "goose session --resume --session-id 20260101_42"},
	}
	for _, test := range tests {
		if got := New().FormatResumeCommand(test.id); got != test.want {
			t.Errorf("FormatResumeCommand(%q) = %q, want %q", test.id, got, test.want)
		}
	}
}

func TestInfoDeclaresGoosePreviewAndCapabilities(t *testing.T) {
	info := New().Info()

	if info.ProtocolVersion != protocol.ProtocolVersion {
		t.Errorf("ProtocolVersion = %d, want %d", info.ProtocolVersion, protocol.ProtocolVersion)
	}
	if info.Name != "goose" {
		t.Errorf("Name = %q, want %q", info.Name, "goose")
	}
	if info.Type != "Goose" {
		t.Errorf("Type = %q, want %q", info.Type, "Goose")
	}
	if !info.IsPreview {
		t.Error("IsPreview = false, want true")
	}

	wantHooks := []string{
		HookNameSessionStart,
		HookNameUserPromptSubmit,
		HookNameStop,
		HookNameSessionEnd,
	}
	if len(info.HookNames) != len(wantHooks) {
		t.Fatalf("HookNames = %v, want %v", info.HookNames, wantHooks)
	}
	for i, name := range wantHooks {
		if info.HookNames[i] != name {
			t.Errorf("HookNames[%d] = %q, want %q", i, info.HookNames[i], name)
		}
	}

	want := protocol.DeclaredCapabilities{
		Hooks:              true,
		TranscriptAnalyzer: true,
		TranscriptPreparer: true,
		TokenCalculator:    true,
		CompactTranscript:  true,
	}
	if info.Capabilities != want {
		t.Errorf("Capabilities = %+v, want %+v", info.Capabilities, want)
	}
}
