package aider

import (
	"os"
	"os/exec"
	"path/filepath"
)

const AgentName = "aider"
const HistoryFile = ".aider.chat.history.md"

type Capabilities struct {
	Hooks                  bool `json:"hooks"`
	TranscriptAnalyzer     bool `json:"transcript_analyzer"`
	TokenCalculator        bool `json:"token_calculator"`
	TranscriptPreparer     bool `json:"transcript_preparer"`
	CompactTranscript      bool `json:"compact_transcript"`
	TextGenerator          bool `json:"text_generator"`
	HookResponseWriter     bool `json:"hook_response_writer"`
	SubagentAwareExtractor bool `json:"subagent_aware_extractor"`
	UsesTerminal           bool `json:"uses_terminal"`
}

type InfoResponse struct {
	ProtocolVersion int          `json:"protocol_version"`
	Name            string       `json:"name"`
	Type            string       `json:"type"`
	Description     string       `json:"description"`
	IsPreview       bool         `json:"is_preview"`
	ProtectedDirs   []string     `json:"protected_dirs"`
	HookNames       []string     `json:"hook_names"`
	Capabilities    Capabilities `json:"capabilities"`
}

type Agent struct{ LookPath func(string) (string, error) }

func New() *Agent { return &Agent{LookPath: exec.LookPath} }
func (*Agent) Info() InfoResponse {
	return InfoResponse{ProtocolVersion: 1, Name: AgentName, Type: "Aider", Description: "Aider Markdown history and commit attribution (preview)", IsPreview: true, ProtectedDirs: []string{}, HookNames: []string{}, Capabilities: Capabilities{TranscriptAnalyzer: true, TokenCalculator: true, UsesTerminal: true}}
}
func (a *Agent) Detect() bool {
	lookup := a.LookPath
	if lookup == nil {
		lookup = exec.LookPath
	}
	if _, err := lookup("aider"); err == nil {
		return true
	}
	info, err := os.Stat(filepath.Join(RepoRoot(), HistoryFile))
	return err == nil && info.Mode().IsRegular()
}
func RepoRoot() string {
	if root := os.Getenv("ENTIRE_REPO_ROOT"); root != "" {
		return root
	}
	root, _ := os.Getwd()
	return root
}
func (*Agent) FormatResumeCommand(string) string { return "aider --restore-chat-history" }
