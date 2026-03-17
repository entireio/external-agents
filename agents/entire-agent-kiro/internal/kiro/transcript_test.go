package kiro

import (
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestReadTranscript(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.json")
	want := []byte(`{"conversation_id":"session-789","history":[]}`)
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	got, err := New().ReadTranscript(path)
	if err != nil {
		t.Fatalf("ReadTranscript() error = %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("ReadTranscript() = %q, want %q", string(got), string(want))
	}
}

func TestChunkTranscriptRoundTrip(t *testing.T) {
	original := []byte("abcdefghijklmnopqrstuvwxyz")

	chunks, err := New().ChunkTranscript(original, 8)
	if err != nil {
		t.Fatalf("ChunkTranscript() error = %v", err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for i, chunk := range chunks {
		if len(chunk) > 8 {
			t.Fatalf("chunk %d length = %d, want <= 8", i, len(chunk))
		}
	}

	reassembled, err := New().ReassembleTranscript(chunks)
	if err != nil {
		t.Fatalf("ReassembleTranscript() error = %v", err)
	}
	if !bytes.Equal(reassembled, original) {
		t.Fatalf("reassembled = %q, want %q", string(reassembled), string(original))
	}
}

func TestQuerySessionIDParsesSQLiteJSON(t *testing.T) {
	repoRoot := t.TempDir()
	home := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)
	t.Setenv("HOME", home)

	db := createFakeKiroDB(t, home)
	restore := stubSQLiteRunner(t, func(args ...string) ([]byte, error) {
		if len(args) != 3 {
			t.Fatalf("sqlite args = %#v, want [-json <db> <query>]", args)
		}
		if args[0] != "-json" {
			t.Fatalf("sqlite mode = %q, want %q", args[0], "-json")
		}
		if args[1] != db {
			t.Fatalf("sqlite db = %q, want %q", args[1], db)
		}
		return []byte(`[{"json_extract(value, '$.conversation_id')":"native-session"}]`), nil
	})
	defer restore()

	sessionID, err := New().querySessionID(repoRoot)
	if err != nil {
		t.Fatalf("querySessionID() error = %v", err)
	}
	if sessionID != "native-session" {
		t.Fatalf("sessionID = %q, want %q", sessionID, "native-session")
	}
}

func TestEnsureCachedTranscriptWritesSQLiteTranscript(t *testing.T) {
	repoRoot := t.TempDir()
	home := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)
	t.Setenv("HOME", home)

	db := createFakeKiroDB(t, home)
	restore := stubSQLiteRunner(t, func(args ...string) ([]byte, error) {
		if len(args) != 2 {
			t.Fatalf("sqlite args = %#v, want [<db> <query>]", args)
		}
		if args[0] != db {
			t.Fatalf("sqlite db = %q, want %q", args[0], db)
		}
		return []byte(`{"conversation_id":"cli-session","history":[]}`), nil
	})
	defer restore()

	cachePath, err := New().ensureCachedTranscript(repoRoot, "stable-session")
	if err != nil {
		t.Fatalf("ensureCachedTranscript() error = %v", err)
	}

	wantPath := filepath.Join(repoRoot, ".entire", "tmp", "stable-session.json")
	if cachePath != wantPath {
		t.Fatalf("cachePath = %q, want %q", cachePath, wantPath)
	}

	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("read cached transcript: %v", err)
	}
	want := `{"conversation_id":"cli-session","history":[]}`
	if string(data) != want {
		t.Fatalf("cached transcript = %q, want %q", string(data), want)
	}
}

func TestEnsureIDETranscriptCopiesLatestWorkspaceSession(t *testing.T) {
	repoRoot := t.TempDir()
	home := t.TempDir()
	cwd := filepath.Join(repoRoot, "workspace")
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)
	t.Setenv("HOME", home)
	if err := os.MkdirAll(cwd, 0o750); err != nil {
		t.Fatalf("mkdir cwd: %v", err)
	}

	sessionsDir := createIDEWorkspaceSessionsDir(t, home, cwd)
	index := `[
  {"sessionId":"older","title":"Old","dateCreated":"2026-01-01T00:00:00Z","workspaceDirectory":"` + cwd + `"},
  {"sessionId":"latest","title":"New","dateCreated":"2026-02-01T00:00:00Z","workspaceDirectory":"` + cwd + `"}
]`
	if err := os.WriteFile(filepath.Join(sessionsDir, "sessions.json"), []byte(index), 0o600); err != nil {
		t.Fatalf("write sessions.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessionsDir, "older.json"), []byte(`{"history":[{"message":{"role":"assistant","content":"old"}}]}`), 0o600); err != nil {
		t.Fatalf("write older transcript: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessionsDir, "latest.json"), []byte(`{"history":[{"message":{"role":"assistant","content":"new"}}]}`), 0o600); err != nil {
		t.Fatalf("write latest transcript: %v", err)
	}

	cachePath, err := New().ensureIDETranscript(cwd, "stable-session")
	if err != nil {
		t.Fatalf("ensureIDETranscript() error = %v", err)
	}

	wantPath := filepath.Join(repoRoot, ".entire", "tmp", "stable-session.json")
	if cachePath != wantPath {
		t.Fatalf("cachePath = %q, want %q", cachePath, wantPath)
	}

	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("read cached IDE transcript: %v", err)
	}
	if string(data) != `{"history":[{"message":{"role":"assistant","content":"new"}}]}` {
		t.Fatalf("cached IDE transcript = %q", string(data))
	}
}

func TestParseHookStopPrefersSQLiteTranscript(t *testing.T) {
	repoRoot := t.TempDir()
	home := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)
	t.Setenv("HOME", home)

	db := createFakeKiroDB(t, home)
	restore := stubSQLiteRunner(t, func(args ...string) ([]byte, error) {
		if len(args) != 2 {
			t.Fatalf("sqlite args = %#v, want [<db> <query>]", args)
		}
		if args[0] != db {
			t.Fatalf("sqlite db = %q, want %q", args[0], db)
		}
		return []byte(`{"conversation_id":"cli-session","history":[]}`), nil
	})
	defer restore()

	seedSessionIDCache(t, repoRoot, "stable-session")

	event, err := New().ParseHook(HookNameStop, []byte(`{"cwd":"`+repoRoot+`"}`))
	if err != nil {
		t.Fatalf("ParseHook(stop) error = %v", err)
	}
	if event == nil {
		t.Fatal("expected TurnEnd event")
	}
	wantPath := filepath.Join(repoRoot, ".entire", "tmp", "stable-session.json")
	if event.SessionRef != wantPath {
		t.Fatalf("sessionRef = %q, want %q", event.SessionRef, wantPath)
	}
	data, err := os.ReadFile(event.SessionRef)
	if err != nil {
		t.Fatalf("read cached transcript: %v", err)
	}
	if string(data) != `{"conversation_id":"cli-session","history":[]}` {
		t.Fatalf("cached transcript = %q", string(data))
	}
}

