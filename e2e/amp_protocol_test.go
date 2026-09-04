//go:build e2e

package e2e

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const ampE2EThreadJSON = `{"v":1,"id":"T-e2e-amp","created":1778155200000,"updatedAt":"2026-05-07T12:00:05Z","messages":[{"role":"user","messageId":1,"meta":{"sentAt":1778155200000},"content":[{"type":"text","text":"Create hello.txt"}]},{"role":"assistant","messageId":"a1","meta":{"sentAt":1778155201000},"usage":{"model":"claude","timestamp":"2026-05-07T12:00:01Z","inputTokens":100,"outputTokens":25,"cacheReadInputTokens":7,"cacheCreationInputTokens":3},"content":[{"type":"tool_use","id":"tool1","name":"edit_file","input":{"path":"hello.txt"}},{"type":"text","text":"Created hello.txt"}]},{"role":"user","messageId":2,"meta":{"sentAt":1778155202000},"content":[{"type":"tool_result","toolUseID":"tool1","run":{"status":"done","trackFiles":["hello.txt"],"result":{"output":"ok"}}}]}]}`

func TestLifecycle_AmpPrepareTranscriptProtocol(t *testing.T) {
	if env := os.Getenv("E2E_AGENT"); env != "" && env != "amp" {
		t.Skip("amp-specific e2e test")
	}

	binPath, ok := AgentBinaries["entire-agent-amp"]
	require.True(t, ok, "entire-agent-amp binary should be built")

	repoRoot := t.TempDir()
	sessionRef := filepath.Join(repoRoot, ".entire", "tmp", "amp", "T-e2e-amp.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(sessionRef), 0o755))
	require.NoError(t, os.WriteFile(sessionRef, []byte("{\"type\":\"agent.start\",\"thread_id\":\"T-e2e-amp\",\"message\":\"Create hello.txt\"}\n"), 0o600))

	fakeBinDir := t.TempDir()
	writeFakeAmp(t, fakeBinDir)
	env := append(os.Environ(),
		"ENTIRE_REPO_ROOT="+repoRoot,
		"PATH="+fakeBinDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)

	runAgent(t, binPath, env, nil, "prepare-transcript", "--session-ref", sessionRef)
	prepared := readFile(t, sessionRef)

	// Entire scopes an external agent's transcript by LINE offset before
	// compacting it, so the stored transcript must be one message per line —
	// not the single JSON document `amp threads export` emits.
	assertJSONLTranscript(t, prepared, 3)

	readTranscript := runAgent(t, binPath, env, nil, "read-transcript", "--session-ref", sessionRef)
	assert.Equal(t, string(prepared), string(readTranscript))

	readSession := runAgent(t, binPath, env, []byte(`{"hook_type":"stop","session_ref":"`+sessionRef+`"}`), "read-session")
	var session struct {
		SessionID     string   `json:"session_id"`
		AgentName     string   `json:"agent_name"`
		ModifiedFiles []string `json:"modified_files"`
	}
	require.NoError(t, json.Unmarshal(readSession, &session))
	assert.Equal(t, "T-e2e-amp", session.SessionID)
	assert.Equal(t, "amp", session.AgentName)
	assert.Equal(t, []string{"hello.txt"}, session.ModifiedFiles)

	prompts := runAgent(t, binPath, env, nil, "extract-prompts", "--session-ref", sessionRef, "--offset", "0")
	assert.JSONEq(t, `{"prompts":["Create hello.txt"]}`, string(prompts))

	summary := runAgent(t, binPath, env, nil, "extract-summary", "--session-ref", sessionRef)
	assert.JSONEq(t, `{"summary":"Created hello.txt","has_summary":true}`, string(summary))

	modified := runAgent(t, binPath, env, nil, "extract-modified-files", "--path", sessionRef, "--offset", "0")
	var modifiedResp struct {
		Files           []string `json:"files"`
		CurrentPosition int      `json:"current_position"`
	}
	require.NoError(t, json.Unmarshal(modified, &modifiedResp))
	assert.Equal(t, []string{"hello.txt"}, modifiedResp.Files)
	assert.Equal(t, 3, modifiedResp.CurrentPosition)

	tokens := runAgent(t, binPath, env, prepared, "calculate-tokens", "--offset", "0")
	assert.JSONEq(t, `{"input_tokens":100,"cache_creation_tokens":3,"cache_read_tokens":7,"output_tokens":25,"api_call_count":1}`, string(tokens))

	// A mid-session checkpoint hands compact-transcript a line-scoped slice
	// with no thread header. It must still be valid and still compact.
	scopedRef := filepath.Join(repoRoot, ".entire", "tmp", "amp", "T-e2e-amp-scoped.json")
	require.NoError(t, os.WriteFile(scopedRef, sliceFromLine(prepared, 1), 0o600))
	assertJSONLTranscript(t, readFile(t, scopedRef), 2)
	scopedCompact := runAgent(t, binPath, env, nil, "compact-transcript", "--session-ref", scopedRef)
	var scopedResp struct {
		Transcript string `json:"transcript"`
	}
	require.NoError(t, json.Unmarshal(scopedCompact, &scopedResp))
	scopedDecoded, err := base64.StdEncoding.DecodeString(scopedResp.Transcript)
	require.NoError(t, err)
	assert.NotEmpty(t, scopedDecoded)
	assert.Contains(t, string(scopedDecoded), `"input_tokens":100`)
	assert.NotContains(t, string(scopedDecoded), "Create hello.txt")

	compact := runAgent(t, binPath, env, nil, "compact-transcript", "--session-ref", sessionRef)
	var compactResp struct {
		Transcript string `json:"transcript"`
	}
	require.NoError(t, json.Unmarshal(compact, &compactResp))
	decoded, decodeErr := base64.StdEncoding.DecodeString(compactResp.Transcript)
	require.NoError(t, decodeErr)
	assert.Contains(t, string(decoded), `"agent":"amp"`)
	assert.Contains(t, string(decoded), `"type":"assistant"`)
	assert.Contains(t, string(decoded), `"input_tokens":100`)
	assert.Contains(t, string(decoded), `"result":{"output":"ok","status":"success"}`)
}

// assertJSONLTranscript asserts that data is exactly wantLines newline-
// terminated lines, each an independently parseable Amp message.
func assertJSONLTranscript(t *testing.T, data []byte, wantLines int) {
	t.Helper()
	require.Equal(t, wantLines, bytes.Count(data, []byte{'\n'}), "transcript is not one message per line: %s", data)
	for _, line := range bytes.Split(bytes.TrimRight(data, "\n"), []byte{'\n'}) {
		var msg struct {
			ThreadID string            `json:"threadID"`
			Role     string            `json:"role"`
			Content  []json.RawMessage `json:"content"`
		}
		require.NoError(t, json.Unmarshal(line, &msg), "line is not valid JSON: %s", line)
		require.Equal(t, "T-e2e-amp", msg.ThreadID, "line lost its thread id: %s", line)
		require.NotEmpty(t, msg.Role, "line has no role: %s", line)
	}
}

// sliceFromLine mirrors Entire's transcript.SliceFromLine
// (cmd/entire/cli/transcript/parse.go:114), the scoping every non-built-in
// agent transcript goes through.
func sliceFromLine(content []byte, startLine int) []byte {
	if len(content) == 0 || startLine <= 0 {
		return content
	}
	count, offset := 0, 0
	for i, b := range content {
		if b == '\n' {
			count++
			if count == startLine {
				offset = i + 1
				break
			}
		}
	}
	if count < startLine || offset >= len(content) {
		return nil
	}
	return content[offset:]
}

func writeFakeAmp(t *testing.T, dir string) {
	t.Helper()
	path := filepath.Join(dir, "amp")
	script := `#!/usr/bin/env bash
set -euo pipefail
if [[ "$1" == "threads" && "$2" == "export" && "$3" == "T-e2e-amp" ]]; then
  printf '%s' '` + ampE2EThreadJSON + `'
  exit 0
fi
echo "unexpected amp args: $*" >&2
exit 2
`
	require.NoError(t, os.WriteFile(path, []byte(script), 0o755))
}

func runAgent(t *testing.T, binPath string, env []string, stdin []byte, args ...string) []byte {
	t.Helper()
	cmd := exec.Command(binPath, args...)
	cmd.Env = env
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "entire-agent-amp %s failed:\n%s", strings.Join(args, " "), out)
	return out
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return data
}
