package kiro

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

const testCLIAnalyzerTranscript = `{
  "conversation_id": "test-conv-123",
  "history": [
    {
      "user": {"content": {"Prompt": {"prompt": "Create a hello.go file"}}, "timestamp": "2026-01-01T00:00:00Z"},
      "assistant": {"Response": {"message_id": "msg-1", "content": "I'll create that file for you."}}
    },
    {
      "user": {"content": {"Prompt": {"prompt": "Now add a test"}}, "timestamp": "2026-01-01T00:01:00Z"},
      "assistant": {"ToolUse": {"message_id": "msg-2", "tool_uses": [
        {"id": "tu-1", "name": "fs_write", "args": {"path": "/repo/hello.go", "content": "package main"}}
      ]}}
    },
    {
      "user": {"content": {"ToolUseResults": {"tool_use_results": [{"id": "tu-1", "result": "ok"}]}}},
      "assistant": {"ToolUse": {"message_id": "msg-3", "tool_uses": [
        {"id": "tu-2", "name": "fs_write", "args": {"path": "/repo/hello_test.go", "content": "package main"}}
      ]}}
    },
    {
      "user": {"content": {"ToolUseResults": {"tool_use_results": [{"id": "tu-2", "result": "ok"}]}}},
      "assistant": {"Response": {"message_id": "msg-4", "content": "Done! I created both files."}}
    }
  ]
}`

