package devin

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/entireio/external-agents/agents/entire-agent-devin/internal/protocol"
)

// parseTranscript unmarshals an ATIF document.
func parseTranscript(data []byte) (*ATIFTranscript, error) {
	var t ATIFTranscript
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("failed to parse ATIF transcript: %w", err)
	}
	return &t, nil
}

// PrepareTranscript materializes or waits for Devin's transcript. Devin
// writes the canonical transcript only when a session run ends — so at
// TurnEnd time the file is either absent (first run, mid-session) or stale
// (resumed session). Entire requires the transcript file to exist before it
// saves a checkpoint, so:
//   - fresh file: return immediately
//   - stale file: keep it — real data from the previous run beats a stub;
//     no flush is coming mid-session
//   - missing file: poll briefly (print mode writes it shortly after Stop
//     fires), then materialize a minimal valid ATIF stub so the turn's code
//     checkpoint can proceed. Devin overwrites the file with the complete
//     transcript when the session ends (see AGENT.md, flush timing).
func (a *Agent) PrepareTranscript(sessionRef string) error {
	if sessionRef == "" {
		return nil
	}
	const (
		maxWait      = 2 * time.Second
		pollInterval = 50 * time.Millisecond
		maxSkew      = 2 * time.Second
		// staleThreshold: a transcript that hasn't been touched in minutes
		// belongs to a previous session run and no flush is coming.
		staleThreshold = 2 * time.Minute
	)

	hookStart := time.Now()
	if info, err := os.Stat(sessionRef); err == nil {
		age := time.Since(info.ModTime())
		if age < maxSkew {
			return nil // Already fresh
		}
		if age > staleThreshold {
			return nil // Previous run's transcript; no flush coming mid-session
		}
	}

	deadline := hookStart.Add(maxWait)
	for time.Now().Before(deadline) {
		if info, err := os.Stat(sessionRef); err == nil && info.ModTime().After(hookStart.Add(-maxSkew)) {
			return nil
		}
		time.Sleep(pollInterval)
	}

	if _, err := os.Stat(sessionRef); os.IsNotExist(err) {
		// Devin has not flushed the canonical ATIF file yet. Try to build one
		// from the live SQLite session store; if that fails, fall back to a
		// minimal stub so the checkpoint can proceed.
		if err := a.materializeLiveTranscript(sessionRef); err != nil {
			return writeStubTranscript(sessionRef)
		}
	}
	return nil // Stale file left in place: best-effort
}

// writeStubTranscript writes a minimal valid ATIF document for a session
// whose canonical transcript has not been flushed yet. Never overwrites an
// existing file. Devin regenerates transcripts from its session store on
// exit, so the stub's lifetime ends with the session run.
func writeStubTranscript(sessionRef string) error {
	sessionID := strings.TrimSuffix(filepath.Base(sessionRef), ".json")
	stub := ATIFTranscript{
		SchemaVersion: "ATIF-v1.7",
		SessionID:     sessionID,
		Steps:         []ATIFStep{},
	}
	data, err := json.Marshal(stub)
	if err != nil {
		return fmt.Errorf("failed to marshal stub transcript: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(sessionRef), 0o750); err != nil {
		return fmt.Errorf("failed to create transcript directory: %w", err)
	}
	f, err := os.OpenFile(sessionRef, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return nil // Real transcript landed in the meantime — keep it
		}
		return fmt.Errorf("failed to create stub transcript: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("failed to write stub transcript: %w", err)
	}
	return nil
}

// GetTranscriptPosition returns the current step count of a Devin transcript.
// Devin uses a JSON document with a steps array, so position is the number of
// steps. Returns 0 if the file doesn't exist or is empty.
func (a *Agent) GetTranscriptPosition(path string) (int, error) {
	if path == "" {
		return 0, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to read transcript file: %w", err)
	}
	if len(data) == 0 {
		return 0, nil
	}
	t, err := parseTranscript(data)
	if err != nil {
		return 0, err
	}
	return len(t.Steps), nil
}

// ExtractModifiedFiles extracts files modified since a given step index.
func (a *Agent) ExtractModifiedFiles(path string, offset int) ([]string, int, error) {
	if path == "" {
		return nil, 0, nil
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return nil, 0, nil
		}
		return nil, 0, fmt.Errorf("failed to read transcript file: %w", readErr)
	}
	t, parseErr := parseTranscript(data)
	if parseErr != nil {
		return nil, 0, parseErr
	}
	return extractModifiedFilesFromSteps(t.Steps, offset), len(t.Steps), nil
}

// extractModifiedFiles extracts all modified file paths from a raw transcript.
func extractModifiedFiles(data []byte) ([]string, error) {
	t, err := parseTranscript(data)
	if err != nil {
		return nil, err
	}
	return extractModifiedFilesFromSteps(t.Steps, 0), nil
}

// extractModifiedFilesFromSteps collects file paths from write/edit tool
// calls on agent steps, starting at the given step index, deduplicated in
// first-seen order.
func extractModifiedFilesFromSteps(steps []ATIFStep, startOffset int) []string {
	if startOffset < 0 {
		startOffset = 0
	}
	seen := make(map[string]struct{})
	var files []string
	for i := startOffset; i < len(steps); i++ {
		var info atifStepInfo
		if err := json.Unmarshal(steps[i], &info); err != nil {
			continue // Skip malformed steps
		}
		for _, call := range info.ToolCalls {
			if !isFileModificationTool(call.FunctionName) {
				continue
			}
			var input fileToolInput
			if err := json.Unmarshal(call.Arguments, &input); err != nil || input.FilePath == "" {
				continue
			}
			if _, ok := seen[input.FilePath]; !ok {
				seen[input.FilePath] = struct{}{}
				files = append(files, input.FilePath)
			}
		}
	}
	return files
}

