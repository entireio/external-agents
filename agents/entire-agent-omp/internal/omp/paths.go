package omp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

const maxSessionHeaderBytes = 64 * 1024

var (
	profileNameRE            = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	windowsReservedProfileRE = regexp.MustCompile(`(?i)^(CON|PRN|AUX|NUL|COM[0-9]|LPT[0-9])(\..*)?$`)
)

type envValue struct {
	value string
	set   bool
}

type sessionHeader struct {
	Type      string `json:"type"`
	Version   int    `json:"version"`
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	Cwd       string `json:"cwd"`
}

func parseSessionHeader(data []byte) (sessionHeader, error) {
	line, rest, ok := nextJSONLLine(data)
	if !ok {
		return sessionHeader{}, errors.New("parse omp session header: missing first JSONL line")
	}

	var kind struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(line, &kind); err != nil {
		return sessionHeader{}, fmt.Errorf("parse omp session header first line: %w", err)
	}
	if kind.Type == "title" {
		line, _, ok = nextJSONLLine(rest)
		if !ok {
			return sessionHeader{}, errors.New("parse omp session header: title slot is not followed by a session line")
		}
	}

	var header sessionHeader
	if err := json.Unmarshal(line, &header); err != nil {
		return sessionHeader{}, fmt.Errorf("parse omp session header JSON: %w", err)
	}
	if header.Type != "session" {
		return sessionHeader{}, fmt.Errorf("parse omp session header: type is %q, want %q", header.Type, "session")
	}
	if header.ID == "" {
		return sessionHeader{}, errors.New("parse omp session header: id is empty")
	}
	if header.Version <= 0 {
		return sessionHeader{}, fmt.Errorf("parse omp session header: version is %d, want a positive value", header.Version)
	}
	return header, nil
}

func nextJSONLLine(data []byte) (line, rest []byte, ok bool) {
	index := bytes.IndexByte(data, '\n')
	if index < 0 {
		if len(data) == 0 {
			return nil, nil, false
		}
		return bytes.TrimSuffix(data, []byte{'\r'}), nil, true
	}
	return bytes.TrimSuffix(data[:index], []byte{'\r'}), data[index+1:], true
}

func (a *Agent) GetSessionDir(repoPath string) (string, error) {
	root, err := ompSessionsRoot()
	if err != nil {
		return "", err
	}
	name, err := encodeSessionDirName(repoPath)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, name), nil
}

func ompSessionsRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return ompSessionsRootFor(
		runtime.GOOS,
		home,
		os.Getenv("PI_CODING_AGENT_DIR"),
		os.Getenv("PI_CONFIG_DIR"),
		os.Getenv("XDG_DATA_HOME"),
		lookupEnv("OMP_PROFILE"),
		lookupEnv("PI_PROFILE"),
		func(path string) bool {
			_, err := os.Stat(path)
			return err == nil
		},
	)
}

func lookupEnv(key string) envValue {
	value, set := os.LookupEnv(key)
	return envValue{value: value, set: set}
}

func normalizeProfileName(profile string) string {
	profile = strings.TrimSpace(profile)
	if profile == "" || profile == "default" {
		return ""
	}
	if profile == "." || profile == ".." || strings.HasSuffix(profile, ".") ||
		!profileNameRE.MatchString(profile) || windowsReservedProfileRE.MatchString(profile) {
		return ""
	}
	return profile
}