const testIDEAnalyzerTranscript = `{
  "history": [
    {
      "message": {
        "role": "user",
        "content": [{"type": "text", "text": "Open the workspace"}]
      }
    },
    {
      "message": {
        "role": "assistant",
        "content": "I opened the workspace."
      }
    },
    {
      "message": {
        "role": "user",
        "content": [{"type": "text", "text": "Create app.js"}]
      }
    },
    {
      "message": {
        "role": "assistant",
        "content": "Created app.js."
      }
    }
  ]
}`

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

	stubData := buildTranscript("cli-session", 2)
	db := createFakeKiroDB(t, home)
	restore := stubSQLiteRunner(t, func(args ...string) ([]byte, error) {
		if len(args) != 2 {
			t.Fatalf("sqlite args = %#v, want [<db> <query>]", args)
		}
		if args[0] != db {
			t.Fatalf("sqlite db = %q, want %q", args[0], db)
		}
		return stubData, nil
	})
	defer restore()

	cachePath, err := New().ensureCachedTranscript(repoRoot, "stable-session", "")
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

	var result kiroTranscript
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("parse cached transcript: %v", err)
	}
	if result.ConversationID != "cli-session" || len(result.History) != 2 {
		t.Fatalf("cached transcript conv=%q history=%d, want cli-session/2", result.ConversationID, len(result.History))
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

func TestEnsureIDETranscriptRejectsPathTraversal(t *testing.T) {
	repoRoot := t.TempDir()
	home := t.TempDir()
	cwd := filepath.Join(repoRoot, "workspace")
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)
	t.Setenv("HOME", home)
	if err := os.MkdirAll(cwd, 0o750); err != nil {
		t.Fatalf("mkdir cwd: %v", err)
	}

	sessionsDir := createIDEWorkspaceSessionsDir(t, home, cwd)
	index := `[{"sessionId":"../../etc/passwd","title":"Evil","dateCreated":"2026-01-01T00:00:00Z"}]`
	if err := os.WriteFile(filepath.Join(sessionsDir, "sessions.json"), []byte(index), 0o600); err != nil {
		t.Fatalf("write sessions.json: %v", err)
	}

	_, err := New().ensureIDETranscript(cwd, "stable-session")
	if err == nil {
		t.Fatal("expected error for path-traversal session ID, got nil")
	}
	if !strings.Contains(err.Error(), "invalid session ID") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseHookStopPrefersSQLiteTranscript(t *testing.T) {
	repoRoot := t.TempDir()
	home := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)
	t.Setenv("HOME", home)

	db := createFakeKiroDB(t, home)
	restore := stubSQLiteRunner(t, func(args ...string) ([]byte, error) {
		// The stop hook now calls querySessionID (3 args: -json, db, query)
		// and then ensureCachedTranscript (2 args: db, query).
		switch len(args) {
		case 3:
			if args[0] != "-json" {
				t.Fatalf("sqlite mode = %q, want %q", args[0], "-json")
			}
			if args[1] != db {
				t.Fatalf("sqlite db = %q, want %q", args[1], db)
			}
			return []byte(`[{"json_extract(value, '$.conversation_id')":"cli-session"}]`), nil
		case 2:
			if args[0] != db {
				t.Fatalf("sqlite db = %q, want %q", args[0], db)
			}
			return buildTranscript("cli-session", 1), nil
		default:
			t.Fatalf("unexpected sqlite args count: %d", len(args))
			return nil, nil
		}
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
	var result kiroTranscript
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("parse cached transcript: %v", err)
	}
	if result.ConversationID != "cli-session" || len(result.History) != 1 {
		t.Fatalf("cached transcript conv=%q history=%d, want cli-session/1", result.ConversationID, len(result.History))
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

func kiroExtensionTestDir(t *testing.T, home string) string {
	t.Helper()
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Kiro", "User", "globalStorage", "kiro.kiroagent")
	default:
		return filepath.Join(home, ".config", "Kiro", "User", "globalStorage", "kiro.kiroagent")
	}
}

func createIDEWorkspaceSessionsDir(t *testing.T, home string, cwd string) string {
	t.Helper()
	encoded := strings.ReplaceAll(base64.StdEncoding.EncodeToString([]byte(cwd)), "=", "_")
	dir := filepath.Join(kiroExtensionTestDir(t, home), "workspace-sessions", encoded)
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

func TestGetTranscriptPositionCountsCLIHistoryEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.json")
	if err := os.WriteFile(path, []byte(testCLIAnalyzerTranscript), 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	position, err := New().GetTranscriptPosition(path)
	if err != nil {
		t.Fatalf("GetTranscriptPosition() error = %v", err)
	}
	if position != 4 {
		t.Fatalf("position = %d, want %d", position, 4)
	}
}

func TestGetTranscriptPositionHandlesPlaceholderTranscript(t *testing.T) {
	path := filepath.Join(t.TempDir(), "placeholder.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	position, err := New().GetTranscriptPosition(path)
	if err != nil {
		t.Fatalf("GetTranscriptPosition() error = %v", err)
	}
	if position != 0 {
		t.Fatalf("position = %d, want %d", position, 0)
	}
}

func TestExtractModifiedFilesFromCLITranscript(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.json")
	if err := os.WriteFile(path, []byte(testCLIAnalyzerTranscript), 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	files, currentPosition, err := New().ExtractModifiedFiles(path, 0)
	if err != nil {
		t.Fatalf("ExtractModifiedFiles() error = %v", err)
	}

	wantFiles := []string{"/repo/hello.go", "/repo/hello_test.go"}
	if !slices.Equal(files, wantFiles) {
		t.Fatalf("files = %v, want %v", files, wantFiles)
	}
	if currentPosition != 4 {
		t.Fatalf("currentPosition = %d, want %d", currentPosition, 4)
	}
}

func TestExtractPromptsFromCLITranscript(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.json")
	if err := os.WriteFile(path, []byte(testCLIAnalyzerTranscript), 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	prompts, err := New().ExtractPrompts(path, 0)
	if err != nil {
		t.Fatalf("ExtractPrompts() error = %v", err)
	}

	want := []string{"Create a hello.go file", "Now add a test"}
	if !slices.Equal(prompts, want) {
		t.Fatalf("prompts = %v, want %v", prompts, want)
	}
}

func TestExtractSummaryFromCLITranscript(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.json")
	if err := os.WriteFile(path, []byte(testCLIAnalyzerTranscript), 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	summary, hasSummary, err := New().ExtractSummary(path)
	if err != nil {
		t.Fatalf("ExtractSummary() error = %v", err)
	}
	if !hasSummary {
		t.Fatal("expected summary to be present")
	}
	if summary != "Done! I created both files." {
		t.Fatalf("summary = %q, want %q", summary, "Done! I created both files.")
	}
}

func TestIDETranscriptAnalyzerParsesPromptsAndSummary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ide-transcript.json")
	if err := os.WriteFile(path, []byte(testIDEAnalyzerTranscript), 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	position, err := New().GetTranscriptPosition(path)
	if err != nil {
		t.Fatalf("GetTranscriptPosition() error = %v", err)
	}
	if position != 2 {
		t.Fatalf("position = %d, want %d", position, 2)
	}

	prompts, err := New().ExtractPrompts(path, 0)
	if err != nil {
		t.Fatalf("ExtractPrompts() error = %v", err)
	}
	wantPrompts := []string{"Open the workspace", "Create app.js"}
	if !slices.Equal(prompts, wantPrompts) {
		t.Fatalf("prompts = %v, want %v", prompts, wantPrompts)
	}

	summary, hasSummary, err := New().ExtractSummary(path)
	if err != nil {
		t.Fatalf("ExtractSummary() error = %v", err)
	}
	if !hasSummary {
		t.Fatal("expected IDE summary to be present")
	}
	if summary != "Created app.js." {
		t.Fatalf("summary = %q, want %q", summary, "Created app.js.")
	}
}

func TestPlaceholderTranscriptAnalyzerReturnsNoFilesPromptsOrSummary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "placeholder.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	files, currentPosition, err := New().ExtractModifiedFiles(path, 0)
	if err != nil {
		t.Fatalf("ExtractModifiedFiles() error = %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("files = %v, want empty", files)
	}
	if currentPosition != 0 {
		t.Fatalf("currentPosition = %d, want %d", currentPosition, 0)
	}

	prompts, err := New().ExtractPrompts(path, 0)
	if err != nil {
		t.Fatalf("ExtractPrompts() error = %v", err)
	}
	if len(prompts) != 0 {
		t.Fatalf("prompts = %v, want empty", prompts)
	}

	summary, hasSummary, err := New().ExtractSummary(path)
	if err != nil {
		t.Fatalf("ExtractSummary() error = %v", err)
	}
	if hasSummary {
		t.Fatalf("hasSummary = true, want false with summary %q", summary)
	}
	if summary != "" {
		t.Fatalf("summary = %q, want empty", summary)
	}
}

// buildTranscript creates a kiro CLI transcript with the given number of prompt entries.
func buildTranscript(convID string, numPrompts int) []byte {
	var entries []kiroHistoryEntry
	for i := range numPrompts {
		promptJSON, _ := json.Marshal(kiroPromptContent{
			Prompt: struct {
				Prompt string `json:"prompt"`
			}{Prompt: fmt.Sprintf("prompt %d", i)},
		})
		entries = append(entries, kiroHistoryEntry{
			User: kiroUserMessage{Content: promptJSON},
		})
	}
	transcript := kiroTranscript{ConversationID: convID, History: entries}
	data, _ := json.Marshal(transcript)
	return data
}

func seedTranscriptOffset(t *testing.T, repoRoot string, convID string, position int) {
	t.Helper()
	offsetPath := filepath.Join(repoRoot, ".entire", "tmp", "kiro-transcript-offset.json")
	if err := os.MkdirAll(filepath.Dir(offsetPath), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data, _ := json.Marshal(transcriptOffset{ConversationID: convID, Position: position})
	if err := os.WriteFile(offsetPath, data, 0o600); err != nil {
		t.Fatalf("write offset: %v", err)
	}
}

func readTestTranscriptOffset(t *testing.T, repoRoot string) transcriptOffset {
	t.Helper()
	offsetPath := filepath.Join(repoRoot, ".entire", "tmp", "kiro-transcript-offset.json")
	return readTranscriptOffset(offsetPath)
}

func TestEnsureCachedTranscriptTrimsWithOffset(t *testing.T) {
	repoRoot := t.TempDir()
	home := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)
	t.Setenv("HOME", home)

	transcriptData := buildTranscript("conv-1", 8)

	db := createFakeKiroDB(t, home)
	restore := stubSQLiteRunner(t, func(args ...string) ([]byte, error) {
		if args[0] != db {
			t.Fatalf("sqlite db = %q, want %q", args[0], db)
		}
		return transcriptData, nil
	})
	defer restore()

	seedTranscriptOffset(t, repoRoot, "conv-1", 4)

	cachePath, err := New().ensureCachedTranscript(repoRoot, "test-session", "")
	if err != nil {
		t.Fatalf("ensureCachedTranscript() error = %v", err)
	}

	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("read cached transcript: %v", err)
	}

	var result kiroTranscript
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("parse cached transcript: %v", err)
	}

	if len(result.History) != 4 {
		t.Fatalf("history length = %d, want 4 (entries 4-7)", len(result.History))
	}

	firstPrompt := extractUserPrompt(result.History[0].User.Content)
	if firstPrompt != "prompt 4" {
		t.Fatalf("first prompt = %q, want %q", firstPrompt, "prompt 4")
	}

	offset := readTestTranscriptOffset(t, repoRoot)
	if offset.Position != 8 {
		t.Fatalf("offset position = %d, want 8", offset.Position)
	}
}

func TestEnsureCachedTranscriptFirstCapture(t *testing.T) {
	repoRoot := t.TempDir()
	home := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)
	t.Setenv("HOME", home)

	transcriptData := buildTranscript("conv-1", 4)

	db := createFakeKiroDB(t, home)
	restore := stubSQLiteRunner(t, func(args ...string) ([]byte, error) {
		if args[0] != db {
			t.Fatalf("sqlite db = %q, want %q", args[0], db)
		}
		return transcriptData, nil
	})
	defer restore()

	// No offset file — full transcript should be written.
	cachePath, err := New().ensureCachedTranscript(repoRoot, "test-session", "")
	if err != nil {
		t.Fatalf("ensureCachedTranscript() error = %v", err)
	}

	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("read cached transcript: %v", err)
	}

	var result kiroTranscript
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("parse cached transcript: %v", err)
	}

	if len(result.History) != 4 {
		t.Fatalf("history length = %d, want 4 (full transcript)", len(result.History))
	}

	offset := readTestTranscriptOffset(t, repoRoot)
	if offset.ConversationID != "conv-1" || offset.Position != 4 {
		t.Fatalf("offset = %+v, want {conv-1, 4}", offset)
	}
}

func TestEnsureCachedTranscriptConversationIDChange(t *testing.T) {
	repoRoot := t.TempDir()
	home := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)
	t.Setenv("HOME", home)

	transcriptData := buildTranscript("new-conv", 3)

	db := createFakeKiroDB(t, home)
	restore := stubSQLiteRunner(t, func(args ...string) ([]byte, error) {
		if args[0] != db {
			t.Fatalf("sqlite db = %q, want %q", args[0], db)
		}
		return transcriptData, nil
	})
	defer restore()

	// Offset from a different conversation.
	seedTranscriptOffset(t, repoRoot, "old-conv", 5)

	cachePath, err := New().ensureCachedTranscript(repoRoot, "test-session", "")
	if err != nil {
		t.Fatalf("ensureCachedTranscript() error = %v", err)
	}

	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("read cached transcript: %v", err)
	}

	var result kiroTranscript
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("parse cached transcript: %v", err)
	}

	// Full transcript (no trimming) since conversation_id changed.
	if len(result.History) != 3 {
		t.Fatalf("history length = %d, want 3 (full, new conversation)", len(result.History))
	}

	offset := readTestTranscriptOffset(t, repoRoot)
	if offset.ConversationID != "new-conv" || offset.Position != 3 {
		t.Fatalf("offset = %+v, want {new-conv, 3}", offset)
	}
}

