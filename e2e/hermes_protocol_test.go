//go:build e2e

package e2e

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLifecycle_HermesPrepareTranscriptProtocol(t *testing.T) {
	if env := os.Getenv("E2E_AGENT"); env != "" && env != "hermes" {
		t.Skip("hermes-specific e2e test")
	}

	binPath, ok := AgentBinaries["entire-agent-hermes"]
	require.True(t, ok, "entire-agent-hermes binary should be built")

	home := t.TempDir()
	repoRoot := t.TempDir()
	sessionRef := filepath.Join(home, "entire", "transcripts", "repo", "session.jsonl")
	require.NoError(t, os.MkdirAll(filepath.Dir(sessionRef), 0o700))
	raw := strings.Join([]string{
		`{"v":1,"type":"user","timestamp":"2026-08-06T12:00:01Z","content":"Create hello.txt"}`,
		`{"v":1,"type":"tool","timestamp":"2026-08-06T12:00:02Z","name":"write_file","status":"ok","modified_files":["hello.txt"]}`,
		`{"v":1,"type":"assistant","timestamp":"2026-08-06T12:00:03Z","content":"Created hello.txt"}`,
	}, "\n") + "\n"
	require.NoError(t, os.WriteFile(sessionRef, []byte(raw), 0o600))
	env := append(os.Environ(), "ENTIRE_REPO_ROOT="+repoRoot, "HERMES_HOME="+home)

	runAgent(t, binPath, env, nil, "prepare-transcript", "--session-ref", sessionRef)
	prepared := readFile(t, sessionRef)
	assert.NotEqual(t, raw, string(prepared))
	assert.Contains(t, string(prepared), `"message":{"content":"Create hello.txt"}`)
	assert.Contains(t, string(prepared), `"type":"tool_use"`)
	assert.Contains(t, string(prepared), `"type":"text","text":"Created hello.txt"`)

	readTranscript := runAgent(t, binPath, env, nil, "read-transcript", "--session-ref", sessionRef)
	assert.Equal(t, prepared, readTranscript)

	prompts := runAgent(t, binPath, env, nil, "extract-prompts", "--session-ref", sessionRef, "--offset", "0")
	assert.JSONEq(t, `{"prompts":["Create hello.txt"]}`, string(prompts))

	modified := runAgent(t, binPath, env, nil, "extract-modified-files", "--path", sessionRef, "--offset", "0")
	assert.JSONEq(t, `{"files":["hello.txt"],"current_position":3}`, string(modified))

	compact := runAgent(t, binPath, env, nil, "compact-transcript", "--session-ref", sessionRef)
	var compactResp struct {
		Transcript string `json:"transcript"`
	}
	require.NoError(t, json.Unmarshal(compact, &compactResp))
	decoded, err := base64.StdEncoding.DecodeString(compactResp.Transcript)
	require.NoError(t, err)
	assert.Contains(t, string(decoded), `"agent":"hermes"`)
	assert.Contains(t, string(decoded), `"type":"user"`)
	assert.Contains(t, string(decoded), `"type":"tool_use"`)
	assert.Contains(t, string(decoded), `"type":"text","text":"Created hello.txt"`)
}
