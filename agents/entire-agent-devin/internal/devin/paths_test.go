package devin

import (
	"path/filepath"
	"testing"
)

func TestResolveSessionFile_SanitizesSessionID(t *testing.T) {
	t.Parallel()
	d := New()
	sessionDir := t.TempDir()

	got := d.ResolveSessionFile(sessionDir, "../../etc/passwd")
	want := filepath.Join(sessionDir, ".._.._etc_passwd.json")
	if got != want {
		t.Errorf("ResolveSessionFile = %q, want %q", got, want)
	}
	if filepath.Dir(got) != sessionDir {
		t.Errorf("resolved path escapes sessionDir: %q", got)
	}

	got = d.ResolveSessionFile(sessionDir, "")
	want = filepath.Join(sessionDir, "unknown.json")
	if got != want {
		t.Errorf("ResolveSessionFile empty = %q, want %q", got, want)
	}
}
