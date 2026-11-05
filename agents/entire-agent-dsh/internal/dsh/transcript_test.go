package dsh

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodeZstdTolerantFull(t *testing.T) {
	session := newTestSession(t, "session-decode", "/repo/decode", []map[string]any{
		eventRecord(t, "turn/start", 0, 1700000001000, map[string]any{"turn": 1}),
	})
	data, err := readTranscriptFile(session.NativePath)
	if err != nil {
		t.Fatalf("readTranscriptFile: %v", err)
	}
	if !bytes.Contains(data, []byte(`"type":"session"`)) {
		t.Fatalf("decoded transcript missing header: %q", string(data))
	}
	if !bytes.Contains(data, []byte(`"type":"turn/start"`)) {
		t.Fatalf("decoded transcript missing event: %q", string(data))
	}
}

func TestDecodeZstdTolerantTornTail(t *testing.T) {
	session := newTestSession(t, "session-torn", "/repo/torn", []map[string]any{
		eventRecord(t, "turn/start", 0, 1700000001000, map[string]any{"turn": 1}),
	})
	raw, err := os.ReadFile(session.NativePath)
	if err != nil {
		t.Fatalf("read native: %v", err)
	}
	if len(raw) < 12 {
		t.Fatalf("native transcript unexpectedly short: %d", len(raw))
	}
	torn := raw[:len(raw)-8]
	decoded, err := decodeZstdTolerant(torn)
	if err != nil {
		t.Fatalf("decode torn transcript: %v", err)
	}
	if !bytes.Contains(decoded, []byte(`"type":"session"`)) {
		t.Fatalf("torn decode lost the header: %q", string(decoded))
	}
}

func TestMaterializeTranscript(t *testing.T) {
	session := newTestSession(t, "session-materialize", "/repo/materialize", []map[string]any{
		eventRecord(t, "turn/start", 0, 1700000001000, map[string]any{"turn": 1}),
		eventRecord(t, "user/message", 1, 1700000001100, userMessage(t, "m1", "hello", "user", "")),
	})
	ref, data, err := (&Agent{}).materializeTranscript(session.SessionID, session.CWD, "", materializeOptions{})
	if err != nil {
		t.Fatalf("materializeTranscript: %v", err)
	}
	if ref != session.CacheRef {
		t.Errorf("ref = %q, want %q", ref, session.CacheRef)
	}
	if len(data) == 0 {
		t.Fatal("materialized transcript is empty")
	}
	cached, err := os.ReadFile(ref)
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	if !bytes.Equal(bytes.TrimSpace(cached), bytes.TrimSpace(data)) {
		t.Error("cache bytes differ from returned bytes")
	}
	if countTranscriptLines(data) != 3 {
		t.Errorf("line count = %d, want 3", countTranscriptLines(data))
	}
}

func TestMaterializeTranscriptRefreshes(t *testing.T) {
	events := []map[string]any{
		eventRecord(t, "turn/start", 0, 1700000001000, map[string]any{"turn": 1}),
	}
	session := newTestSession(t, "session-refresh", "/repo/refresh", events)
	if _, _, err := (&Agent{}).materializeTranscript(session.SessionID, session.CWD, "", materializeOptions{}); err != nil {
		t.Fatalf("first materialize: %v", err)
	}
	first, err := os.ReadFile(session.CacheRef)
	if err != nil {
		t.Fatalf("read first cache: %v", err)
	}

	events = append(events, eventRecord(t, "user/message", 1, 1700000001100, userMessage(t, "m1", "later", "user", "")))
	writeNativeSession(t, session.DSHHome, session.SessionID, session.CWD, events)
	if _, _, err := (&Agent{}).materializeTranscript(session.SessionID, session.CWD, "", materializeOptions{}); err != nil {
		t.Fatalf("second materialize: %v", err)
	}
	second, err := os.ReadFile(session.CacheRef)
	if err != nil {
		t.Fatalf("read second cache: %v", err)
	}
	if len(second) <= len(first) || !strings.Contains(string(second), "later") {
		t.Error("cache was not refreshed from the newer native log")
	}
}

