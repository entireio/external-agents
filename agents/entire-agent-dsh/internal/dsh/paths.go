package dsh

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

// DSH keeps every session under one user-data root. The environment variable
// wins over the default so a test or a container can point the integration at
// a disposable harness home.
const defaultDSHHomeDir = ".dsh"

// cacheSubdir is the repository-scoped scratch tree this integration owns.
// Deliberately not under .entire/tmp: another external agent (Kiro) uses that
// directory, and a transcript nested inside another agent's session store can
// be attributed to the wrong agent.
var cacheSubdir = filepath.Join("entire", "sessions")

var safeSessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

func dshHome() string {
	if configured := strings.TrimSpace(os.Getenv("DSH_HOME")); configured != "" {
		return filepath.Clean(expandHomePath(configured))
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return defaultDSHHomeDir
	}
	return filepath.Join(home, defaultDSHHomeDir)
}

func expandHomePath(p string) string {
	if p == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return p
	}
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

func sessionsRoot() string {
	return filepath.Join(dshHome(), "sessions")
}

// GetSessionDir returns the repository-scoped directory that holds the
// normalized transcripts this binary materializes for Entire.
func (a *Agent) GetSessionDir(repoPath string) (string, error) {
	root := repoPath
	if strings.TrimSpace(root) == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("resolve working directory: %w", err)
		}
		root = cwd
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve repository path: %w", err)
	}
	return filepath.Join(dshHome(), cacheSubdir, projectKey(physicalPath(abs))), nil
}

// ResolveSessionFile returns the normalized transcript path for a session ID.
// Safe IDs keep their readable name; anything else, including Windows device
// names, is hashed with a sidecar holding the original ID.
func (a *Agent) ResolveSessionFile(sessionDir, sessionID string) string {
	if strings.TrimSpace(sessionDir) == "" {
		dir, err := a.GetSessionDir("")
		if err != nil {
			return ""
		}
		sessionDir = dir
	}
	name, _ := safeSessionName(sessionID)
	return filepath.Join(sessionDir, name+".jsonl")
}

// safeSessionName returns the basename used for a session's normalized
// transcript and whether that name is a hash rather than the raw ID.
func safeSessionName(sessionID string) (string, bool) {
	safe := []byte(sessionID)
	for i, b := range safe {
		switch {
		case b >= 'A' && b <= 'Z', b >= 'a' && b <= 'z', b >= '0' && b <= '9':
		case b == '_', b == '.', b == '-':
		default:
			safe[i] = '_'
		}
	}
	transformed := string(safe)
	if transformed != "" && transformed == sessionID && len(sessionID) <= 180 && !isWindowsDeviceName(sessionID) {
		return transformed, false
	}
	sum := sha256.Sum256([]byte(sessionID))
	return "~" + hex.EncodeToString(sum[:16]), true
}

// sessionIDFromRef recovers the original session ID from a normalized
// transcript path. It reports false for files this integration did not name.
func sessionIDFromRef(ref string) (string, bool) {
	if strings.TrimSpace(ref) == "" {
		return "", false
	}
	base := filepath.Base(ref)
	if filepath.Ext(base) != ".jsonl" {
		return "", false
	}
	base = strings.TrimSuffix(base, ".jsonl")
	if strings.HasPrefix(base, "~") {
		sidecar := strings.TrimSuffix(ref, filepath.Ext(ref)) + ".id"
		data, err := os.ReadFile(sidecar) // #nosec G304 -- sidecar sits beside the caller-provided transcript
		if err != nil {
			return "", false
		}
		id := strings.TrimSpace(string(data))
		return id, id != ""
	}
	if base == "" || !safeSessionIDPattern.MatchString(base) || isWindowsDeviceName(base) {
		return "", false
	}
	return base, true
}