func TestEnsureCachedTranscriptOffsetExceedsLength(t *testing.T) {
	repoRoot := t.TempDir()
	home := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)
	t.Setenv("HOME", home)

	transcriptData := buildTranscript("conv-1", 3)

	db := createFakeKiroDB(t, home)
	restore := stubSQLiteRunner(t, func(args ...string) ([]byte, error) {
		if args[0] != db {
			t.Fatalf("sqlite db = %q, want %q", args[0], db)
		}
		return transcriptData, nil
	})
	defer restore()

	// Offset exceeds transcript length.
	seedTranscriptOffset(t, repoRoot, "conv-1", 100)

	cachePath, err := New().ensureCachedTranscript(repoRoot, "test-session", "")
	if err != nil {
		t.Fatalf("ensureCachedTranscript() error = %v", err)
	}

	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("read cached transcript: %v", err)
	}

	var result kiroTranscript
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("parse cached transcript: %v", err)
	}

	// Full transcript (no trimming) since offset exceeds length.
	if len(result.History) != 3 {
		t.Fatalf("history length = %d, want 3 (full, offset exceeded)", len(result.History))
	}

	offset := readTestTranscriptOffset(t, repoRoot)
	if offset.Position != 3 {
		t.Fatalf("offset position = %d, want 3 (reset)", offset.Position)
	}
}

func TestEnsureCachedTranscriptNoNewEntriesReturnsFull(t *testing.T) {
	repoRoot := t.TempDir()
	home := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)
	t.Setenv("HOME", home)

	transcriptData := buildTranscript("conv-1", 4)

	db := createFakeKiroDB(t, home)
	restore := stubSQLiteRunner(t, func(args ...string) ([]byte, error) {
		if args[0] != db {
			t.Fatalf("sqlite db = %q, want %q", args[0], db)
		}
		return transcriptData, nil
	})
	defer restore()

	// Offset matches total entries — no new content, but should return
	// full cumulative transcript so the CLI always has a valid session_ref.
	seedTranscriptOffset(t, repoRoot, "conv-1", 4)

	cachePath, err := New().ensureCachedTranscript(repoRoot, "test-session", "")
	if err != nil {
		t.Fatalf("ensureCachedTranscript() should succeed with full transcript, got error: %v", err)
	}

	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("read cached transcript: %v", err)
	}

	var result kiroTranscript
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("parse cached transcript: %v", err)
	}

	// Full transcript returned (no trimming since offset == total).
	if len(result.History) != 4 {
		t.Fatalf("history length = %d, want 4 (full cumulative transcript)", len(result.History))
	}

	// Offset should NOT advance (still 4).
	offset := readTestTranscriptOffset(t, repoRoot)
	if offset.Position != 4 {
		t.Fatalf("offset position = %d, want 4 (should not advance)", offset.Position)
	}
}

func TestEnsureCachedTranscriptQueriesByConversationID(t *testing.T) {
	repoRoot := t.TempDir()
	home := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)
	t.Setenv("HOME", home)

	db := createFakeKiroDB(t, home)
	var capturedQuery string
	restore := stubSQLiteRunner(t, func(args ...string) ([]byte, error) {
		if args[0] != db {
			t.Fatalf("sqlite db = %q, want %q", args[0], db)
		}
		capturedQuery = args[1]
		return buildTranscript("target-conv", 1), nil
	})
	defer restore()

	_, err := New().ensureCachedTranscript(repoRoot, "test-session", "target-conv")
	if err != nil {
		t.Fatalf("ensureCachedTranscript() error = %v", err)
	}

	if !strings.Contains(capturedQuery, "target-conv") {
		t.Fatalf("query should contain conversation_id, got: %s", capturedQuery)
	}
	if !strings.Contains(capturedQuery, "json_extract") {
		t.Fatalf("query should use json_extract for conversation_id, got: %s", capturedQuery)
	}
}

