package devin

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// setupSessionsDB creates a temporary Devin sessions.db with the live schema
// and returns the path to the data directory (suitable for XDG_DATA_HOME).
func setupSessionsDB(t *testing.T) (string, *sql.DB) {
	t.Helper()
	dataDir := t.TempDir()
	dbDir := filepath.Join(dataDir, "devin", "cli")
	if err := os.MkdirAll(dbDir, 0o750); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dbDir, "sessions.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	schema := `
CREATE TABLE IF NOT EXISTS sessions (
  id TEXT PRIMARY KEY,
  working_directory TEXT NOT NULL,
  backend_type TEXT NOT NULL,
  model TEXT NOT NULL,
  agent_mode TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  last_activity_at INTEGER NOT NULL,
  title TEXT,
  main_chain_id INTEGER,
  shell_last_seen_index INTEGER DEFAULT 0,
  cogs_json TEXT,
  workspace_dirs TEXT,
  hidden INTEGER NOT NULL DEFAULT 0,
  metadata TEXT
);
CREATE TABLE IF NOT EXISTS message_nodes (
  row_id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL,
  node_id INTEGER NOT NULL,
  parent_node_id INTEGER,
  chat_message TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  metadata TEXT,
  FOREIGN KEY (session_id) REFERENCES sessions(id),
  UNIQUE(session_id, node_id)
);`
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	return dataDir, db
}

func insertSession(t *testing.T, db *sql.DB, id, model string, mainChainID int) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO sessions (id, working_directory, backend_type, model, agent_mode, created_at, last_activity_at, title, main_chain_id, metadata)
		 VALUES (?, '/tmp/repo', 'local', ?, 'auto', 1700000000000, 1700000000000, 'test', ?, '{}')`,
		id, model, mainChainID,
	)
	if err != nil {
		t.Fatal(err)
	}
}