func writeSessionIDSidecar(ref, sessionID string) error {
	name, hashed := safeSessionName(sessionID)
	if !hashed {
		return nil
	}
	sidecar := filepath.Join(filepath.Dir(ref), name+".id")
	return writeFileAtomic(sidecar, []byte(sessionID+"\n"), 0o600)
}

// encodeSegment mirrors DSH's JSONL persistence encoding: safe code units stay
// literal and every other UTF-16 code unit becomes ~XXXX. The mapping is
// injective over all Go strings and neutralizes traversal.
func encodeSegment(raw string) (string, error) {
	if raw == "" {
		return "", errors.New("cannot encode an empty path segment")
	}
	if raw == "." {
		return "~002E", nil
	}
	if raw == ".." {
		return "~002E~002E", nil
	}
	var b strings.Builder
	for _, unit := range utf16.Encode([]rune(raw)) {
		if unit != '~' && isSafePathUnit(unit) {
			b.WriteRune(rune(unit))
			continue
		}
		fmt.Fprintf(&b, "~%04X", unit)
	}
	return b.String(), nil
}

// projectKey mirrors DSH's human-navigable project directory name: filesystem
// and drive separators collapse into one '-', unsafe code units use ~XXXX, and
// the result is wrapped in '--' and bounded for filesystem component limits.
func projectKey(cwd string) string {
	if cwd == "" {
		return "--root--"
	}
	var b strings.Builder
	separatorRun := false
	for _, unit := range utf16.Encode([]rune(cwd)) {
		ch := rune(unit)
		switch {
		case ch == '/' || ch == '\\' || ch == ':':
			if !separatorRun {
				b.WriteByte('-')
			}
			separatorRun = true
		case isSafePathUnit(unit):
			b.WriteRune(ch)
			separatorRun = false
		default:
			fmt.Fprintf(&b, "~%04X", unit)
			separatorRun = false
		}
	}
	readable := strings.TrimLeft(b.String(), "-")
	if readable == "" {
		readable = "root"
	}
	if len(readable) > 251 {
		readable = readable[:251]
	}
	return "--" + readable + "--"
}

func isSafePathUnit(unit uint16) bool {
	ch := rune(unit)
	return ch == '.' || ch == '_' || ch == '-' ||
		(ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9')
}

// isWindowsDeviceName applies on every OS so a session restored on Windows
// keeps the same file name and never lands on a reserved device basename.
func isWindowsDeviceName(name string) bool {
	base, _, _ := strings.Cut(name, ".")
	base = strings.ToUpper(strings.TrimRight(base, " "))
	switch base {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		return true
	}
	if strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT") {
		switch base[3:] {
		case "0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "¹", "²", "³":
			return true
		}
	}
	return false
}

// findNativeSession resolves the DSH event log for a session ID. The project
// directory is derived from the repository root and an optional cwd hint; when
// neither matches (DSH keys the directory by the session's workspace, which can
// be a subdirectory of the git root) every project directory is searched and
// the header cwd is checked for containment.
func findNativeSession(sessionID, repoRoot, cwdHint string) (string, error) {
	if strings.TrimSpace(sessionID) == "" {
		return "", errors.New("session ID is empty")
	}
	root := sessionsRoot()
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return "", fmt.Errorf("DSH sessions directory not found at %s", root)
	}

	candidates := make([]string, 0, 2)
	seen := map[string]struct{}{}
	addCandidate := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return
		}
		key := filepath.Clean(abs)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		candidates = append(candidates, key)
	}
	addCandidate(repoRoot)
	addCandidate(cwdHint)

	// The Host may report a symlinked workspace while DSH stored the physical
	// path (or the reverse). Try both spellings before scanning.
	candidateKeys := make([]string, 0, len(candidates)*2)
	for _, cwd := range candidates {
		candidateKeys = append(candidateKeys, projectKey(cwd), projectKey(physicalPath(cwd)))
	}
	for _, key := range dedupeStrings(candidateKeys) {
		if logPath, ok := findGenerationLog(filepath.Join(root, key), sessionID); ok {
			return logPath, nil
		}
	}

	projectDirs, err := os.ReadDir(root)
	if err != nil {
		return "", fmt.Errorf("read DSH sessions directory: %w", err)
	}
	var matches []string
	for _, entry := range projectDirs {
		if !entry.IsDir() {
			continue
		}
		logPath, ok := findGenerationLog(filepath.Join(root, entry.Name()), sessionID)
		if !ok {
			continue
		}
		headerCWD, _ := readSessionHeaderCWD(logPath)
		if headerCWD == "" || repoRoot == "" || pathWithin(headerCWD, repoRoot) {
			matches = append(matches, logPath)
		}
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("DSH session %q not found under %s", sessionID, root)
	}
	sort.SliceStable(matches, func(i, j int) bool {
		return modificationTime(matches[i]) > modificationTime(matches[j])
	})
	return matches[0], nil
}