func TestEnsureIDETranscriptWithModifiedBase64Encoding(t *testing.T) {
	repoRoot := t.TempDir()
	home := t.TempDir()
	cwd := filepath.Join(repoRoot, "workspace")
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)
	t.Setenv("HOME", home)
	if err := os.MkdirAll(cwd, 0o750); err != nil {
		t.Fatalf("mkdir cwd: %v", err)
	}

	// Create sessions dir using the REAL IDE encoding (= replaced with _)
	sessionsDir := createIDEWorkspaceSessionsDir(t, home, cwd)
	index := `[{"sessionId":"ide-delete-session","title":"delete files","dateCreated":"2026-03-21T09:05:00Z","workspaceDirectory":"` + cwd + `"}]`
	if err := os.WriteFile(filepath.Join(sessionsDir, "sessions.json"), []byte(index), 0o600); err != nil {
		t.Fatalf("write sessions.json: %v", err)
	}
	ideTranscript := `{"history":[{"message":{"role":"user","content":"please delete all the hello files"}},{"message":{"role":"assistant","content":"On it."}}]}`
	if err := os.WriteFile(filepath.Join(sessionsDir, "ide-delete-session.json"), []byte(ideTranscript), 0o600); err != nil {
		t.Fatalf("write IDE transcript: %v", err)
	}

	cachePath, err := New().ensureIDETranscript(cwd, "test-session")
	if err != nil {
		t.Fatalf("ensureIDETranscript() error = %v", err)
	}

	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("read cached IDE transcript: %v", err)
	}
	if !strings.Contains(string(data), "please delete all the hello files") {
		t.Fatalf("cached IDE transcript should contain delete prompt, got: %s", string(data))
	}
}

func TestCaptureTranscriptFallsToIDEWhenCLIUnavailable(t *testing.T) {
	repoRoot := t.TempDir()
	home := t.TempDir()
	cwd := filepath.Join(repoRoot, "workspace")
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)
	t.Setenv("HOME", home)
	if err := os.MkdirAll(cwd, 0o750); err != nil {
		t.Fatalf("mkdir cwd: %v", err)
	}

	// CLI DB is unavailable — forces IDE fallback
	restore := stubSQLiteRunner(t, func(args ...string) ([]byte, error) {
		return nil, errors.New("sqlite unavailable")
	})
	defer restore()

	// Set up IDE workspace sessions with real transcript
	sessionsDir := createIDEWorkspaceSessionsDir(t, home, cwd)
	index := `[{"sessionId":"ide-sess","title":"delete","dateCreated":"2026-03-21T09:05:00Z"}]`
	if err := os.WriteFile(filepath.Join(sessionsDir, "sessions.json"), []byte(index), 0o600); err != nil {
		t.Fatalf("write sessions.json: %v", err)
	}
	ideTranscript := `{"history":[{"message":{"role":"user","content":"please delete all the hello files"}},{"message":{"role":"assistant","content":"Deleted."}}]}`
	if err := os.WriteFile(filepath.Join(sessionsDir, "ide-sess.json"), []byte(ideTranscript), 0o600); err != nil {
		t.Fatalf("write IDE transcript: %v", err)
	}

	seedSessionIDCache(t, repoRoot, "test-session")

	event, err := New().ParseHook(HookNameStop, []byte(`{"cwd":"`+cwd+`"}`))
	if err != nil {
		t.Fatalf("ParseHook(stop) error = %v", err)
	}
	if event == nil || event.SessionRef == "" {
		t.Fatal("expected stop event with session ref")
	}

	data, err := os.ReadFile(event.SessionRef)
	if err != nil {
		t.Fatalf("read transcript: %v", err)
	}
	if !strings.Contains(string(data), "please delete all the hello files") {
		t.Fatalf("transcript should contain IDE content, got: %s", string(data))
	}
}

func TestCaptureTranscriptPrefersIDEOverCLI(t *testing.T) {
	repoRoot := t.TempDir()
	home := t.TempDir()
	cwd := filepath.Join(repoRoot, "workspace")
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)
	t.Setenv("HOME", home)
	if err := os.MkdirAll(cwd, 0o750); err != nil {
		t.Fatalf("mkdir cwd: %v", err)
	}

	// CLI DB returns a WRONG conversation (different from the IDE session)
	createFakeKiroDB(t, home)
	restore := stubSQLiteRunner(t, func(args ...string) ([]byte, error) {
		switch len(args) {
		case 3: // querySessionID
			return []byte(`[{"json_extract(value, '$.conversation_id')":"cli-wrong-conv"}]`), nil
		case 2: // ensureCachedTranscript
			return buildTranscript("cli-wrong-conv", 3), nil
		default:
			return nil, errors.New("unexpected")
		}
	})
	defer restore()

	// IDE workspace has the CORRECT transcript
	sessionsDir := createIDEWorkspaceSessionsDir(t, home, cwd)
	index := `[{"sessionId":"ide-correct","title":"python hello","dateCreated":"2026-03-21T10:00:00Z"}]`
	if err := os.WriteFile(filepath.Join(sessionsDir, "sessions.json"), []byte(index), 0o600); err != nil {
		t.Fatalf("write sessions.json: %v", err)
	}
	ideTranscript := `{"history":[{"message":{"role":"user","content":"add python hello world - ide"}},{"message":{"role":"assistant","content":"Created hello.py"}}]}`
	if err := os.WriteFile(filepath.Join(sessionsDir, "ide-correct.json"), []byte(ideTranscript), 0o600); err != nil {
		t.Fatalf("write IDE transcript: %v", err)
	}

	seedSessionIDCache(t, repoRoot, "test-session")

	event, err := New().ParseHook(HookNameStop, []byte(`{"cwd":"`+cwd+`"}`))
	if err != nil {
		t.Fatalf("ParseHook(stop) error = %v", err)
	}
	if event == nil || event.SessionRef == "" {
		t.Fatal("expected stop event with session ref")
	}

	data, err := os.ReadFile(event.SessionRef)
	if err != nil {
		t.Fatalf("read transcript: %v", err)
	}
	// IDE transcript should win over CLI
	if !strings.Contains(string(data), "add python hello world - ide") {
		t.Fatalf("should prefer IDE transcript, got: %s", string(data))
	}
}

func TestPostToolUseHookCapturesToolCalls(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)

	input := `{"tool_name":"fs_write","tool_input":{"path":"/repo/hello.py","command":"create","file_text":"print('hello')"}}`
	_, err := New().ParseHook(HookNamePostToolUse, []byte(input))
	if err != nil {
		t.Fatalf("ParseHook(post-tool-use) error = %v", err)
	}

	toolCallsPath := filepath.Join(repoRoot, ".entire", "tmp", toolCallsFile)
	data, err := os.ReadFile(toolCallsPath)
	if err != nil {
		t.Fatalf("tool calls file not found: %v", err)
	}
	if !strings.Contains(string(data), "fs_write") {
		t.Fatalf("tool calls should contain fs_write, got: %s", string(data))
	}
	if !strings.Contains(string(data), "/repo/hello.py") {
		t.Fatalf("tool calls should contain file path, got: %s", string(data))
	}
}

