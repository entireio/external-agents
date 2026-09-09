//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/entireio/external-agents/e2e/entire"
	"github.com/entireio/external-agents/e2e/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLifecycle_CrewAISmoke(t *testing.T) {
	if selected := os.Getenv("E2E_AGENT"); selected != "" && selected != "crewai" {
		t.Skip("CrewAI is not selected")
	}
	if os.Getenv("CREWAI_E2E") != "1" {
		t.Skip("set CREWAI_E2E=1 to run CrewAI smoke coverage")
	}

	binPath, ok := AgentBinaries["entire-agent-crewai"]
	require.True(t, ok, "entire-agent-crewai binary should be built")

	repo := t.TempDir()
	testutil.Git(t, repo, "init")
	testutil.Git(t, repo, "config", "user.name", "E2E Test")
	testutil.Git(t, repo, "config", "user.email", "e2e@test.local")
	testutil.Git(t, repo, "commit", "--allow-empty", "-m", "initial commit")

	entireDir := filepath.Join(repo, ".entire")
	require.NoError(t, os.MkdirAll(entireDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(entireDir, "settings.json"), []byte("{\"external_agents\": true}\n"), 0o644))

	entire.Enable(t, repo, "crewai")

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, binPath, "__e2e_run_fixture")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), "CREWAI_TELEMETRY_DISABLED=true", "OTEL_SDK_DISABLED=true")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "CrewAI event fixture failed:\n%s", out)
	var session struct {
		ID  string `json:"session_id"`
		Ref string `json:"session_ref"`
	}
	require.NoError(t, json.Unmarshal(out, &session), "fixture output: %s", out)
	require.NotEmpty(t, session.ID)
	require.NotEmpty(t, session.Ref)

	transcript, err := os.ReadFile(session.Ref)
	require.NoError(t, err)
	assert.Contains(t, string(transcript), "Create crewai-output.txt")
	assert.Contains(t, string(transcript), "write_file")
	assert.Contains(t, string(transcript), "fixture complete")
	content, err := os.ReadFile(filepath.Join(repo, "crewai-output.txt"))
	require.NoError(t, err)
	assert.Equal(t, "hello from CrewAI\n", string(content))

	testutil.Git(t, repo, "add", "crewai-output.txt")
	testutil.Git(t, repo, "commit", "-m", "record CrewAI fixture output")
	cpID := testutil.AssertHasCheckpointTrailer(t, repo, "HEAD")
	testutil.AssertCheckpointExists(t, repo, cpID)
	testutil.ValidateCheckpointDeep(t, repo, testutil.DeepCheckpointValidation{
		CheckpointID:              cpID,
		FilesTouched:              []string{"crewai-output.txt"},
		ExpectedTranscriptContent: []string{"write_file", "hello from CrewAI"},
	})
}
