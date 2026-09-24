package grok

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/entireio/external-agents/agents/entire-agent-grok/internal/protocol"
)

func TestEncodeRepoCWD(t *testing.T) {
	for name, repo := range map[string]string{
		"forward slashes":   "/Users/test/project",
		"native separators": filepath.FromSlash("/Users/test/project"),
	} {
		t.Run(name, func(t *testing.T) {
			if encoded := encodeRepoCWD(repo); encoded != "%2FUsers%2Ftest%2Fproject" {
				t.Fatalf("unexpected encoded cwd: %q", encoded)
			}
		})
	}
}

func TestNativeTranscriptPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GROK_HOME", home)
	for name, repo := range map[string]string{
		"forward slashes":   "/Users/test/project",
		"native separators": filepath.FromSlash("/Users/test/project"),
	} {
		t.Run(name, func(t *testing.T) {
			path := nativeTranscriptPath(repo, "session-123")
			want := filepath.Join(home, "sessions", "%2FUsers%2Ftest%2Fproject", "session-123", "chat_history.jsonl")
			if path != want {
				t.Fatalf("transcript path = %q, want %q", path, want)
			}
		})
	}
}

func TestNativeSessionDirMatchesMarkerSeparators(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GROK_HOME", home)
	repo := "/very/long/path/" + strings.Repeat("segmentxxxxxxxxxx/", 20) + "tail"
	hashed := filepath.Join(home, "sessions", "tail-test-hash")
	if err := os.MkdirAll(hashed, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, marker := range map[string]string{
		"forward slashes":   repo,
		"native separators": filepath.FromSlash(repo),
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(hashed, cwdMarkerFile), []byte(marker+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if got := nativeSessionDir(filepath.FromSlash(repo)); got != hashed {
				t.Fatalf("session directory = %q, want %q", got, hashed)
			}
		})
	}
}

