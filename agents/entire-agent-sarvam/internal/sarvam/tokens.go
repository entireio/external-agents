package sarvam

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"

	"github.com/entireio/external-agents/agents/entire-agent-sarvam/internal/protocol"
)

// CalculateTokens sums per-API-call usage from token_count records at or
// after the given line offset. Sarvam emits one token_count event per API
// call carrying info.last_token_usage (input/cached-input/output/reasoning
// tokens); events with a null info (rate-limit snapshots only) count toward
// nothing. The records are event_msg lines in the rollout JSONL, so the
// token_count payload is nested under "payload".
//
// The offset is a physical line index — the same unit this agent's
// get-transcript-position and extract-* report, and the unit Entire feeds
// back from them. A byte offset here would re-count nearly the whole
// transcript on every checkpoint, so lines (complete and placeholder alike)
// are skipped by index, keeping the alignment the analyzer guarantees.
func (a *Agent) CalculateTokens(transcript []byte, offset int) (protocol.TokenUsageResponse, error) {
	usage := protocol.TokenUsageResponse{
		CacheCreationTokens: 0,
		SubagentTokens:      nil,
	}

	scanner := bufio.NewScanner(bytes.NewReader(transcript))
	scanner.Buffer(make([]byte, 64*1024), maxRolloutLine)
	index := 0
	for scanner.Scan() {
		index++
		if index <= offset {
			continue
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event rolloutLine
		if json.Unmarshal([]byte(line), &event) != nil || event.Type != "event_msg" {
			continue
		}
		var count rolloutTokenCount
		if json.Unmarshal(event.Payload, &count) != nil || count.Type != "token_count" {
			continue
		}
		if count.Info == nil {
			continue
		}
		usage.InputTokens += count.Info.LastTokenUsage.InputTokens
		usage.CacheReadTokens += count.Info.LastTokenUsage.CachedInputTokens
		usage.OutputTokens += count.Info.LastTokenUsage.OutputTokens
		usage.APICallCount++
	}
	if err := scanner.Err(); err != nil {
		return protocol.TokenUsageResponse{}, err
	}
	return usage, nil
}
