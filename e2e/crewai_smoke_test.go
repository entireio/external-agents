//go:build e2e

package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/entireio/external-agents/e2e/entire"
	"github.com/entireio/external-agents/e2e/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLifecycle_CrewAISmoke(t *testing.T) {
	if os.Getenv("CREWAI_E2E") != "1" {
		t.Skip("set CREWAI_E2E=1 to run CrewAI smoke coverage")
	}

	binPath, ok := AgentBinaries["entire-agent-crewai"]
	require.True(t, ok, "entire-agent-crewai binary should be built")

	info := runCrewAIAgent(t, binPath, "info")
	var infoResp struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	require.NoError(t, json.Unmarshal(info, &infoResp))
	assert.Equal(t, "crewai", infoResp.Name)
	assert.Equal(t, "CrewAI", infoResp.Type)

	detect := runCrewAIAgent(t, binPath, "detect")
	assert.JSONEq(t, `{"present":true}`, string(detect))

	repo := t.TempDir()
	testutil.Git(t, repo, "init")
	testutil.Git(t, repo, "config", "user.name", "E2E Test")
	testutil.Git(t, repo, "config", "user.email", "e2e@test.local")
	testutil.Git(t, repo, "commit", "--allow-empty", "-m", "initial commit")

	entireDir := filepath.Join(repo, ".entire")
	require.NoError(t, os.MkdirAll(entireDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(entireDir, "settings.json"), []byte("{\"external_agents\": true}\n"), 0o644))

	entire.Enable(t, repo, "crewai")
}

func runCrewAIAgent(t *testing.T, binPath string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command(binPath, args...)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "entire-agent-crewai failed:\n%s", out)
	return out
}