// ExtractPrompts returns user prompts from the transcript starting at the
// given step index.
func (a *Agent) ExtractPrompts(sessionRef string, offset int) ([]string, error) {
	data, err := os.ReadFile(sessionRef)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read transcript file: %w", err)
	}
	t, err := parseTranscript(data)
	if err != nil {
		return nil, err
	}
	if offset < 0 {
		offset = 0
	}
	var prompts []string
	for i := offset; i < len(t.Steps); i++ {
		var info atifStepInfo
		if err := json.Unmarshal(t.Steps[i], &info); err != nil {
			continue
		}
		if info.Source == "user" && info.Message != "" {
			prompts = append(prompts, info.Message)
		}
	}
	return prompts, nil
}

// ExtractSummary reports that Devin transcripts carry no native summary.
func (a *Agent) ExtractSummary(_ string) (string, bool, error) {
	return "", false, nil
}

// CalculateTokens computes token usage from the transcript starting at the
// given step offset. Devin's per-step metrics report prompt_tokens inclusive
// of cache reads, so fresh input is prompt_tokens - cached_tokens.
func (a *Agent) CalculateTokens(data []byte, offset int) (protocol.TokenUsageResponse, error) {
	t, err := parseTranscript(data)
	if err != nil {
		return protocol.TokenUsageResponse{}, err
	}
	if offset < 0 {
		offset = 0
	}
	usage := protocol.TokenUsageResponse{}
	for i := offset; i < len(t.Steps); i++ {
		var info atifStepInfo
		if err := json.Unmarshal(t.Steps[i], &info); err != nil || info.Metrics == nil {
			continue
		}
		fresh := info.Metrics.PromptTokens - info.Metrics.CachedTokens
		if fresh < 0 {
			fresh = 0
		}
		usage.InputTokens += fresh
		usage.CacheReadTokens += info.Metrics.CachedTokens
		usage.OutputTokens += info.Metrics.CompletionTokens
		usage.APICallCount++
	}
	return usage, nil
}

// ChunkTranscript splits an ATIF transcript by distributing steps across
// chunks, preserving the envelope (schema_version, session_id, agent,
// final_metrics) in each chunk so every chunk is independently parseable.
func (a *Agent) ChunkTranscript(content []byte, maxSize int) ([][]byte, error) {
	if maxSize <= 0 {
		return nil, fmt.Errorf("invalid max size %d", maxSize)
	}
	t, err := parseTranscript(content)
	if err != nil {
		// Not an ATIF document (e.g. protocol round-trip data): chunk by raw
		// size so the content still survives storage limits losslessly.
		return chunkBytes(content, maxSize), nil //nolint:nilerr // fallback path, not an error
	}
	if len(t.Steps) == 0 || len(content) <= maxSize {
		return [][]byte{content}, nil
	}

	envelope := ATIFTranscript{
		SchemaVersion: t.SchemaVersion,
		SessionID:     t.SessionID,
		Agent:         t.Agent,
		FinalMetrics:  t.FinalMetrics,
	}
	envelopeBytes, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal envelope for chunking: %w", err)
	}
	baseSize := len(envelopeBytes) + len(`,"steps":[]`)

	var chunks [][]byte
	var current []ATIFStep
	currentSize := baseSize

	flush := func() error {
		if len(current) == 0 {
			return nil
		}
		chunk := envelope
		chunk.Steps = current
		data, err := json.Marshal(chunk)
		if err != nil {
			return fmt.Errorf("failed to marshal chunk: %w", err)
		}
		chunks = append(chunks, data)
		current = nil
		currentSize = baseSize
		return nil
	}

	for _, step := range t.Steps {
		stepSize := len(step) + 1 // +1 for comma separator
		if currentSize+stepSize > maxSize && len(current) > 0 {
			if err := flush(); err != nil {
				return nil, err
			}
		}
		current = append(current, step)
		currentSize += stepSize
	}
	if err := flush(); err != nil {
		return nil, err
	}
	if len(chunks) == 0 {
		return nil, errors.New("failed to create any chunks")
	}
	return chunks, nil
}

// chunkBytes splits content into maxSize-byte chunks (non-ATIF fallback).
func chunkBytes(content []byte, maxSize int) [][]byte {
	if len(content) <= maxSize {
		return [][]byte{content}
	}
	var chunks [][]byte
	for len(content) > maxSize {
		chunks = append(chunks, content[:maxSize])
		content = content[maxSize:]
	}
	return append(chunks, content)
}

// ReassembleTranscript merges ATIF chunks by concatenating their step arrays.
// The envelope is taken from the first chunk.
func (a *Agent) ReassembleTranscript(chunks [][]byte) ([]byte, error) {
	if len(chunks) == 0 {
		return nil, errors.New("no chunks to reassemble")
	}
	if _, err := parseTranscript(chunks[0]); err != nil {
		// Raw-size chunks from the non-ATIF fallback: concatenate.
		var out []byte
		for _, chunk := range chunks {
			out = append(out, chunk...)
		}
		return out, nil
	}
	var result *ATIFTranscript
	for i, chunk := range chunks {
		t, err := parseTranscript(chunk)
		if err != nil {
			return nil, fmt.Errorf("failed to parse chunk %d: %w", i, err)
		}
		if i == 0 {
			result = t
			continue
		}
		result.Steps = append(result.Steps, t.Steps...)
	}
	data, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal reassembled transcript: %w", err)
	}
	return data, nil
}