func TestEnsureIDETranscriptMergesToolCalls(t *testing.T) {
	repoRoot := t.TempDir()
	home := t.TempDir()
	cwd := filepath.Join(repoRoot, "workspace")
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)
	t.Setenv("HOME", home)
	if err := os.MkdirAll(cwd, 0o750); err != nil {
		t.Fatalf("mkdir cwd: %v", err)
	}

	// Set up IDE workspace session
	sessionsDir := createIDEWorkspaceSessionsDir(t, home, cwd)
	index := `[{"sessionId":"ide-sess","title":"create hello","dateCreated":"2026-03-21T12:00:00Z"}]`
	if err := os.WriteFile(filepath.Join(sessionsDir, "sessions.json"), []byte(index), 0o600); err != nil {
		t.Fatalf("write sessions.json: %v", err)
	}
	ideTranscript := `{"history":[{"message":{"role":"user","content":"create hello world python"}},{"message":{"role":"assistant","content":"On it."}}]}`
	if err := os.WriteFile(filepath.Join(sessionsDir, "ide-sess.json"), []byte(ideTranscript), 0o600); err != nil {
		t.Fatalf("write IDE transcript: %v", err)
	}

	// Seed tool calls JSONL (simulating post-tool-use hooks that fired)
	toolCallsDir := filepath.Join(repoRoot, ".entire", "tmp")
	if err := os.MkdirAll(toolCallsDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	toolCallLine := `{"name":"fs_write","args":{"path":"/repo/hello.py","command":"create","file_text":"print('hello')"}}` + "\n"
	if err := os.WriteFile(filepath.Join(toolCallsDir, toolCallsFile), []byte(toolCallLine), 0o600); err != nil {
		t.Fatalf("write tool calls: %v", err)
	}

	cachePath, err := New().ensureIDETranscript(cwd, "test-session")
	if err != nil {
		t.Fatalf("ensureIDETranscript() error = %v", err)
	}

	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("read cached transcript: %v", err)
	}

	// Should be in CLI format with tool calls merged
	var result kiroTranscript
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("parse cached transcript: %v", err)
	}

	// Extract modified files should find the tool call
	files, _, err := New().ExtractModifiedFiles(cachePath, 0)
	if err != nil {
		t.Fatalf("ExtractModifiedFiles() error = %v", err)
	}
	if len(files) == 0 {
		t.Fatal("expected at least one modified file from merged tool calls")
	}
	if files[0] != "/repo/hello.py" {
		t.Fatalf("files[0] = %q, want %q", files[0], "/repo/hello.py")
	}

	// Tool calls JSONL should be consumed (deleted)
	if _, err := os.Stat(filepath.Join(toolCallsDir, toolCallsFile)); !os.IsNotExist(err) {
		t.Fatal("tool calls JSONL should be deleted after consumption")
	}
}

func TestEnsureIDETranscriptWithoutToolCalls(t *testing.T) {
	repoRoot := t.TempDir()
	home := t.TempDir()
	cwd := filepath.Join(repoRoot, "workspace")
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)
	t.Setenv("HOME", home)
	if err := os.MkdirAll(cwd, 0o750); err != nil {
		t.Fatalf("mkdir cwd: %v", err)
	}

	sessionsDir := createIDEWorkspaceSessionsDir(t, home, cwd)
	index := `[{"sessionId":"ide-sess","title":"hello","dateCreated":"2026-03-21T12:00:00Z"}]`
	if err := os.WriteFile(filepath.Join(sessionsDir, "sessions.json"), []byte(index), 0o600); err != nil {
		t.Fatalf("write sessions.json: %v", err)
	}
	ideTranscript := `{"history":[{"message":{"role":"user","content":"hello"}},{"message":{"role":"assistant","content":"Hi!"}}]}`
	if err := os.WriteFile(filepath.Join(sessionsDir, "ide-sess.json"), []byte(ideTranscript), 0o600); err != nil {
		t.Fatalf("write IDE transcript: %v", err)
	}

	// No tool calls file — should still work, caching IDE format as-is
	cachePath, err := New().ensureIDETranscript(cwd, "test-session")
	if err != nil {
		t.Fatalf("ensureIDETranscript() error = %v", err)
	}

	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("read cached transcript: %v", err)
	}
	if !strings.Contains(string(data), "Hi!") {
		t.Fatalf("cached transcript should contain IDE content, got: %s", string(data))
	}
}

// --- Execution log enrichment tests ---

func TestConvertExecutionActionsToHistoryEntries_SayOnly(t *testing.T) {
	actions := []kiroExecutionAction{
		{ActionType: "model", ActionState: "Success"},
		{ActionType: "say", ActionState: "Success",
			Output: json.RawMessage(`{"message":"Hello, I created the file for you."}`)},
	}

	entries := convertExecutionActionsToHistoryEntries(actions)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}

	resp := extractLastAssistantResponse(entries)
	if resp != "Hello, I created the file for you." {
		t.Fatalf("response = %q, want %q", resp, "Hello, I created the file for you.")
	}
}

func TestConvertExecutionActionsToHistoryEntries_ToolCallsAndSay(t *testing.T) {
	actions := []kiroExecutionAction{
		{ActionType: "model", ActionState: "Success"},
		{ActionType: "create", ActionState: "Success",
			Input: json.RawMessage(`{"file":"hello.py","modifiedContent":"print('hello')"}`)},
		{ActionType: "model", ActionState: "Success"},
		{ActionType: "say", ActionState: "Success",
			Output: json.RawMessage(`{"message":"Done! Run it with python hello.py."}`)},
	}

	entries := convertExecutionActionsToHistoryEntries(actions)
	if len(entries) == 0 {
		t.Fatal("expected at least one entry")
	}

	// Should extract modified files
	files := extractModifiedFilesFromHistory(entries)
	if len(files) == 0 {
		t.Fatal("expected at least one modified file")
	}
	if files[0] != "hello.py" {
		t.Fatalf("files[0] = %q, want %q", files[0], "hello.py")
	}

	// Should extract the say response
	resp := extractLastAssistantResponse(entries)
	if resp != "Done! Run it with python hello.py." {
		t.Fatalf("response = %q", resp)
	}
}