func TestSessionFileResolversContainUnsafeSessionIDs(t *testing.T) {
	grokHome := t.TempDir()
	t.Setenv("GROK_HOME", grokHome)
	repo := t.TempDir()
	sessionDir := filepath.Join(t.TempDir(), "sessions")
	agent := New()

	tests := []struct {
		name      string
		sessionID string
		unchanged bool
	}{
		{name: "benign", sessionID: "session-123", unchanged: true},
		{name: "unicode benign", sessionID: "sessión-１２３", unchanged: true},
		{name: "slash traversal", sessionID: "../../../../tmp/pwned"},
		{name: "backslash traversal", sessionID: `..\..\tmp\pwned`},
		{name: "nested path", sessionID: "nested/session"},
		{name: "empty fallback", sessionID: ""},
		{name: "whitespace fallback", sessionID: " \t"},
		{name: "trailing whitespace", sessionID: ".. "},
		{name: "leading whitespace", sessionID: " .."},
		{name: "surrounding whitespace", sessionID: " .. "},
		{name: "windows drive relative", sessionID: "C:outside"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			safeID := safeFilename(tt.sessionID)
			if got := safeFilename(tt.sessionID); got != safeID {
				t.Fatalf("safeFilename() is not deterministic: first %q, second %q", safeID, got)
			}
			switch {
			case strings.TrimSpace(tt.sessionID) == "":
				if safeID != stubSessionID {
					t.Fatalf("blank ID = %q, want %q", safeID, stubSessionID)
				}
			case tt.unchanged:
				if safeID != tt.sessionID {
					t.Fatalf("safeFilename() changed benign ID %q to %q", tt.sessionID, safeID)
				}
			default:
				if safeID == tt.sessionID {
					t.Fatalf("safeFilename() left unsafe ID unchanged: %q", safeID)
				}
				sum := sha256.Sum256([]byte(tt.sessionID))
				wantSafeID := "~" + hex.EncodeToString(sum[:16])
				if safeID != wantSafeID {
					t.Fatalf("safeFilename() = %q, want reserved SHA-256 name %q", safeID, wantSafeID)
				}
			}
			if strings.ContainsAny(safeID, `/\`) || strings.TrimSpace(safeID) != safeID {
				t.Fatalf("safeFilename() returned unsafe path component %q", safeID)
			}

			publicPath := agent.ResolveSessionFile(sessionDir, tt.sessionID)
			wantPublicPath := filepath.Join(sessionDir, safeID, nativeTranscriptFile)
			if publicPath != wantPublicPath {
				t.Fatalf("ResolveSessionFile() = %q, want %q", publicPath, wantPublicPath)
			}
			assertPathWithin(t, sessionDir, publicPath)

			nativeDir := nativeSessionDir(repo)
			wantNativePath := filepath.Join(nativeDir, safeID, nativeTranscriptFile)
			if got := nativeTranscriptPath(repo, tt.sessionID); got != wantNativePath {
				t.Fatalf("nativeTranscriptPath() = %q, want %q", got, wantNativePath)
			}
			if got := agent.resolveSessionRef(tt.sessionID, repo); got != wantNativePath {
				t.Fatalf("resolveSessionRef() = %q, want %q", got, wantNativePath)
			}
			assertPathWithin(t, nativeDir, wantNativePath)
		})
	}
}

func TestSafeFilenameSeparatesSanitizationCollisions(t *testing.T) {
	for _, pair := range [][2]string{
		{"../../../../tmp/pwned", "tmp_pwned"},
		{`..\..\tmp\pwned`, "tmp_pwned"},
		{"../../../../tmp/pwned", `..\..\tmp\pwned`},
		{"nested/session", "nested_session"},
		{".. ", stubSessionID},
		{".. ", " .."},
		{".. ", " .. "},
		{"C:outside", "C_outside"},
	} {
		first := safeFilename(pair[0])
		second := safeFilename(pair[1])
		if first == second {
			t.Fatalf("safeFilename(%q) and safeFilename(%q) both returned %q", pair[0], pair[1], first)
		}
	}
}

func TestParseHookContainsUnsafeSessionIDInSessionRef(t *testing.T) {
	t.Setenv("GROK_HOME", t.TempDir())
	repo := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repo)
	agent := New()
	sessionID := "../../outside"

	payload, err := json.Marshal(map[string]string{
		"session_id": sessionID,
		"cwd":        repo,
	})
	if err != nil {
		t.Fatal(err)
	}
	event, err := agent.ParseHook(HookNameSessionStart, payload)
	if err != nil {
		t.Fatalf("ParseHook(): %v", err)
	}
	if event == nil {
		t.Fatal("ParseHook() returned no event")
	}

	sessionDir := nativeSessionDir(repo)
	safeID := safeFilename(sessionID)
	want := filepath.Join(sessionDir, safeID, nativeTranscriptFile)
	if event.SessionRef != want {
		t.Fatalf("SessionRef = %q, want %q", event.SessionRef, want)
	}
	assertPathWithin(t, sessionDir, event.SessionRef)
	markerPath := filepath.Join(repo, ".entire", "tmp", safeID+".json")
	markerData, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("read marker using canonical session component: %v", err)
	}
	var marker protocol.AgentSessionJSON
	if err := json.Unmarshal(markerData, &marker); err != nil {
		t.Fatalf("decode marker: %v", err)
	}
	if marker.SessionRef != event.SessionRef {
		t.Fatalf("marker SessionRef = %q, event SessionRef = %q", marker.SessionRef, event.SessionRef)
	}
}

func TestWriteSessionWithResolvedUnsafeIDStaysWithinSessionDir(t *testing.T) {
	t.Setenv("GROK_HOME", t.TempDir())
	root := t.TempDir()
	sessionDir := filepath.Join(root, "sessions")
	outsidePath := filepath.Join(root, "outside", nativeTranscriptFile)
	if err := os.MkdirAll(filepath.Dir(outsidePath), 0o700); err != nil {
		t.Fatal(err)
	}
	const sentinel = "do not overwrite"
	if err := os.WriteFile(outsidePath, []byte(sentinel), 0o600); err != nil {
		t.Fatal(err)
	}

	agent := New()
	sessionID := "../outside"
	sessionRef := agent.ResolveSessionFile(sessionDir, sessionID)
	nativeData := []byte("{\"type\":\"user\",\"content\":\"restored\"}\n")
	if err := agent.WriteSession(protocol.AgentSessionJSON{
		SessionID:  sessionID,
		RepoPath:   root,
		SessionRef: sessionRef,
		NativeData: nativeData,
	}); err != nil {
		t.Fatalf("WriteSession(): %v", err)
	}

	outsideData, err := os.ReadFile(outsidePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(outsideData) != sentinel {
		t.Fatalf("outside transcript was overwritten with %q", outsideData)
	}
	assertPathWithin(t, sessionDir, sessionRef)
	written, err := os.ReadFile(sessionRef)
	if err != nil {
		t.Fatalf("read contained transcript: %v", err)
	}
	if string(written) != string(nativeData) {
		t.Fatalf("contained transcript = %q, want %q", written, nativeData)
	}
}

func assertPathWithin(t *testing.T, base, path string) {
	t.Helper()
	rel, err := filepath.Rel(base, path)
	if err != nil {
		t.Fatalf("filepath.Rel(%q, %q): %v", base, path, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		t.Fatalf("path %q escapes base %q (relative path %q)", path, base, rel)
	}
}
