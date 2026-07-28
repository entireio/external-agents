package omp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetSessionDirRoots(t *testing.T) {
	home := t.TempDir()
	repo := filepath.Join(home, "src", "project")
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("PI_CODING_AGENT_DIR", "")
	t.Setenv("PI_CONFIG_DIR", ".custom-omp")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("OMP_PROFILE", "")
	t.Setenv("PI_PROFILE", "")

	got, err := New().GetSessionDir(repo)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".custom-omp", "agent", "sessions", "-src-project")
	if got != want {
		t.Fatalf("custom config session dir = %q, want %q", got, want)
	}

	xdg := filepath.Join(home, "xdg")
	profileRoot := filepath.Join(xdg, "omp", "profiles", "work")
	if err := os.MkdirAll(profileRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PI_CONFIG_DIR", "")
	t.Setenv("XDG_DATA_HOME", xdg)
	t.Setenv("OMP_PROFILE", "work")
	t.Setenv("PI_PROFILE", "")
	got, err = New().GetSessionDir(repo)
	if err != nil {
		t.Fatal(err)
	}
	want = filepath.Join(profileRoot, "sessions", "-src-project")
	if got != want {
		t.Fatalf("profile XDG session dir = %q, want %q", got, want)
	}
}

func TestOMPSessionsRootPlatformAndXDGSelection(t *testing.T) {
	t.Parallel()

	home := filepath.Join(string(filepath.Separator), "home", "user")
	xdg := filepath.Join(string(filepath.Separator), "xdg", "data")
	appRoot := filepath.Join(xdg, "omp")
	workXDG := filepath.Join(appRoot, "profiles", "work")

	tests := []struct {
		name      string
		goos      string
		agentDir  string
		configDir string
		xdgData   string
		omp       envValue
		pi        envValue
		existing  string
		want      string
	}{
		{"default Linux XDG", "linux", "", "", xdg, envValue{}, envValue{}, appRoot, filepath.Join(appRoot, "sessions")},
		{"default macOS XDG", "darwin", "", "", xdg, envValue{}, envValue{}, appRoot, filepath.Join(appRoot, "sessions")},
		{"inactive Linux XDG", "linux", "", "", xdg, envValue{}, envValue{}, "", filepath.Join(home, ".omp", "agent", "sessions")},
		{"Windows ignores active XDG", "windows", "", "", xdg, envValue{}, envValue{}, appRoot, filepath.Join(home, ".omp", "agent", "sessions")},
		{"custom config root", "linux", "", ".custom-omp", "", envValue{}, envValue{}, "", filepath.Join(home, ".custom-omp", "agent", "sessions")},
		{"agent override takes priority", "linux", filepath.Join(home, "agent"), ".custom-omp", xdg, envValue{}, envValue{}, appRoot, filepath.Join(home, "agent", "sessions")},
		{"default agent override still permits XDG", "linux", filepath.Join(home, ".omp", "agent"), "", xdg, envValue{}, envValue{}, appRoot, filepath.Join(appRoot, "sessions")},
		{"named profile home", "linux", "", "", xdg, envValue{"work", true}, envValue{}, "", filepath.Join(home, ".omp", "profiles", "work", "agent", "sessions")},
		{"named profile XDG", "linux", "", "", xdg, envValue{"work", true}, envValue{}, workXDG, filepath.Join(workXDG, "sessions")},
		{"named profile ignores custom agent", "linux", filepath.Join(home, "custom-agent"), "", "", envValue{"work", true}, envValue{}, "", filepath.Join(home, ".omp", "profiles", "work", "agent", "sessions")},
		{"PI profile fallback", "linux", "", "", "", envValue{}, envValue{"work", true}, "", filepath.Join(home, ".omp", "profiles", "work", "agent", "sessions")},
		{"OMP_PROFILE precedence", "linux", "", "", "", envValue{"personal", true}, envValue{"work", true}, "", filepath.Join(home, ".omp", "profiles", "personal", "agent", "sessions")},
		{"explicit empty OMP_PROFILE selects default", "linux", "", "", "", envValue{"", true}, envValue{"work", true}, "", filepath.Join(home, ".omp", "agent", "sessions")},
		{"default sentinel selects default", "linux", "", "", "", envValue{"default", true}, envValue{"work", true}, "", filepath.Join(home, ".omp", "agent", "sessions")},
		{"inherited profile agent suppressed", "linux", filepath.Join(home, ".omp", "profiles", "work", "agent"), "", "", envValue{"", true}, envValue{"work", true}, "", filepath.Join(home, ".omp", "agent", "sessions")},
		{"default profile custom override", "linux", filepath.Join(home, "custom-agent"), "", "", envValue{"default", true}, envValue{"work", true}, "", filepath.Join(home, "custom-agent", "sessions")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := ompSessionsRootFor(
				test.goos,
				home,
				test.agentDir,
				test.configDir,
				test.xdgData,
				test.omp,
				test.pi,
				func(path string) bool { return path == test.existing },
			)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("session root = %q, want %q", got, test.want)
			}
		})
	}
}

