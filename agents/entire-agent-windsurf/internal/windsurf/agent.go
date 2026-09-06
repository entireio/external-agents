// Package windsurf supplies the protocol core for Windsurf Cascade.
package windsurf

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/entireio/external-agents/agents/entire-agent-windsurf/internal/protocol"
)

const AgentName = "windsurf"

type Agent struct{}

func New() *Agent { return &Agent{} }

func (a *Agent) Info() protocol.InfoResponse {
	return protocol.InfoResponse{
		ProtocolVersion: protocol.ProtocolVersion,
		Name:            AgentName,
		Type:            "Windsurf Cascade",
		Description:     "Windsurf Cascade external-agent adapter",
		IsPreview:       true,
		ProtectedDirs:   []string{".windsurf"},
		HookNames:       []string{},
		Capabilities:    protocol.DeclaredCapabilities{},
	}
}

// Detect treats either the desktop executable or a workspace configuration as
// evidence that Windsurf is available. The config check lets IDE-only installs
// participate without requiring a command-line executable.
func (a *Agent) Detect() protocol.DetectResponse {
	if _, err := exec.LookPath("windsurf"); err == nil {
		return protocol.DetectResponse{Present: true}
	}
	_, err := os.Stat(filepath.Join(protocol.RepoRoot(), ".windsurf"))
	return protocol.DetectResponse{Present: err == nil}
}

func (a *Agent) GetSessionID(input *protocol.HookInputJSON) string {
	if input == nil { return "" }
	if input.SessionID != "" { return input.SessionID }
	return stringValue(input.RawData, "trajectory_id")
}
func (a *Agent) GetSessionDir(repo string) (string, error) {
	if repo == "" { return "", errors.New("repo-path is required") }
	return protocol.DefaultSessionDir(repo), nil
}
func (a *Agent) ResolveSessionFile(dir, id string) string {
	if !safeSessionID(id) { return "" }
	return protocol.ResolveSessionFile(dir, id)
}
func (a *Agent) ReadSession(input *protocol.HookInputJSON) (protocol.AgentSessionJSON, error) {
	id := a.GetSessionID(input)
	if id == "" { return protocol.AgentSessionJSON{}, errors.New("Windsurf trajectory_id is required") }
	return protocol.AgentSessionJSON{SessionID: id, AgentName: AgentName, SessionRef: input.SessionRef, StartTime: input.Timestamp, ModifiedFiles: []string{}, NewFiles: []string{}, DeletedFiles: []string{}}, nil
}
// WriteSession is intentionally a no-op until the lifecycle owner supplies a
// native session persistence strategy.
func (a *Agent) WriteSession(protocol.AgentSessionJSON) error { return nil }
func (a *Agent) ReadTranscript(ref string) ([]byte, error) { return os.ReadFile(ref) }
func (a *Agent) ChunkTranscript(data []byte, max int) ([][]byte, error) {
	if max <= 0 { return nil, errors.New("max-size must be positive") }
	chunks := make([][]byte, 0, (len(data)+max-1)/max)
	for len(data) > 0 {
		end := max; if end > len(data) { end = len(data) }
		chunks = append(chunks, append([]byte(nil), data[:end]...)); data = data[end:]
	}
	return chunks, nil
}
func (a *Agent) ReassembleTranscript(chunks [][]byte) ([]byte, error) { return bytes.Join(chunks, nil), nil }
func (a *Agent) FormatResumeCommand(string) string { return "windsurf" }
func stringValue(values map[string]interface{}, key string) string { if values == nil { return "" }; value, _ := values[key].(string); return value }
func safeSessionID(id string) bool { if id == "" || id == "." || id == ".." { return false }; return !strings.ContainsAny(id, `/\\`) }
