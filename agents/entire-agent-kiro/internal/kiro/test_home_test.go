package kiro

import (
	"path/filepath"
	"testing"
)

// setupTestKiroHome isolates both native and simulated Windows data lookups.
func setupTestKiroHome(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
}

func TestSetupTestKiroHomeWindowsPaths(t *testing.T) {
	withRuntimeGOOS(t, "windows")
	home := t.TempDir()
	setupTestKiroHome(t, home)

	db, err := kiroCLIDataDBPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := expectedCLIKiroDBPath(home); db != want {
		t.Fatalf("CLI database path = %q, want %q", db, want)
	}
	storage, err := kiroExtensionStorageDir()
	if err != nil {
		t.Fatal(err)
	}
	if want := kiroExtensionTestDir(t, home); storage != want {
		t.Fatalf("extension storage directory = %q, want %q", storage, want)
	}
}