func ompSessionsRootFor(
	goos, home, agentDir, configDir, xdgData string,
	ompProfile, piProfile envValue,
	exists func(string) bool,
) (string, error) {
	if configDir == "" {
		configDir = ".omp"
	}
	baseConfigRoot := filepath.Join(home, configDir)
	profileValue := piProfile
	if ompProfile.set {
		profileValue = ompProfile
	}
	profile := normalizeProfileName(profileValue.value)
	configRoot := baseConfigRoot
	if profile != "" {
		configRoot = filepath.Join(baseConfigRoot, "profiles", profile)
	}
	defaultAgentDir := filepath.Join(configRoot, "agent")

	profileAgentSource := profile
	if profileAgentSource == "" {
		profileAgentSource = normalizeProfileName(piProfile.value)
	}
	switch {
	case profile != "" || (profileAgentSource != "" && agentDir == filepath.Join(baseConfigRoot, "profiles", profileAgentSource, "agent")):
		agentDir = defaultAgentDir
	case agentDir == "":
		agentDir = defaultAgentDir
	default:
		absolute, err := filepath.Abs(agentDir)
		if err != nil {
			return "", fmt.Errorf("resolve PI_CODING_AGENT_DIR: %w", err)
		}
		agentDir = absolute
	}
	if (goos == "linux" || goos == "darwin") && agentDir == defaultAgentDir && xdgData != "" {
		appRoot := filepath.Join(xdgData, "omp")
		xdgRoot := appRoot
		if profile != "" {
			xdgRoot = filepath.Join(appRoot, "profiles", profile)
		}
		if exists(xdgRoot) {
			return filepath.Join(xdgRoot, "sessions"), nil
		}
	}
	return filepath.Join(agentDir, "sessions"), nil
}

func encodeSessionDirName(cwd string) (string, error) {
	canonicalCwd, err := canonicalPath(cwd)
	if err != nil {
		return "", fmt.Errorf("resolve omp session cwd %q: %w", cwd, err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	canonicalHome, err := canonicalPath(home)
	if err != nil {
		return "", fmt.Errorf("resolve canonical home directory: %w", err)
	}
	canonicalTemp, err := canonicalPath(os.TempDir())
	if err != nil {
		return "", fmt.Errorf("resolve canonical temporary directory: %w", err)
	}

	if relative, ok := relativeWithin(canonicalHome, canonicalCwd); ok {
		return encodeRelative("-", relative), nil
	}
	if relative, ok := relativeWithin(canonicalTemp, canonicalCwd); ok {
		return encodeRelative("-tmp", relative), nil
	}
	trimmed := strings.TrimLeft(canonicalCwd, "/\\")
	replacer := strings.NewReplacer("/", "-", "\\", "-", ":", "-")
	return "--" + replacer.Replace(trimmed) + "--", nil
}

func canonicalPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err == nil {
		return canonical, nil
	}
	return filepath.Clean(absolute), nil
}

func relativeWithin(root, candidate string) (string, bool) {
	relative, err := filepath.Rel(root, candidate)
	if err != nil {
		return "", false
	}
	return relative, relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative))
}

func encodeRelative(prefix, relative string) string {
	if relative == "." || relative == "" {
		return prefix
	}
	replacer := strings.NewReplacer("/", "-", "\\", "-", ":", "-")
	encoded := replacer.Replace(relative)
	if strings.HasSuffix(prefix, "-") {
		return prefix + encoded
	}
	return prefix + "-" + encoded
}

func (a *Agent) ResolveSessionFile(sessionDir, sessionID string) string {
	if !safeSessionID(sessionID) {
		return ""
	}
	entries, _ := os.ReadDir(sessionDir)
	exactFilename := filepath.Join(sessionDir, sessionID+".jsonl")
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" {
			continue
		}
		path := filepath.Join(sessionDir, entry.Name())
		header, err := readSessionHeader(path)
		if err == nil && header.ID == sessionID {
			return path
		}
	}
	return exactFilename
}

func readSessionHeader(path string) (sessionHeader, error) {
	file, err := os.Open(path)
	if err != nil {
		return sessionHeader{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxSessionHeaderBytes))
	if err != nil {
		return sessionHeader{}, err
	}
	return parseSessionHeader(data)
}

func safeSessionID(sessionID string) bool {
	if sessionID == "" || sessionID == "." || sessionID == ".." {
		return false
	}
	for _, r := range sessionID {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}
