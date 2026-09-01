package zcode

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/entireio/external-agents/agents/entire-agent-zcode/internal/protocol"
)

// Agent implements the Entire external agent protocol for ZCode.
//
// ZCode is an Electron desktop app with no headless CLI, so every operation
// either reads its on-disk state (SQLite session DB at ~/.zcode/cli/db) or its
// user-level hook config (~/.zcode/cli/config.json — the only hook config
// source ZCode executes).
type Agent struct {
	DBQuerier DBQuerier
}

func New() *Agent {
	return &Agent{DBQuerier: &SQLiteQuerier{}}
}

func (a *Agent) Info() protocol.InfoResponse {
	return protocol.InfoResponse{
		ProtocolVersion: protocol.ProtocolVersion,
		Name:            "zcode",
		Type:            "ZCode",
		Description:     "ZCode agentic development environment integration for Entire (desktop app; sessions are read from ZCode's local SQLite store)",
		IsPreview:       true,
		ProtectedDirs:   []string{".zcode"},
		HookNames: []string{
			HookNameSessionStart,
			HookNameTurnStart,
			HookNameTurnEnd,
			HookNameCompaction,
		},
		Capabilities: protocol.DeclaredCapabilities{
			Hooks:              true,
			TranscriptAnalyzer: true,
			TranscriptPreparer: true,
			TokenCalculator:    true,
			UsesTerminal:       false,
		},
	}
}

func (a *Agent) Detect() protocol.DetectResponse {
	if _, err := exec.LookPath("zcode"); err == nil {
		return protocol.DetectResponse{Present: true}
	}
	// The binary may not be on PATH (desktop install); the local state
	// directory is equally strong evidence ZCode is in use.
	if info, err := os.Stat(zcodeHome()); err == nil && info.IsDir() {
		return protocol.DetectResponse{Present: true}
	}
	return protocol.DetectResponse{Present: false}
}

func (a *Agent) GetSessionID(input *protocol.HookInputJSON) string {
	if input != nil && input.SessionID != "" {
		return input.SessionID
	}
	return ""
}

// FormatResumeCommand cannot resume a specific session: ZCode is a GUI app
// with no headless resume. Launching the app opens the session list where the
// user picks the session.
func (a *Agent) FormatResumeCommand(_ string) string {
	return "zcode"
}

// zcodeHome returns ZCode's state root. ZCODE_HOME overrides it so tests (and
// sandboxed environments) can point at a fixture directory.
func zcodeHome() string {
	if home := os.Getenv("ZCODE_HOME"); home != "" {
		return home
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".zcode"
	}
	return filepath.Join(home, ".zcode")
}

func configPath() string {
	return filepath.Join(zcodeHome(), "cli", "config.json")
}

func dbPath() string {
	return filepath.Join(zcodeHome(), "cli", "db", "db.sqlite")
}
