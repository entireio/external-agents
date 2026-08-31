package grok

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/entireio/external-agents/agents/entire-agent-grok/internal/protocol"
)

const nativeTranscriptFile = "chat_history.jsonl"

// maxEncodedCWDLen is the point at which Grok stops using the encoded working
// directory as the session group name and switches to a slug plus a hash,
// recording the original path in a .cwd file inside the group instead.
const maxEncodedCWDLen = 255

// cwdMarkerFile names the file Grok writes inside a hashed session group to
// record which working directory it belongs to.
const cwdMarkerFile = ".cwd"

func grokHome() string {
	if home := strings.TrimSpace(os.Getenv("GROK_HOME")); home != "" {
		return home
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(userHome, ".grok")
}

// resolveRepoPath canonicalizes a repo path the way Grok sees it. macOS temp
// directories live under /var, a symlink to /private/var, so the path must be
// resolved or the encoded group name will not match Grok's.
func resolveRepoPath(repoPath string) string {
	repoPath = strings.TrimSpace(repoPath)
	if repoPath == "" {
		repoPath = protocol.RepoRoot()
	}
	if resolved, err := filepath.EvalSymlinks(repoPath); err == nil {
		repoPath = resolved
	}
	return filepath.Clean(repoPath)
}

// encodeRepoCWD percent-encodes a working directory into Grok's session group
// name.
//
// Grok URL-encodes every byte outside the RFC 3986 unreserved set
// (ALPHA / DIGIT / "-" / "." / "_" / "~") using uppercase hex, so "/" becomes
// %2F, a space becomes %20 and "é" becomes %C3%A9. Encoding only "/" — as this
// did — silently misses any repo path containing a space, an accent, or
// punctuation such as !()'*+@&=,;:$, and Entire then looks for a session
// directory that does not exist.
//
// Note this is stricter than Go's url.PathEscape, which leaves the sub-delims
// $&+,;=:@ unescaped in a path segment. Grok escapes those.
func encodeRepoCWD(repoPath string) string {
	return percentEncodeCWD(resolveRepoPath(repoPath))
}

func percentEncodeCWD(path string) string {
	var encoded strings.Builder
	encoded.Grow(len(path))
	for i := range len(path) {
		if c := path[i]; isUnreservedCWDByte(c) {
			encoded.WriteByte(c)
			continue
		}
		fmt.Fprintf(&encoded, "%%%02X", path[i])
	}
	return encoded.String()
}

func isUnreservedCWDByte(c byte) bool {
	switch {
	case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		return true
	case c == '-', c == '.', c == '_', c == '~':
		return true
	default:
		return false
	}
}

// nativeSessionDir returns the Grok session group directory for a repo.
//
// Below the length limit the group is named after the encoded path. Above it,
// Grok uses "<last-path-segment>-<hash>", and the hash is not something we can
// reproduce, so the group is located by reading the .cwd marker Grok leaves in
// each hashed group. If no marker matches, fall back to the encoded name so the
// caller still gets a usable path for a session that does not exist yet.
func nativeSessionDir(repoPath string) string {
	resolved := resolveRepoPath(repoPath)
	root := filepath.Join(grokHome(), "sessions")
	encoded := percentEncodeCWD(resolved)
	if len(encoded) <= maxEncodedCWDLen {
		return filepath.Join(root, encoded)
	}
	if hashed := findHashedSessionDir(root, resolved); hashed != "" {
		return hashed
	}
	return filepath.Join(root, encoded)
}

// findHashedSessionDir scans session groups for the .cwd marker naming repoPath.
func findHashedSessionDir(root, repoPath string) string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		marker, err := os.ReadFile(filepath.Join(root, entry.Name(), cwdMarkerFile))
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(marker)) == repoPath {
			return filepath.Join(root, entry.Name())
		}
	}
	return ""
}

func nativeTranscriptPath(repoPath, sessionID string) string {
	if strings.TrimSpace(sessionID) == "" {
		sessionID = stubSessionID
	}
	return filepath.Join(nativeSessionDir(repoPath), sessionID, nativeTranscriptFile)
}

func (a *Agent) resolveSessionRef(sessionID, repoPath string) string {
	if strings.TrimSpace(repoPath) == "" {
		repoPath = protocol.RepoRoot()
	}
	return nativeTranscriptPath(repoPath, sessionID)
}