func TestParseHookStopFallsBackToIDETranscript(t *testing.T) {
	repoRoot := t.TempDir()
	home := t.TempDir()
	cwd := filepath.Join(repoRoot, "workspace")
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)
	t.Setenv("HOME", home)
	if err := os.MkdirAll(cwd, 0o750); err != nil {
		t.Fatalf("mkdir cwd: %v", err)
	}

	restore := stubSQLiteRunner(t, func(args ...string) ([]byte, error) {
		return nil, errors.New("sqlite unavailable")
	})
	defer restore()

	sessionsDir := createIDEWorkspaceSessionsDir(t, home, cwd)
	index := `[{"sessionId":"ide-session","title":"IDE","dateCreated":"2026-02-01T00:00:00Z","workspaceDirectory":"` + cwd + `"}]`
	if err := os.WriteFile(filepath.Join(sessionsDir, "sessions.json"), []byte(index), 0o600); err != nil {
		t.Fatalf("write sessions.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessionsDir, "ide-session.json"), []byte(`{"history":[{"message":{"role":"assistant","content":"ide"}}]}`), 0o600); err != nil {
		t.Fatalf("write IDE transcript: %v", err)
	}

	seedSessionIDCache(t, repoRoot, "stable-session")

	event, err := New().ParseHook(HookNameStop, []byte(`{"cwd":"`+cwd+`"}`))
	if err != nil {
		t.Fatalf("ParseHook(stop) error = %v", err)
	}
	data, err := os.ReadFile(event.SessionRef)
	if err != nil {
		t.Fatalf("read cached transcript: %v", err)
	}
	if string(data) != `{"history":[{"message":{"role":"assistant","content":"ide"}}]}` {
		t.Fatalf("cached IDE transcript = %q", string(data))
	}
}