func TestConvertExecutionActionsToHistoryEntries_MultipleToolCalls(t *testing.T) {
	actions := []kiroExecutionAction{
		{ActionType: "create", ActionState: "Success",
			Input: json.RawMessage(`{"file":"main.py","modifiedContent":"import os"}`)},
		{ActionType: "create", ActionState: "Success",
			Input: json.RawMessage(`{"file":"test_main.py","modifiedContent":"import pytest"}`)},
		{ActionType: "say", ActionState: "Success",
			Output: json.RawMessage(`{"message":"Created both files."}`)},
	}

	entries := convertExecutionActionsToHistoryEntries(actions)
	files := extractModifiedFilesFromHistory(entries)
	if len(files) != 2 {
		t.Fatalf("got %d files, want 2", len(files))
	}
	if files[0] != "main.py" || files[1] != "test_main.py" {
		t.Fatalf("files = %v", files)
	}
}

func TestConvertExecutionActionsToHistoryEntries_EditAction(t *testing.T) {
	actions := []kiroExecutionAction{
		{ActionType: "edit", ActionState: "Success",
			Input: json.RawMessage(`{"file":"app.go"}`)},
		{ActionType: "say", ActionState: "Success",
			Output: json.RawMessage(`{"message":"Updated app.go"}`)},
	}

	entries := convertExecutionActionsToHistoryEntries(actions)
	files := extractModifiedFilesFromHistory(entries)
	if len(files) != 1 || files[0] != "app.go" {
		t.Fatalf("files = %v, want [app.go]", files)
	}
}

func TestConvertExecutionActionsToHistoryEntries_NoActions(t *testing.T) {
	entries := convertExecutionActionsToHistoryEntries(nil)
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries for nil actions, got %d", len(entries))
	}
}

func TestExtractIDESessionID(t *testing.T) {
	data := []byte(`{"sessionId":"abc-123","history":[],"title":"test"}`)
	got := extractIDESessionID(data)
	if got != "abc-123" {
		t.Fatalf("extractIDESessionID() = %q, want %q", got, "abc-123")
	}
}

func TestExtractIDESessionID_Missing(t *testing.T) {
	data := []byte(`{"history":[]}`)
	got := extractIDESessionID(data)
	if got != "" {
		t.Fatalf("extractIDESessionID() = %q, want empty", got)
	}
}

func TestExtractIDEExecutionIDs(t *testing.T) {
	data := []byte(`{
		"sessionId":"sess-1",
		"history":[
			{"message":{"role":"user","content":"hello"}},
			{"message":{"role":"assistant","content":"On it."},"executionId":"exec-1"},
			{"message":{"role":"user","content":"more"}},
			{"message":{"role":"assistant","content":"On it."},"executionId":"exec-2"}
		]
	}`)
	ids := extractIDEExecutionIDs(data)
	if len(ids) != 2 || ids[0] != "exec-1" || ids[1] != "exec-2" {
		t.Fatalf("extractIDEExecutionIDs() = %v, want [exec-1 exec-2]", ids)
	}
}

func TestFindExecutionLogsForSession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Create a fake workspace hash dir with execution logs
	workspaceHash := "abcd1234abcd1234abcd1234abcd1234"
	execLogsDir := createExecLogsDir(t, home, workspaceHash)

	// Write execution log files
	writeExecutionLog(t, execLogsDir, "logfile1", kiroExecutionLog{
		ExecutionID:   "exec-1",
		ChatSessionID: "target-session",
		Status:        "succeed",
		Actions: []kiroExecutionAction{
			{ActionType: "say", ActionState: "Success",
				Output: json.RawMessage(`{"message":"Hello world"}`)},
		},
	})
	writeExecutionLog(t, execLogsDir, "logfile2", kiroExecutionLog{
		ExecutionID:   "exec-other",
		ChatSessionID: "other-session",
		Status:        "succeed",
	})

	logs, err := findExecutionLogsForSession("target-session")
	if err != nil {
		t.Fatalf("findExecutionLogsForSession() error = %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("got %d logs, want 1", len(logs))
	}
	if logs["exec-1"] == nil {
		t.Fatal("expected exec-1 in results")
	}
	if logs["exec-1"].Actions[0].ActionType != "say" {
		t.Fatalf("action type = %q, want say", logs["exec-1"].Actions[0].ActionType)
	}
}

func TestFindExecutionLogsForSession_NoMatch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	workspaceHash := "abcd1234abcd1234abcd1234abcd1234"
	execLogsDir := createExecLogsDir(t, home, workspaceHash)
	writeExecutionLog(t, execLogsDir, "logfile1", kiroExecutionLog{
		ExecutionID:   "exec-1",
		ChatSessionID: "other-session",
	})

	logs, err := findExecutionLogsForSession("nonexistent-session")
	if err != nil {
		t.Fatalf("findExecutionLogsForSession() error = %v", err)
	}
	if len(logs) != 0 {
		t.Fatalf("got %d logs, want 0", len(logs))
	}
}

func TestEnrichIDETranscriptWithExecutionLogs(t *testing.T) {
	ideData := []byte(`{
		"sessionId":"sess-1",
		"history":[
			{"message":{"role":"user","content":[{"type":"text","text":"create hello.py"}]}},
			{"message":{"role":"assistant","content":"On it."},"executionId":"exec-1"},
			{"message":{"role":"user","content":[{"type":"text","text":"now commit"}]}},
			{"message":{"role":"assistant","content":"On it."},"executionId":"exec-2"}
		]
	}`)

	execLogs := map[string]*kiroExecutionLog{
		"exec-1": {
			ExecutionID:   "exec-1",
			ChatSessionID: "sess-1",
			Actions: []kiroExecutionAction{
				{ActionType: "create", ActionState: "Success",
					Input: json.RawMessage(`{"file":"hello.py","modifiedContent":"print('hi')"}`)},
				{ActionType: "say", ActionState: "Success",
					Output: json.RawMessage(`{"message":"Created hello.py for you!"}`)},
			},
		},
		"exec-2": {
			ExecutionID:   "exec-2",
			ChatSessionID: "sess-1",
			Actions: []kiroExecutionAction{
				{ActionType: "say", ActionState: "Success",
					Output: json.RawMessage(`{"message":"Committed the changes."}`)},
			},
		},
	}

	enriched := enrichIDETranscriptWithExecutionLogs(ideData, execLogs)
	if enriched == nil {
		t.Fatal("enrichIDETranscriptWithExecutionLogs() returned nil")
	}

	// Parse as CLI transcript format
	transcript, err := parseTranscript(enriched)
	if err != nil {
		t.Fatalf("parseTranscript() error = %v", err)
	}

	// Should have 2 history entries (user/assistant pairs)
	if len(transcript.History) < 2 {
		t.Fatalf("got %d history entries, want >= 2", len(transcript.History))
	}

	// First turn should have the tool call (create hello.py)
	files := extractModifiedFilesFromHistory(transcript.History)
	if !slices.Contains(files, "hello.py") {
		t.Fatalf("expected hello.py in modified files, got %v", files)
	}

	// Should have the actual response text
	lastResp := extractLastAssistantResponse(transcript.History)
	if lastResp != "Committed the changes." {
		t.Fatalf("last response = %q, want %q", lastResp, "Committed the changes.")
	}
}