func TestEncodeSessionDirName(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	insideHome := filepath.Join(home, "src", "repo")
	if err := os.MkdirAll(insideHome, 0o700); err != nil {
		t.Fatal(err)
	}

	// Canonicalize the temp base so the expectations hold on platforms where
	// os.TempDir() is itself a symlink (e.g. /var -> /private/var on macOS).
	tmp, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct{ path, want string }{
		{home, "-"},
		{insideHome, "-src-repo"},
		{tmp, "-tmp"},
		{filepath.Join(tmp, "omp", "repo"), "-tmp-omp-repo"},
		{filepath.Join(string(filepath.Separator), "opt", "omp", "repo"), "--opt-omp-repo--"},
	}
	for _, test := range tests {
		got, err := encodeSessionDirName(test.path)
		if err != nil {
			t.Errorf("encodeSessionDirName(%q) error = %v", test.path, err)
			continue
		}
		if got != test.want {
			t.Errorf("encodeSessionDirName(%q) = %q, want %q", test.path, got, test.want)
		}
	}
}

func TestEncodeSessionDirNameCanonicalizesSymlinks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	target := filepath.Join(home, "real", "repo")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(home, "alias")
	if err := os.Symlink(filepath.Join(home, "real"), alias); err != nil {
		t.Fatal(err)
	}
	got, err := encodeSessionDirName(filepath.Join(alias, "repo"))
	if err != nil {
		t.Fatal(err)
	}
	if got != "-real-repo" {
		t.Fatalf("encoded symlink cwd = %q, want -real-repo", got)
	}
}

func TestParseSessionHeader(t *testing.T) {
	headerLine := `{"type":"session","version":3,"id":"session-1","timestamp":"2026-07-26T00:00:00Z","cwd":"/repo"}`
	title := titleSlotLine(t)
	tests := []struct {
		name    string
		data    string
		wantID  string
		wantErr string
	}{
		{"legacy physical layout", headerLine + "\n", "session-1", ""},
		{"fixed title slot", title + headerLine + "\n", "session-1", ""},
		{"missing header after title", title, "", "not followed"},
		{"wrong type", `{"type":"message","version":3,"id":"x"}` + "\n", "", "type is"},
		{"missing id", `{"type":"session","version":3}` + "\n", "", "id is empty"},
		{"missing version", `{"type":"session","id":"x"}` + "\n", "", "positive"},
		{"malformed", "not-json\n", "", "first line"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			header, err := parseSessionHeader([]byte(test.data))
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if header.ID != test.wantID || header.Version != 3 || header.Cwd != "/repo" {
				t.Fatalf("header = %#v", header)
			}
		})
	}
}

func TestResolveSessionFileByHeaderID(t *testing.T) {
	dir := t.TempDir()
	writeSession := func(name, id string, title bool) {
		t.Helper()
		body := fmt.Sprintf(`{"type":"session","version":3,"id":%q,"timestamp":"2026-07-26T00:00:00Z","cwd":"/repo"}`+"\n", id)
		if title {
			body = titleSlotLine(t) + body
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeSession("z-later.jsonl", "duplicate", false)
	writeSession("a-first.jsonl", "duplicate", true)
	writeSession("timestamp_unrelated.jsonl", "other", true)

	if got, want := New().ResolveSessionFile(dir, "duplicate"), filepath.Join(dir, "a-first.jsonl"); got != want {
		t.Fatalf("ResolveSessionFile() = %q, want %q", got, want)
	}
	if got, want := New().ResolveSessionFile(dir, "new-session"), filepath.Join(dir, "new-session.jsonl"); got != want {
		t.Fatalf("fallback = %q, want %q", got, want)
	}
	if err := os.WriteFile(filepath.Join(dir, "filename-only.jsonl"), []byte("corrupt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, want := New().ResolveSessionFile(dir, "filename-only"), filepath.Join(dir, "filename-only.jsonl"); got != want {
		t.Fatalf("exact filename fallback = %q, want %q", got, want)
	}
}

func TestResolveSessionFileRejectsUnsafeIDs(t *testing.T) {
	dir := t.TempDir()
	for _, id := range []string{"", ".", "..", "../escape", "a/b", `a\\b`, "two words", "é"} {
		if got := New().ResolveSessionFile(dir, id); got != "" {
			t.Errorf("ResolveSessionFile(%q) = %q, want empty", id, got)
		}
	}
}

func titleSlotLine(t *testing.T) string {
	t.Helper()
	prefix := `{"type":"title","v":1,"title":"Title","updatedAt":"2026-07-26T00:00:00Z","pad":"`
	suffix := `"}` + "\n"
	padding := 256 - len(prefix) - len(suffix)
	if padding < 0 {
		t.Fatal("title slot fixture exceeds 256 bytes")
	}
	line := prefix + strings.Repeat(" ", padding) + suffix
	if len(line) != 256 {
		t.Fatalf("title slot fixture = %d bytes", len(line))
	}
	return line
}
