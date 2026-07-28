package protocol_test

import (
	"reflect"
	"testing"

	"github.com/entireio/external-agents/agents/entire-agent-omp/internal/omp"
	"github.com/entireio/external-agents/agents/entire-agent-omp/internal/protocol"
)

func TestInfoResponseShape(t *testing.T) {
	info := omp.New().Info()
	if info.ProtocolVersion != protocol.ProtocolVersion {
		t.Fatalf("protocol_version = %d", info.ProtocolVersion)
	}
	if info.Name != "omp" {
		t.Fatalf("name = %q, want omp", info.Name)
	}
	if info.Type != "Oh My Pi" {
		t.Fatalf("type = %q, want Oh My Pi", info.Type)
	}
	if info.Description == "" {
		t.Fatal("description is empty")
	}
	if !info.IsPreview {
		t.Fatal("is_preview = false")
	}
	if !reflect.DeepEqual(info.ProtectedDirs, []string{".omp"}) {
		t.Fatalf("protected_dirs = %#v", info.ProtectedDirs)
	}
	if !reflect.DeepEqual(info.ProtectedFiles, []string{".omp/extensions/entire/index.ts"}) {
		t.Fatalf("protected_files = %#v", info.ProtectedFiles)
	}
	wantHooks := []string{"session_start", "agent_start", "agent_end", "session_shutdown"}
	if !reflect.DeepEqual(info.HookNames, wantHooks) {
		t.Fatalf("hook_names = %#v, want %#v", info.HookNames, wantHooks)
	}

	if !info.Capabilities.Hooks {
		t.Fatal("hooks = false")
	}
	if !info.Capabilities.TranscriptAnalyzer {
		t.Fatal("transcript_analyzer = false")
	}
	if !info.Capabilities.CompactTranscript {
		t.Fatal("compact_transcript = false")
	}
	if !info.Capabilities.UsesTerminal {
		t.Fatal("uses_terminal = false")
	}
	if info.Capabilities.TranscriptPreparer {
		t.Fatal("transcript_preparer = true")
	}
	if info.Capabilities.TokenCalculator {
		t.Fatal("token_calculator = true")
	}
	if info.Capabilities.TextGenerator {
		t.Fatal("text_generator = true")
	}
	if info.Capabilities.HookResponseWriter {
		t.Fatal("hook_response_writer = true")
	}
	if info.Capabilities.SubagentAwareExtractor {
		t.Fatal("subagent_aware_extractor = true")
	}
}