func TestParseHookStopFallsBackToPlaceholderTranscript(t *testing.T) {
	repoRoot := t.TempDir()
	home := t.TempDir()
	cwd := filepath.Join(repoRoot, "workspace")
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)
	t.Setenv("HOME", home)
	if err := os.MkdirAll(cwd, 0o750); err != nil {
		t.Fatalf("mkdir cwd: %v", err)
	}

	restore := stubSQLiteRunner(t, func(args ...string) ([]byte, error) {
		return nil, errors.New("sqlite unavailable")
	})
	defer restore()

	seedSessionIDCache(t, repoRoot, "stable-session")

	event, err := New().ParseHook(HookNameStop, []byte(`{"cwd":"`+cwd+`"}`))
	if err != nil {
		t.Fatalf("ParseHook(stop) error = %v", err)
	}
	data, err := os.ReadFile(event.SessionRef)
	if err != nil {
		t.Fatalf("read placeholder transcript: %v", err)
	}
	if string(data) != "{}" {
		t.Fatalf("placeholder transcript = %q, want %q", string(data), "{}")
	}
}

func stubSQLiteRunner(t *testing.T, fn func(args ...string) ([]byte, error)) func() {
	t.Helper()
	previous := runSQLiteCommand
	runSQLiteCommand = fn
	return func() {
		runSQLiteCommand = previous
	}
}

func createFakeKiroDB(t *testing.T, home string) string {
	t.Helper()
	db := expectedCLIKiroDBPath(home)
	if err := os.MkdirAll(filepath.Dir(db), 0o750); err != nil {
		t.Fatalf("mkdir db dir: %v", err)
	}
	if err := os.WriteFile(db, []byte{}, 0o600); err != nil {
		t.Fatalf("write fake db: %v", err)
	}
	return db
}

func createIDEWorkspaceSessionsDir(t *testing.T, home string, cwd string) string {
	t.Helper()
	encoded := base64.StdEncoding.EncodeToString([]byte(cwd))
	var dir string
	switch runtime.GOOS {
	case "darwin":
		dir = filepath.Join(home, "Library", "Application Support", "Kiro", "User", "globalStorage", "kiro.kiroagent", "workspace-sessions", encoded)
	default:
		dir = filepath.Join(home, ".config", "Kiro", "User", "globalStorage", "kiro.kiroagent", "workspace-sessions", encoded)
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir sessions dir: %v", err)
	}
	return dir
}

func expectedCLIKiroDBPath(home string) string {
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "kiro-cli", "data.sqlite3")
	default:
		return filepath.Join(home, ".local", "share", "kiro-cli", "data.sqlite3")
	}
}

func seedSessionIDCache(t *testing.T, repoRoot string, sessionID string) {
	t.Helper()
	cacheDir := filepath.Join(repoRoot, ".entire", "tmp")
	if err := os.MkdirAll(cacheDir, 0o750); err != nil {
		t.Fatalf("mkdir cache dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, sessionIDFile), []byte(sessionID), 0o600); err != nil {
		t.Fatalf("write session id cache: %v", err)
	}
}
