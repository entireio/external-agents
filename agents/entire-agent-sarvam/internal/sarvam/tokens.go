package sarvam

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/entireio/external-agents/agents/entire-agent-sarvam/internal/protocol"
)

// CalculateTokens sums per-API-call usage from token_count records at or
// after the given line offset. Sarvam re-emits token_count events whose
// total_token_usage repeats the previous event's total (432 of 911 events in
// one sampled session), so per-event last_token_usage cannot be summed — that
// roughly doubles the reported usage. The window's usage is instead the delta
// between the cumulative total_token_usage of the last counted event and the
// total as of the offset, and an event is an API call only when its total
// advances.
//
// info.input_tokens includes info.cached_input_tokens, so the input bucket
// reports the non-cached share and the cached share is reported as cache
// reads. Events with a null info (rate-limit snapshots) change nothing.
//
// The records are event_msg lines in the rollout JSONL, so the token_count
// payload is nested under "payload". The offset is a physical line index —
// the same unit this agent's get-transcript-position and extract-* report,
// and the unit Entire feeds back from them. Lines are read exactly the way
// readRolloutLines reads them: with no fixed per-line cap — one record can
// embed a whole tool output, and a size cap here would fail token usage for
// the whole checkpoint — and a final line without a trailing newline is
// excluded, so it is not counted now and recounted once its newline
// arrives.
func (a *Agent) CalculateTokens(transcript []byte, offset int) (protocol.TokenUsageResponse, error) {
	usage := protocol.TokenUsageResponse{
		CacheCreationTokens: 0,
		SubagentTokens:      nil,
	}

	reader := bufio.NewReaderSize(bytes.NewReader(transcript), 64*1024)
	index := 0
	prev := rolloutTokenUsage{}
	for {
		raw, readErr := reader.ReadBytes('\n')
		if readErr != nil {
			// A final unterminated line is not consumed by the transcript
			// position; counting it here would only recount it once its
			// newline arrives.
			if errors.Is(readErr, io.EOF) {
				return usage, nil
			}
			return protocol.TokenUsageResponse{}, readErr
		}
		index++
		line := strings.TrimSpace(string(raw))
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
		total := count.Info.TotalTokenUsage
		if total == prev {
			// A re-emitted snapshot of the same cumulative total is not
			// another API call.
			continue
		}
		if index <= offset {
			prev = total
			continue
		}
		usage.APICallCount++
		usage.InputTokens += nonNegative(nonNegative(total.InputTokens-prev.InputTokens) - nonNegative(total.CachedInputTokens-prev.CachedInputTokens))
		usage.CacheReadTokens += nonNegative(total.CachedInputTokens - prev.CachedInputTokens)
		usage.OutputTokens += nonNegative(total.OutputTokens - prev.OutputTokens)
		prev = total
	}
}

// nonNegative clamps a usage delta at zero: cumulative totals only grow in a
// well-formed rollout, and a malformed one must not report negative tokens.
func nonNegative(delta int) int {
	if delta < 0 {
		return 0
	}
	return delta
}