func TestEnsureIDETranscriptWithExecutionLogs(t *testing.T) {
	repoRoot := t.TempDir()
	home := t.TempDir()
	cwd := filepath.Join(repoRoot, "workspace")
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)
	t.Setenv("HOME", home)
	if err := os.MkdirAll(cwd, 0o750); err != nil {
		t.Fatalf("mkdir cwd: %v", err)
	}

	// Set up IDE workspace session with executionId on assistant entries
	sessionsDir := createIDEWorkspaceSessionsDir(t, home, cwd)
	index := `[{"sessionId":"ide-sess","title":"test","dateCreated":"2026-03-21T12:00:00Z"}]`
	if err := os.WriteFile(filepath.Join(sessionsDir, "sessions.json"), []byte(index), 0o600); err != nil {
		t.Fatalf("write sessions.json: %v", err)
	}
	ideTranscript := `{
		"sessionId":"ide-sess",
		"history":[
			{"message":{"role":"user","content":"create hello.py"}},
			{"message":{"role":"assistant","content":"On it."},"executionId":"exec-1"}
		]
	}`
	if err := os.WriteFile(filepath.Join(sessionsDir, "ide-sess.json"), []byte(ideTranscript), 0o600); err != nil {
		t.Fatalf("write IDE transcript: %v", err)
	}

	// Set up execution logs
	workspaceHash := "abcd1234abcd1234abcd1234abcd1234"
	execLogsDir := createExecLogsDir(t, home, workspaceHash)
	writeExecutionLog(t, execLogsDir, "log1", kiroExecutionLog{
		ExecutionID:   "exec-1",
		ChatSessionID: "ide-sess",
		Status:        "succeed",
		Actions: []kiroExecutionAction{
			{ActionType: "create", ActionState: "Success",
				Input: json.RawMessage(`{"file":"hello.py","modifiedContent":"print('hi')"}`)},
			{ActionType: "say", ActionState: "Success",
				Output: json.RawMessage(`{"message":"Created hello.py!"}`)},
		},
	})

	cachePath, err := New().ensureIDETranscript(cwd, "test-session")
	if err != nil {
		t.Fatalf("ensureIDETranscript() error = %v", err)
	}

	// Verify the cached transcript has real data
	files, _, err := New().ExtractModifiedFiles(cachePath, 0)
	if err != nil {
		t.Fatalf("ExtractModifiedFiles() error = %v", err)
	}
	if !slices.Contains(files, "hello.py") {
		t.Fatalf("expected hello.py in modified files, got %v", files)
	}

	summary, hasSummary, err := New().ExtractSummary(cachePath)
	if err != nil {
		t.Fatalf("ExtractSummary() error = %v", err)
	}
	if !hasSummary || summary != "Created hello.py!" {
		t.Fatalf("summary = %q, hasSummary = %v", summary, hasSummary)
	}
}

func TestEnsureIDETranscriptFallsBackToToolCallsWhenNoExecLogs(t *testing.T) {
	repoRoot := t.TempDir()
	home := t.TempDir()
	cwd := filepath.Join(repoRoot, "workspace")
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)
	t.Setenv("HOME", home)
	if err := os.MkdirAll(cwd, 0o750); err != nil {
		t.Fatalf("mkdir cwd: %v", err)
	}

	sessionsDir := createIDEWorkspaceSessionsDir(t, home, cwd)
	index := `[{"sessionId":"ide-sess","title":"test","dateCreated":"2026-03-21T12:00:00Z"}]`
	if err := os.WriteFile(filepath.Join(sessionsDir, "sessions.json"), []byte(index), 0o600); err != nil {
		t.Fatalf("write sessions.json: %v", err)
	}
	ideTranscript := `{"sessionId":"ide-sess","history":[{"message":{"role":"user","content":"create hello"}},{"message":{"role":"assistant","content":"On it."}}]}`
	if err := os.WriteFile(filepath.Join(sessionsDir, "ide-sess.json"), []byte(ideTranscript), 0o600); err != nil {
		t.Fatalf("write IDE transcript: %v", err)
	}

	// No execution logs dir — seed JSONL tool calls as fallback
	toolCallsDir := filepath.Join(repoRoot, ".entire", "tmp")
	if err := os.MkdirAll(toolCallsDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	toolCallLine := `{"name":"fs_write","args":{"path":"hello.py"}}` + "\n"
	if err := os.WriteFile(filepath.Join(toolCallsDir, toolCallsFile), []byte(toolCallLine), 0o600); err != nil {
		t.Fatalf("write tool calls: %v", err)
	}

	cachePath, err := New().ensureIDETranscript(cwd, "test-session")
	if err != nil {
		t.Fatalf("ensureIDETranscript() error = %v", err)
	}

	// Should still find the tool call via JSONL fallback
	files, _, err := New().ExtractModifiedFiles(cachePath, 0)
	if err != nil {
		t.Fatalf("ExtractModifiedFiles() error = %v", err)
	}
	if !slices.Contains(files, "hello.py") {
		t.Fatalf("expected hello.py from JSONL fallback, got %v", files)
	}
}

