package testutil

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// DeepCheckpointValidation contains expected values for comprehensive checkpoint validation.
type DeepCheckpointValidation struct {
	CheckpointID              string
	Strategy                  string
	FilesTouched              []string
	ExpectedPrompts           []string
	ExpectedTranscriptContent []string
}

var hexIDPattern = regexp.MustCompile(`^[0-9a-f]{12}$`)

// AssertFileExists asserts that at least one file matches the glob pattern
// relative to dir.
func AssertFileExists(t *testing.T, dir string, glob string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, glob))
	require.NoError(t, err)
	assert.NotEmpty(t, matches, "expected files matching %s in %s", glob, dir)
}

// WaitForFileExists polls until at least one file matches the glob pattern
// relative to dir, or fails the test after timeout.
func WaitForFileExists(t *testing.T, dir string, glob string, timeout time.Duration) {
	t.Helper()
	pattern := filepath.Join(dir, glob)
	deadline := time.Now().Add(timeout)
	for {
		matches, err := filepath.Glob(pattern)
		require.NoError(t, err)
		if len(matches) > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected files matching %s in %s within %s", glob, dir, timeout)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// AssertNewCommits polls until at least `atLeast` new commits exist since setup,
// or fails after 20 seconds.
func AssertNewCommits(t *testing.T, s *RepoState, atLeast int) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		out := GitOutput(t, s.Dir, "log", "--oneline", s.HeadBefore+"..HEAD")
		var lines []string
		if out != "" {
			lines = strings.Split(strings.TrimSpace(out), "\n")
		}
		if len(lines) >= atLeast {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected at least %d new commit(s), got %d after 20s", atLeast, len(lines))
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// WaitForCheckpoint polls until the checkpoint branch advances from its
// initial state, or fails the test after timeout.
func WaitForCheckpoint(t *testing.T, s *RepoState, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		after := GitOutput(t, s.Dir, "rev-parse", "entire/checkpoints/v1")
		if after != s.CheckpointBefore {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("checkpoint branch did not advance within %s", timeout)
}

// WaitForCheckpointAdvanceFrom polls until the checkpoint branch advances from
// the given ref, or fails the test after timeout.
func WaitForCheckpointAdvanceFrom(t *testing.T, dir string, fromRef string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		after := GitOutput(t, dir, "rev-parse", "entire/checkpoints/v1")
		if after != fromRef {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("checkpoint branch did not advance from %s within %s", fromRef[:8], timeout)
}

// AssertCheckpointAdvanced asserts the checkpoint branch moved forward.
func AssertCheckpointAdvanced(t *testing.T, s *RepoState) {
	t.Helper()
	after := GitOutput(t, s.Dir, "rev-parse", "entire/checkpoints/v1")
	assert.NotEqual(t, s.CheckpointBefore, after, "checkpoint branch did not advance")
}

// AssertCheckpointNotAdvanced asserts the checkpoint branch has NOT moved.
func AssertCheckpointNotAdvanced(t *testing.T, s *RepoState) {
	t.Helper()
	after := GitOutput(t, s.Dir, "rev-parse", "entire/checkpoints/v1")
	assert.Equal(t, s.CheckpointBefore, after, "checkpoint branch advanced unexpectedly")
}

// AssertCheckpointIDFormat asserts the checkpoint ID is 12 lowercase hex chars.
func AssertCheckpointIDFormat(t *testing.T, checkpointID string) {
	t.Helper()
	assert.Regexp(t, hexIDPattern, checkpointID,
		"checkpoint ID %q should be 12 lowercase hex chars", checkpointID)
}

// AssertHasCheckpointTrailer asserts the commit has an Entire-Checkpoint trailer,
// validates its format, and returns its value.
func AssertHasCheckpointTrailer(t *testing.T, dir string, ref string) string {
	t.Helper()
	trailer := GetCheckpointTrailer(t, dir, ref)
	require.NotEmpty(t, trailer, "no Entire-Checkpoint trailer on %s", ref)
	AssertCheckpointIDFormat(t, trailer)
	return trailer
}

// AssertNoCheckpointTrailer asserts the commit does NOT have an Entire-Checkpoint trailer.
func AssertNoCheckpointTrailer(t *testing.T, dir string, ref string) {
	t.Helper()
	trailer := GetCheckpointTrailer(t, dir, ref)
	assert.Empty(t, trailer, "expected no Entire-Checkpoint trailer on %s, got %q", ref, trailer)
}

// AssertCheckpointExists asserts that the checkpoint ID is mentioned on
// the checkpoint branch and that its metadata.json exists in the tree.
func AssertCheckpointExists(t *testing.T, dir string, checkpointID string) {
	t.Helper()
	out := GitOutput(t, dir, "log", "entire/checkpoints/v1", "--grep="+checkpointID, "--oneline")
	assert.NotEmpty(t, out, "checkpoint %s not found on checkpoint branch", checkpointID)

	path := CheckpointPath(checkpointID) + "/metadata.json"
	blob := "entire/checkpoints/v1:" + path
	raw := gitOutputSafe(dir, "show", blob)
	assert.NotEmpty(t, raw,
		"checkpoint %s metadata not found at %s", checkpointID, path)
}

// AssertCommitLinkedToCheckpoint asserts the trailer exists AND the
// checkpoint data exists on the checkpoint branch.
func AssertCommitLinkedToCheckpoint(t *testing.T, dir string, ref string) {
	t.Helper()
	trailer := AssertHasCheckpointTrailer(t, dir, ref)
	AssertCheckpointExists(t, dir, trailer)
}

// AssertCheckpointHasSingleSession asserts checkpoint metadata has exactly one session.
func AssertCheckpointHasSingleSession(t *testing.T, dir string, checkpointID string) {
	t.Helper()
	meta := ReadCheckpointMetadata(t, dir, checkpointID)
	assert.Len(t, meta.Sessions, 1,
		"expected 1 session in checkpoint %s, got %d", checkpointID, len(meta.Sessions))
}

// AssertCheckpointMetadataComplete asserts essential fields in checkpoint metadata are populated.
func AssertCheckpointMetadataComplete(t *testing.T, dir string, checkpointID string) {
	t.Helper()
	meta := ReadCheckpointMetadata(t, dir, checkpointID)
	assert.NotEmpty(t, meta.CLIVersion, "checkpoint %s: cli_version should be set", checkpointID)
	assert.NotEmpty(t, meta.Strategy, "checkpoint %s: strategy should be set", checkpointID)
	assert.NotEmpty(t, meta.Sessions, "checkpoint %s: should have at least 1 session", checkpointID)
	assert.Equal(t, checkpointID, meta.CheckpointID,
		"checkpoint metadata ID should match expected")
}

// AssertCheckpointFilesTouched asserts the checkpoint metadata lists exactly
// the expected files in files_touched (order-independent).
func AssertCheckpointFilesTouched(t *testing.T, dir string, checkpointID string, expected []string) {
	t.Helper()
	meta := ReadCheckpointMetadata(t, dir, checkpointID)
	assert.ElementsMatch(t, expected, meta.FilesTouched,
		"checkpoint %s: files_touched mismatch", checkpointID)
}

// shadowBranches returns all shadow branches (entire/*) excluding entire/checkpoints/*.
func shadowBranches(t *testing.T, dir string) []string {
	t.Helper()
	branches := GitOutput(t, dir, "for-each-ref", "--format=%(refname:short)", "refs/heads/entire/")
	var shadow []string
	for _, b := range strings.Split(branches, "\n") {
		b = strings.TrimSpace(b)
		if b == "" || strings.HasPrefix(b, "entire/checkpoints") {
			continue
		}
		shadow = append(shadow, b)
	}
	return shadow
}

// AssertNoShadowBranches asserts that no shadow branches (entire/*) remain,
// excluding entire/checkpoints/*.
func AssertNoShadowBranches(t *testing.T, dir string) {
	t.Helper()
	shadow := shadowBranches(t, dir)
	assert.Empty(t, shadow,
		"shadow branches should be cleaned up after commit, found: %v", shadow)
}

// AssertHasShadowBranches asserts that at least one shadow branch exists.
func AssertHasShadowBranches(t *testing.T, dir string) {
	t.Helper()
	shadow := shadowBranches(t, dir)
	assert.NotEmpty(t, shadow,
		"expected at least one shadow branch to persist, but none found")
}

// WaitForSessionIdle polls the session state files in .git/entire-sessions/
// until no session has phase "active", or fails the test after timeout.
func WaitForSessionIdle(t *testing.T, dir string, timeout time.Duration) {
	t.Helper()
	stateDir := filepath.Join(dir, ".git", "entire-sessions")
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		entries, err := os.ReadDir(stateDir)
		if err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		anyActive := false
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") || strings.HasSuffix(entry.Name(), ".tmp") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(stateDir, entry.Name()))
			if err != nil {
				continue
			}
			var state struct {
				Phase string `json:"phase"`
			}
			if err := json.Unmarshal(data, &state); err != nil {
				continue
			}
			if state.Phase == "active" {
				anyActive = true
				break
			}
		}
		if !anyActive {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("session(s) did not transition to idle within %s", timeout)
}

// ValidateCheckpointDeep performs comprehensive validation of checkpoint metadata
// on the checkpoint branch, including transcript JSONL validity, content hash
// verification, and prompt content checking.
func ValidateCheckpointDeep(t *testing.T, dir string, v DeepCheckpointValidation) {
	t.Helper()

	AssertCheckpointExists(t, dir, v.CheckpointID)
	AssertCheckpointMetadataComplete(t, dir, v.CheckpointID)

	if v.Strategy != "" {
		meta := ReadCheckpointMetadata(t, dir, v.CheckpointID)
		assert.Equal(t, v.Strategy, meta.Strategy,
			"checkpoint %s: strategy mismatch", v.CheckpointID)
	}

	if len(v.FilesTouched) > 0 {
		AssertCheckpointFilesTouched(t, dir, v.CheckpointID, v.FilesTouched)
	}

	path := CheckpointPath(v.CheckpointID)

	// Validate session metadata exists and has checkpoint_id
	sessionBlob := fmt.Sprintf("entire/checkpoints/v1:%s/0/metadata.json", path)
	sessionRaw := gitOutputSafe(dir, "show", sessionBlob)
	if assert.NotEmpty(t, sessionRaw, "session metadata should exist at %s", sessionBlob) {
		var sessionMeta map[string]any
		if assert.NoError(t, json.Unmarshal([]byte(sessionRaw), &sessionMeta)) {
			assert.Equal(t, v.CheckpointID, sessionMeta["checkpoint_id"],
				"session metadata checkpoint_id should match")
			assert.NotEmpty(t, sessionMeta["created_at"], "session metadata should have created_at")
		}
	}

	// Validate transcript is valid JSONL
	transcriptBlob := fmt.Sprintf("entire/checkpoints/v1:%s/0/full.jsonl", path)
	transcriptRaw := gitOutputSafe(dir, "show", transcriptBlob)
	if assert.NotEmpty(t, transcriptRaw, "transcript should exist at %s", transcriptBlob) {
		lines := strings.Split(transcriptRaw, "\n")
		nonEmpty := 0
		for _, line := range lines {
			if strings.TrimSpace(line) != "" {
				nonEmpty++
			}
		}
		assert.Positive(t, nonEmpty, "transcript should have at least one line")

		for _, expected := range v.ExpectedTranscriptContent {
			assert.Contains(t, transcriptRaw, expected,
				"transcript should contain %q", expected)
		}

		// Validate content hash
		hashBlob := fmt.Sprintf("entire/checkpoints/v1:%s/0/content_hash.txt", path)
		hashRaw := gitOutputSafe(dir, "show", hashBlob)
		if hashRaw != "" {
			hash := sha256.Sum256([]byte(transcriptRaw))
			expectedHash := "sha256:" + hex.EncodeToString(hash[:])
			assert.Equal(t, expectedHash, strings.TrimSpace(hashRaw),
				"content hash should match transcript SHA-256")
		}
	}

	// Validate prompt.txt if expected prompts specified
	if len(v.ExpectedPrompts) > 0 {
		promptBlob := fmt.Sprintf("entire/checkpoints/v1:%s/0/prompt.txt", path)
		promptRaw := gitOutputSafe(dir, "show", promptBlob)
		for _, expected := range v.ExpectedPrompts {
			assert.Contains(t, promptRaw, expected,
				"prompt.txt should contain %q", expected)
		}
	}
}
