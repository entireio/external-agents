package goose

import (
	"os"

	"github.com/entireio/external-agents/agents/entire-agent-goose/internal/protocol"
)

// ReadSession builds an AgentSession from a hook input. Implemented in
// Phase 3 (reads session metadata from the goose export).
func (a *Agent) ReadSession(input *protocol.HookInputJSON) (protocol.AgentSessionJSON, error) {
	return protocol.AgentSessionJSON{
		SessionID:  input.SessionID,
		AgentName:  "goose",
		RepoPath:   protocol.RepoRoot(),
		SessionRef: input.SessionRef,
	}, nil
}

// WriteSession persists session data. Implemented in Phase 3.
func (a *Agent) WriteSession(session protocol.AgentSessionJSON) error {
	return nil
}

func (a *Agent) ReadTranscript(sessionRef string) ([]byte, error) {
	return os.ReadFile(sessionRef)
}

func (a *Agent) ChunkTranscript(content []byte, maxSize int) ([][]byte, error) {
	if maxSize <= 0 || len(content) == 0 {
		return [][]byte{content}, nil
	}
	var chunks [][]byte
	for len(content) > maxSize {
		chunks = append(chunks, content[:maxSize])
		content = content[maxSize:]
	}
	return append(chunks, content), nil
}

func (a *Agent) ReassembleTranscript(chunks [][]byte) ([]byte, error) {
	var out []byte
	for _, chunk := range chunks {
		out = append(out, chunk...)
	}
	return out, nil
}

// PrepareTranscript materializes the transcript by exporting the goose
// session (`goose session export --format json`). Implemented in Phase 3.
func (a *Agent) PrepareTranscript(sessionRef string) error {
	return nil
}

func (a *Agent) GetTranscriptPosition(path string) (int, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	return int(info.Size()), nil
}

// ExtractModifiedFiles parses write/edit tool calls out of the exported
// transcript. Implemented in Phase 3.
func (a *Agent) ExtractModifiedFiles(path string, offset int) ([]string, int, error) {
	position, err := a.GetTranscriptPosition(path)
	if err != nil {
		return nil, 0, err
	}
	return []string{}, position, nil
}

// ExtractPrompts parses user prompts out of the exported transcript.
// Implemented in Phase 3.
func (a *Agent) ExtractPrompts(sessionRef string, offset int) ([]string, error) {
	return []string{}, nil
}

// ExtractSummary returns the goose session name as the summary.
// Implemented in Phase 3.
func (a *Agent) ExtractSummary(sessionRef string) (string, bool, error) {
	return "", false, nil
}

// CalculateTokens reads accumulated token totals from the exported
// transcript. Implemented in Phase 3.
func (a *Agent) CalculateTokens(data []byte, offset int) (protocol.TokenUsageResponse, error) {
	return protocol.TokenUsageResponse{}, nil
}