func TestEnsureIDETranscriptTrimsWithOffset(t *testing.T) {
	repoRoot := t.TempDir()
	home := t.TempDir()
	cwd := filepath.Join(repoRoot, "workspace")
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)
	t.Setenv("HOME", home)
	if err := os.MkdirAll(cwd, 0o750); err != nil {
		t.Fatalf("mkdir cwd: %v", err)
	}

	sessionsDir := createIDEWorkspaceSessionsDir(t, home, cwd)
	index := `[{"sessionId":"ide-sess","title":"test","dateCreated":"2026-03-21T09:00:00Z"}]`
	if err := os.WriteFile(filepath.Join(sessionsDir, "sessions.json"), []byte(index), 0o600); err != nil {
		t.Fatalf("write sessions.json: %v", err)
	}

	// IDE transcript with 4 user/assistant pairs (cumulative conversation).
	ideTranscript := `{"history":[
		{"message":{"role":"user","content":"prompt 0"}},{"message":{"role":"assistant","content":"response 0"}},
		{"message":{"role":"user","content":"prompt 1"}},{"message":{"role":"assistant","content":"response 1"}},
		{"message":{"role":"user","content":"prompt 2"}},{"message":{"role":"assistant","content":"response 2"}},
		{"message":{"role":"user","content":"prompt 3"}},{"message":{"role":"assistant","content":"response 3"}}
	]}`
	if err := os.WriteFile(filepath.Join(sessionsDir, "ide-sess.json"), []byte(ideTranscript), 0o600); err != nil {
		t.Fatalf("write IDE transcript: %v", err)
	}

	// Seed offset at position 2 — first 2 pairs already checkpointed.
	// IDE transcripts have empty conversation_id after conversion.
	seedTranscriptOffset(t, repoRoot, "", 2)

	cachePath, err := New().ensureIDETranscript(cwd, "test-session")
	if err != nil {
		t.Fatalf("ensureIDETranscript() error = %v", err)
	}

	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("read cached IDE transcript: %v", err)
	}

	// trimTranscriptHistory re-serializes in CLI format, so use parseTranscript.
	result, err := parseTranscript(data)
	if err != nil {
		t.Fatalf("parse cached transcript: %v", err)
	}

	// Should only contain the 2 new pairs (entries 2-3), not all 4.
	if len(result.History) != 2 {
		t.Fatalf("history length = %d, want 2 (trimmed entries 2-3)", len(result.History))
	}

	firstPrompt := extractUserPrompt(result.History[0].User.Content)
	if firstPrompt != "prompt 2" {
		t.Fatalf("first prompt = %q, want %q", firstPrompt, "prompt 2")
	}

	// Offset should be updated to 4 (total length).
	offset := readTestTranscriptOffset(t, repoRoot)
	if offset.Position != 4 {
		t.Fatalf("offset position = %d, want 4", offset.Position)
	}
}

func TestEnsureIDETranscriptFirstCapture(t *testing.T) {
	repoRoot := t.TempDir()
	home := t.TempDir()
	cwd := filepath.Join(repoRoot, "workspace")
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)
	t.Setenv("HOME", home)
	if err := os.MkdirAll(cwd, 0o750); err != nil {
		t.Fatalf("mkdir cwd: %v", err)
	}

	sessionsDir := createIDEWorkspaceSessionsDir(t, home, cwd)
	index := `[{"sessionId":"ide-sess","title":"test","dateCreated":"2026-03-21T09:00:00Z"}]`
	if err := os.WriteFile(filepath.Join(sessionsDir, "sessions.json"), []byte(index), 0o600); err != nil {
		t.Fatalf("write sessions.json: %v", err)
	}

	ideTranscript := `{"history":[
		{"message":{"role":"user","content":"prompt 0"}},{"message":{"role":"assistant","content":"response 0"}},
		{"message":{"role":"user","content":"prompt 1"}},{"message":{"role":"assistant","content":"response 1"}}
	]}`
	if err := os.WriteFile(filepath.Join(sessionsDir, "ide-sess.json"), []byte(ideTranscript), 0o600); err != nil {
		t.Fatalf("write IDE transcript: %v", err)
	}

	// No offset file — first capture should return full transcript and create offset.
	cachePath, err := New().ensureIDETranscript(cwd, "test-session")
	if err != nil {
		t.Fatalf("ensureIDETranscript() error = %v", err)
	}

	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("read cached IDE transcript: %v", err)
	}

	result, err := parseTranscript(data)
	if err != nil {
		t.Fatalf("parse cached transcript: %v", err)
	}

	if len(result.History) != 2 {
		t.Fatalf("history length = %d, want 2 (full transcript)", len(result.History))
	}

	// Offset file should be created with position 2.
	offset := readTestTranscriptOffset(t, repoRoot)
	if offset.Position != 2 {
		t.Fatalf("offset position = %d, want 2", offset.Position)
	}
}

func TestEnsureIDETranscriptNoNewEntriesSucceeds(t *testing.T) {
	repoRoot := t.TempDir()
	home := t.TempDir()
	cwd := filepath.Join(repoRoot, "workspace")
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)
	t.Setenv("HOME", home)
	if err := os.MkdirAll(cwd, 0o750); err != nil {
		t.Fatalf("mkdir cwd: %v", err)
	}

	sessionsDir := createIDEWorkspaceSessionsDir(t, home, cwd)
	index := `[{"sessionId":"ide-sess","title":"test","dateCreated":"2026-03-21T09:00:00Z"}]`
	if err := os.WriteFile(filepath.Join(sessionsDir, "sessions.json"), []byte(index), 0o600); err != nil {
		t.Fatalf("write sessions.json: %v", err)
	}

	ideTranscript := `{"history":[
		{"message":{"role":"user","content":"prompt 0"}},{"message":{"role":"assistant","content":"response 0"}}
	]}`
	if err := os.WriteFile(filepath.Join(sessionsDir, "ide-sess.json"), []byte(ideTranscript), 0o600); err != nil {
		t.Fatalf("write IDE transcript: %v", err)
	}

	// Offset already at position 1 — no new entries since last checkpoint.
	// trimTranscriptHistory returns (nil, false) when no new entries, so the
	// full transcript is kept (same behavior as ensureCachedTranscript).
	seedTranscriptOffset(t, repoRoot, "", 1)

	cachePath, err := New().ensureIDETranscript(cwd, "test-session")
	if err != nil {
		t.Fatalf("ensureIDETranscript() should succeed with full transcript when no new entries, got error: %v", err)
	}

	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("read cached IDE transcript: %v", err)
	}

	result, err := parseTranscript(data)
	if err != nil {
		t.Fatalf("parse cached transcript: %v", err)
	}

	// Full transcript returned (no new entries = no trimming).
	if len(result.History) != 1 {
		t.Fatalf("history length = %d, want 1 (full transcript, no new entries)", len(result.History))
	}
}

// --- Test helpers for execution logs ---

func createExecLogsDir(t *testing.T, home string, workspaceHash string) string {
	t.Helper()
	dir := filepath.Join(kiroExtensionTestDir(t, home), workspaceHash, execLogsSubdir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir exec logs dir: %v", err)
	}
	return dir
}

func writeExecutionLog(t *testing.T, dir string, filename string, log kiroExecutionLog) {
	t.Helper()
	data, err := json.Marshal(log)
	if err != nil {
		t.Fatalf("marshal execution log: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, filename), data, 0o600); err != nil {
		t.Fatalf("write execution log: %v", err)
	}
}
