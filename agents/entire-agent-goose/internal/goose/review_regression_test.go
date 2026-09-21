package goose

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExportRejectsSessionPathTraversal(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(dir, "victim.json")
	if err := os.WriteFile(victim, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &stubRunner{export: []byte(exportFixture)}
	a := testAgent(t, runner)
	if err := a.exportSession("../victim", filepath.Join(dir, "safe.json")); err == nil {
		t.Error("expected invalid session ID error")
	}
	data, err := os.ReadFile(victim)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "keep" {
		t.Error("export overwrote a file outside its scratch directory")
	}
	if len(runner.calls) != 0 {
		t.Errorf("export invoked for invalid ID: %v", runner.calls)
	}
}

func TestEmptyExportRetainsSessionMetadata(t *testing.T) {
	runner := &stubRunner{export: []byte(`{"id":"empty","name":"New session","model_config":{"model_name":"test-model"},"conversation":[]}`)}
	a := testAgent(t, runner)
	path := filepath.Join(t.TempDir(), "empty.json")
	if err := a.exportSession("empty", path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	export, err := parseGooseExport(data)
	if err != nil {
		t.Fatal(err)
	}
	if export.ID != "empty" || export.Name != "New session" {
		t.Errorf("lost metadata: %+v", export)
	}
	if modelFromSessionRef(path) != "test-model" {
		t.Error("lost model")
	}
}
