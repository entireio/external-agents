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
	// No start time cached, so full transcript is written (re-serialized has no whitespace changes for empty history)
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
			return []byte(`{"conversation_id":"cli-session","history":[]}`), nil
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
		return []byte(`{"conversation_id":"target-conv","history":[]}`), nil
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