func insertNode(t *testing.T, db *sql.DB, sessionID string, nodeID, parentID int, chat, metadata string) {
	t.Helper()
	var parent any
	if parentID != 0 {
		parent = parentID
	}
	_, err := db.Exec(
		`INSERT INTO message_nodes (session_id, node_id, parent_node_id, chat_message, created_at, metadata)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		sessionID, nodeID, parent, chat, 1700000000000+int64(nodeID)*1000, metadata,
	)
	if err != nil {
		t.Fatal(err)
	}
}

func TestMaterializeLiveTranscript(t *testing.T) {
	dataDir, db := setupSessionsDB(t)
	t.Setenv("XDG_DATA_HOME", dataDir)
	transcriptDir := t.TempDir()
	t.Setenv("ENTIRE_TEST_DEVIN_TRANSCRIPT_DIR", transcriptDir)

	sessionID := "snowy-efraasia"
	insertSession(t, db, sessionID, "SWE-1.7", 3)

	userMsg := `{"role":"user","content":"Create hello.txt","metadata":{"is_user_input":true,"num_tokens":10}}`
	agentMsg := `{"role":"assistant","content":"","tool_calls":[{"id":"write_0","function":{"name":"write","arguments":"{\"file_path\":\"/repo/hello.txt\",\"content\":\"hi\"}"}}],"metadata":{"num_tokens":20}}`
	toolMsg := `{"role":"tool","content":"success","tool_call_id":"write_0","metadata":{"num_tokens":5}}`

	insertNode(t, db, sessionID, 1, 0, userMsg, "")
	insertNode(t, db, sessionID, 2, 1, agentMsg, "")
	insertNode(t, db, sessionID, 3, 2, toolMsg, "")

	d := New()
	sessionRef := filepath.Join(transcriptDir, sessionID+".json")
	if err := d.materializeLiveTranscript(sessionRef); err != nil {
		t.Fatalf("materializeLiveTranscript: %v", err)
	}

	data, err := os.ReadFile(sessionRef)
	if err != nil {
		t.Fatalf("read transcript: %v", err)
	}

	tx, err := parseTranscript(data)
	if err != nil {
		t.Fatalf("parse transcript: %v", err)
	}

	if tx.SchemaVersion != "ATIF-v1.7" {
		t.Errorf("schema_version = %q, want ATIF-v1.7", tx.SchemaVersion)
	}
	if tx.SessionID != sessionID {
		t.Errorf("session_id = %q, want %q", tx.SessionID, sessionID)
	}
	if len(tx.Steps) != 3 {
		t.Fatalf("steps = %d, want 3", len(tx.Steps))
	}

	var step0 map[string]any
	if err := json.Unmarshal(tx.Steps[0], &step0); err != nil {
		t.Fatal(err)
	}
	if step0["source"] != "user" || step0["message"] != "Create hello.txt" {
		t.Errorf("step0 = %+v", step0)
	}

	var step1 map[string]any
	if err := json.Unmarshal(tx.Steps[1], &step1); err != nil {
		t.Fatal(err)
	}
	if step1["source"] != "agent" {
		t.Errorf("step1 source = %q, want agent", step1["source"])
	}
	toolCalls, ok := step1["tool_calls"].([]any)
	if !ok || len(toolCalls) != 1 {
		t.Fatalf("step1 tool_calls = %+v", step1["tool_calls"])
	}
	call0 := toolCalls[0].(map[string]any)
	if call0["function_name"] != "write" {
		t.Errorf("function_name = %q, want write", call0["function_name"])
	}
	args := call0["arguments"].(map[string]any)
	if args["file_path"] != "/repo/hello.txt" {
		t.Errorf("arguments.file_path = %q", args["file_path"])
	}

	var step2 map[string]any
	if err := json.Unmarshal(tx.Steps[2], &step2); err != nil {
		t.Fatal(err)
	}
	if step2["source"] != "tool" || step2["message"] != "success" {
		t.Errorf("step2 = %+v", step2)
	}

	pos, err := d.GetTranscriptPosition(sessionRef)
	if err != nil || pos != 3 {
		t.Errorf("position = %d, %v; want 3, nil", pos, err)
	}

	files, pos, err := d.ExtractModifiedFiles(sessionRef, 0)
	if err != nil {
		t.Fatal(err)
	}
	if pos != 3 {
		t.Errorf("extract position = %d, want 3", pos)
	}
	if len(files) != 1 || files[0] != "/repo/hello.txt" {
		t.Errorf("files = %v, want [/repo/hello.txt]", files)
	}
}

func TestMaterializeLiveTranscript_FallsBackToStubWhenSessionMissing(t *testing.T) {
	dataDir, _ := setupSessionsDB(t)
	t.Setenv("XDG_DATA_HOME", dataDir)
	transcriptDir := t.TempDir()
	t.Setenv("ENTIRE_TEST_DEVIN_TRANSCRIPT_DIR", transcriptDir)

	d := New()
	sessionRef := filepath.Join(transcriptDir, "brisk-otter.json")
	if err := d.PrepareTranscript(sessionRef); err != nil {
		t.Fatalf("PrepareTranscript: %v", err)
	}

	tx, err := parseTranscriptFile(sessionRef)
	if err != nil {
		t.Fatal(err)
	}
	if tx.SessionID != "brisk-otter" {
		t.Errorf("session_id = %q, want brisk-otter", tx.SessionID)
	}
	if len(tx.Steps) != 0 {
		t.Errorf("steps = %d, want 0", len(tx.Steps))
	}
}

func parseTranscriptFile(path string) (*ATIFTranscript, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseTranscript(data)
}

func TestBuildSteps_FollowsMainChain(t *testing.T) {
	dataDir, db := setupSessionsDB(t)
	t.Setenv("XDG_DATA_HOME", dataDir)
	transcriptDir := t.TempDir()
	t.Setenv("ENTIRE_TEST_DEVIN_TRANSCRIPT_DIR", transcriptDir)

	sessionID := "chain-test"
	insertSession(t, db, sessionID, "SWE-1.7", 5)
	for i := 1; i <= 5; i++ {
		parent := i - 1
		if i == 1 {
			parent = 0
		}
		chat := `{"role":"user","content":"prompt"}`
		if i%2 == 0 {
			chat = `{"role":"assistant","content":"ok"}`
		}
		insertNode(t, db, sessionID, i, parent, chat, "")
	}

	d := New()
	sessionRef := filepath.Join(transcriptDir, sessionID+".json")
	if err := d.materializeLiveTranscript(sessionRef); err != nil {
		t.Fatal(err)
	}

	tx, err := parseTranscriptFile(sessionRef)
	if err != nil {
		t.Fatal(err)
	}
	if len(tx.Steps) != 5 {
		t.Fatalf("steps = %d, want 5", len(tx.Steps))
	}
	for i, step := range tx.Steps {
		var s map[string]any
		if err := json.Unmarshal(step, &s); err != nil {
			t.Fatal(err)
		}
		want := "user"
		if (i+1)%2 == 0 {
			want = "agent"
		}
		if s["source"] != want {
			t.Errorf("step %d source = %q, want %q", i+1, s["source"], want)
		}
	}
}