func TestCountTranscriptLines(t *testing.T) {
	data := []byte("{\"type\":\"session\"}\n\n{\"type\":\"turn/start\"}\n")
	if got := countTranscriptLines(data); got != 2 {
		t.Errorf("countTranscriptLines = %d, want 2", got)
	}
}

func TestParseSessionHeader(t *testing.T) {
	data := []byte("{\"type\":\"session\",\"version\":4,\"id\":\"s1\",\"createdAt\":1700000000000,\"cwd\":\"/repo\",\"isSeeded\":false,\"delegationDepth\":0}\n")
	header, err := parseSessionHeader(data)
	if err != nil {
		t.Fatalf("parseSessionHeader: %v", err)
	}
	if header.ID != "s1" || header.Version != 4 || header.CWD != "/repo" {
		t.Errorf("unexpected header: %+v", header)
	}
	if _, err := parseSessionHeader([]byte("{\"type\":\"other\"}\n")); err == nil {
		t.Error("expected a non-session header to fail")
	}
}

func TestMaterializeUnsafeSessionID(t *testing.T) {
	session := newTestSession(t, "../evil", "/repo/unsafe", []map[string]any{
		eventRecord(t, "turn/start", 0, 1700000001000, map[string]any{"turn": 1}),
	})
	t.Setenv("ENTIRE_REPO_ROOT", session.CWD)
	ref, data, err := (&Agent{}).materializeTranscript(session.SessionID, session.CWD, "", materializeOptions{})
	if err != nil {
		t.Fatalf("materialize unsafe id: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("materialized transcript is empty")
	}
	dir, err := (&Agent{}).GetSessionDir(session.CWD)
	if err != nil {
		t.Fatalf("GetSessionDir: %v", err)
	}
	if !pathWithin(ref, dir) {
		t.Errorf("unsafe session ref %q escaped %q", ref, dir)
	}
	if strings.Contains(filepath.Base(ref), "..") || strings.ContainsAny(filepath.Base(ref), `/\`) {
		t.Errorf("unsafe session filename %q", filepath.Base(ref))
	}
	id, ok := sessionIDFromRef(ref)
	if !ok || id != "../evil" {
		t.Errorf("sessionIDFromRef = %q,%v; want ../evil,true", id, ok)
	}
	if err := (&Agent{}).PrepareTranscript(ref); err != nil {
		t.Errorf("PrepareTranscript unsafe id: %v", err)
	}
}

func TestCacheMetaNativePathMismatchRefreshes(t *testing.T) {
	session := newTestSession(t, "session-meta", "/repo/meta", []map[string]any{
		eventRecord(t, "turn/start", 0, 1700000001000, map[string]any{"turn": 1}),
	})
	if _, _, err := (&Agent{}).materializeTranscript(session.SessionID, session.CWD, "", materializeOptions{}); err != nil {
		t.Fatalf("materialize: %v", err)
	}
	metaRaw, err := os.ReadFile(metaPathFor(session.CacheRef))
	if err != nil {
		t.Fatalf("read meta: %v", err)
	}
	var meta cacheMeta
	if err := json.Unmarshal(metaRaw, &meta); err != nil {
		t.Fatalf("parse meta: %v", err)
	}
	meta.NativePath = "/somewhere/else/session.v4.jsonl.zstd"
	rewritten, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("marshal meta: %v", err)
	}
	if err := os.WriteFile(metaPathFor(session.CacheRef), rewritten, 0o600); err != nil {
		t.Fatalf("rewrite meta: %v", err)
	}
	if err := os.WriteFile(session.CacheRef, []byte("stale\n"), 0o600); err != nil {
		t.Fatalf("seed stale cache: %v", err)
	}

	if _, data, err := (&Agent{}).materializeTranscript(session.SessionID, session.CWD, "", materializeOptions{}); err != nil {
		t.Fatalf("second materialize: %v", err)
	} else if bytes.Contains(data, []byte("stale")) {
		t.Error("materialize served the cache despite a different native path")
	}
}
