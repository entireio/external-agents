package windsurf

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/entireio/external-agents/agents/entire-agent-windsurf/internal/protocol"
)

func TestInfo(t *testing.T) {
	info := New().Info()
	if info.ProtocolVersion != protocol.ProtocolVersion || info.Name != AgentName || info.Type != "Windsurf Cascade" || !info.IsPreview {
		t.Fatalf("unexpected info: %#v", info)
	}
	if !reflect.DeepEqual(info.ProtectedDirs, []string{".windsurf"}) || info.Capabilities.Hooks || info.Capabilities.TranscriptAnalyzer {
		t.Fatalf("unexpected core-only info: %#v", info)
	}
}

func TestDetect(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repo)
	t.Setenv("PATH", t.TempDir())
	if New().Detect().Present { t.Fatal("Detect().Present = true without executable or configuration") }
	if err := os.Mkdir(filepath.Join(repo, ".windsurf"), 0o755); err != nil { t.Fatal(err) }
	if !New().Detect().Present { t.Fatal("Detect().Present = false with workspace configuration") }
}

func TestSessionIdentityUsesTrajectoryID(t *testing.T) {
	agent := New()
	input := &protocol.HookInputJSON{RawData: map[string]interface{}{"trajectory_id": "trajectory-1"}}
	if got := agent.GetSessionID(input); got != "trajectory-1" { t.Fatalf("GetSessionID() = %q", got) }
	input.SessionID = "already-normalized"
	if got := agent.GetSessionID(input); got != "already-normalized" { t.Fatalf("GetSessionID() = %q", got) }
	if got := agent.ResolveSessionFile("sessions", "../unsafe"); got != "" { t.Fatalf("unsafe session file = %q", got) }
}

func TestNormalizeEvent(t *testing.T) {
	event := NormalizeEvent(2, LifecycleEvent{Name: "pre_user_prompt", TrajectoryID: "trajectory-1", ExecutionID: "execution-1", Prompt: "hello", Timestamp: "2026-01-01T00:00:00Z"})
	if event == nil || event.Type != 2 || event.SessionID != "trajectory-1" || event.Metadata["execution_id"] != "execution-1" { t.Fatalf("event = %#v", event) }
	if NormalizeEvent(2, LifecycleEvent{}) != nil { t.Fatal("empty trajectory produced event") }
}