// findGenerationLog selects the highest canonical session generation in one
// project directory. DSH writes session.vN.jsonl[.zstd]; older installs may
// still carry the untagged session.jsonl.
func findGenerationLog(projectDir, sessionID string) (string, bool) {
	encoded, err := encodeSegment(sessionID)
	if err != nil {
		return "", false
	}
	sessionDir := filepath.Join(projectDir, encoded)
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		return "", false
	}

	type generation struct {
		version int
		zstd    bool
		name    string
	}
	var found []generation
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		version, zstd, ok := parseGenerationName(entry.Name())
		if !ok {
			continue
		}
		found = append(found, generation{version: version, zstd: zstd, name: entry.Name()})
	}
	if len(found) == 0 {
		return "", false
	}
	sort.SliceStable(found, func(i, j int) bool {
		if found[i].version != found[j].version {
			return found[i].version > found[j].version
		}
		return found[i].zstd && !found[j].zstd
	})
	return filepath.Join(sessionDir, found[0].name), true
}

func parseGenerationName(name string) (version int, zstd bool, ok bool) {
	base := name
	if strings.HasSuffix(base, ".zstd") {
		zstd = true
		base = strings.TrimSuffix(base, ".zstd")
	}
	if !strings.HasSuffix(base, ".jsonl") {
		return 0, false, false
	}
	base = strings.TrimSuffix(base, ".jsonl")
	if base == "session" {
		return 0, zstd, true
	}
	if !strings.HasPrefix(base, "session.v") {
		return 0, false, false
	}
	raw := strings.TrimPrefix(base, "session.v")
	if raw == "" || (len(raw) > 1 && raw[0] == '0') {
		return 0, false, false
	}
	for _, r := range raw {
		if r < '0' || r > '9' {
			return 0, false, false
		}
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return 0, false, false
	}
	return value, zstd, true
}

func readSessionHeaderCWD(path string) (string, error) {
	data, err := readTranscriptFile(path)
	if err != nil {
		return "", err
	}
	header, err := parseSessionHeader(data)
	if err != nil {
		return "", err
	}
	return header.CWD, nil
}

func pathWithin(path, root string) bool {
	absPath := physicalPath(path)
	absRoot := physicalPath(root)
	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// physicalPath resolves symlinks for the deepest existing prefix. Workspace
// paths on macOS (/var -> /private/var) and CI symlinked checkouts disagree
// between the logical ENTIRE_REPO_ROOT and DSH's physical header.cwd.
func physicalPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}

	// Resolve the deepest existing ancestor and re-append the missing suffix.
	// A not-yet-written session file would otherwise compare its logical path
	// against a physical session directory and look like an escape.
	current := abs
	missing := make([]string, 0, 4)
	for {
		if _, statErr := os.Lstat(current); statErr == nil {
			resolved, resolveErr := filepath.EvalSymlinks(current)
			if resolveErr != nil {
				return abs
			}
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return resolved
		}
		parent := filepath.Dir(current)
		if parent == current {
			return abs
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

func modificationTime(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.ModTime().UnixNano()
}
