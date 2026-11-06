package qwen

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"time"

	"github.com/entireio/external-agents/agents/entire-agent-qwen/internal/protocol"
)

// Token usage.
//
// Qwen's hook payloads carry no token counts, but its own chat log does: each
// assistant record in ~/.qwen/projects/<project>/chats/<session>.jsonl, the
// file the hooks name as transcript_path, carries the provider's usageMetadata
// for that API call (ChatRecordingService.recordAssistantTurn).
//
// The sidecar stays the session transcript. It is what Entire stores in a
// checkpoint and hands back to calculate-tokens later, possibly on a machine
// where the native log does not exist, so calculate-tokens cannot read the
// native log itself. Instead, at the hooks usageSyncHooks lists, the adapter
// copies the usage of each assistant record the native log gained since the
// last copy into the sidecar as an AssistantUsage record, and calculate-tokens
// sums the AssistantUsage records after --offset. They carry no "type", so
// Entire's compact transcript drops them.
//
// Qwen queues its log writes and fires Stop without waiting for the queue, so
// a turn's last API call can reach the log after the Stop hook has read it.
// That call is copied at the next sync and counted with the turn and the
// checkpoint in which it was copied: totals stay right, but one turn's share
// can move to the next.

const eventAssistantUsage = "AssistantUsage"

// nativeTypeAssistant is the type of the chat-log records that carry usage.
const nativeTypeAssistant = "assistant"

// maxNativeLine bounds one line read while syncing usage. A native record can
// embed a whole tool result or written file, and a sidecar record a whole tool
// input or response, so a line can be very long; a longer one is skipped
// rather than failing the sync.
const maxNativeLine = 64 * 1024 * 1024

// usageMetadataKey prefilters native lines: only a line containing it can
// carry usage, so the rest are never decoded.
var usageMetadataKey = []byte(`"usageMetadata"`)

// nativeUUIDKey prefilters sidecar lines the same way for AssistantUsage
// records.
var nativeUUIDKey = []byte(`"native_uuid"`)

// syncsUsage reports whether a hook copies new native usage into the sidecar:
// PreToolUse, so a commit a tool makes mid-turn sees the API calls before it,
// and every hook that ends a turn, a subagent or the session.
func syncsUsage(hookName string) bool {
	switch hookName {
	case HookNamePreToolUse, HookNameStop, HookNameStopFailure, HookNameSubagentStop, HookNameSessionEnd:
		return true
	default:
		return false
	}
}

// nativeChatRecord is the part of a Qwen chat-log record a usage sync reads.
type nativeChatRecord struct {
	UUID          string             `json:"uuid"`
	Type          string             `json:"type"`
	Timestamp     string             `json:"timestamp"`
	Model         string             `json:"model"`
	UsageMetadata *qwenUsageMetadata `json:"usageMetadata"`
	// IsSidechain marks a subagent's record. Qwen writes those to their own
	// files under subagents/, so one in the session log is not this session's.
	IsSidechain bool `json:"isSidechain"`
	// ForkedFrom marks a record /branch copied from the parent session: its
	// API call was made, and counted, there.
	ForkedFrom *struct{} `json:"forkedFrom"`
}

// usageSyncState is what the sidecar already records about native usage.
type usageSyncState struct {
	// copied holds the native UUID of every AssistantUsage record, so a call
	// is copied once even when the native log is rewritten or two hooks sync
	// at the same time.
	copied map[string]struct{}
	// anchor is the AssistantUsage record copied from the furthest line of
	// the native log in use: the sync resumes after that line when it still
	// holds the same record.
	anchor *sidecarRecord
	// floor is the time of the sidecar's first record. A native record older
	// than it predates the part of the session Entire saw, such as the
	// history of a resumed session whose sidecar was cleaned out of the temp
	// directory, and is not copied.
	floor time.Time
}

// syncNativeUsage copies into the sidecar the usage of assistant records the
// native log gained since the last sync. It is best effort for the hook that
// calls it: an unreadable native log costs the usage of this sync, never the
// hook.
func (a *Agent) syncNativeUsage(raw qwenHookInputRaw) error {
	nativePath := strings.TrimSpace(raw.TranscriptPath)
	if nativePath == "" {
		return nil
	}
	sidecarPath := a.sidecarPath(raw.SessionID)
	state, err := readUsageSyncState(sidecarPath, nativePath, raw.Timestamp)
	if err != nil {
		return err
	}
	records, err := scanNativeUsage(nativePath, raw.SessionID, state)
	if err != nil || len(records) == 0 {
		return err
	}
	return appendSidecarRecords(sidecarPath, records)
}

// readUsageSyncState reads the copied UUIDs, the anchor for nativePath and
// the floor from the sidecar. Only lines that can be AssistantUsage records
// are decoded, so the cost of a hook stays small as the sidecar grows. An
// empty or missing sidecar takes its floor from the hook now running.
func readUsageSyncState(sidecarPath, nativePath, hookTimestamp string) (usageSyncState, error) {
	state := usageSyncState{copied: map[string]struct{}{}}
	f, err := os.Open(sidecarPath)
	if errors.Is(err, os.ErrNotExist) {
		state.floor = parseTimestamp(hookTimestamp)
		return state, nil
	}
	if err != nil {
		return state, err
	}
	defer func() { _ = f.Close() }()

	reader := bufio.NewReaderSize(f, 64*1024)
	first := true
	for {
		line, _, complete, err := readLine(reader, maxNativeLine)
		if err != nil {
			return state, err
		}
		if !complete {
			break
		}
		if first && len(bytes.TrimSpace(line)) > 0 {
			first = false
			var head struct {
				TS string `json:"ts"`
			}
			if json.Unmarshal(line, &head) == nil {
				state.floor = parseTimestamp(head.TS)
			}
		}
		if !bytes.Contains(line, nativeUUIDKey) {
			continue
		}
		var record sidecarRecord
		if json.Unmarshal(line, &record) != nil || !isUsageRecord(record) || record.NativeUUID == "" {
			continue
		}
		state.copied[record.NativeUUID] = struct{}{}
		if record.NativeTranscriptPath == nativePath && (state.anchor == nil || record.NativeOffset > state.anchor.NativeOffset) {
			anchor := record
			state.anchor = &anchor
		}
	}
	if first {
		state.floor = parseTimestamp(hookTimestamp)
	}
	return state, nil
}

// scanNativeUsage returns an AssistantUsage record for each assistant record
// in the native log that carries usage and has not been copied. It resumes
// after the anchor when that line still holds the anchored record and reads
// the whole log otherwise (Qwen can rewrite it, e.g. when prompts are
// deleted), relying on the copied UUIDs to skip what is already recorded. A
// trailing line without its newline is still being written and is left for
// the next sync.
func scanNativeUsage(nativePath, sessionID string, state usageSyncState) ([]sidecarRecord, error) {
	f, err := os.Open(nativePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, nil
	}

	offset := resumeOffset(f, state.anchor)
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}
	reader := bufio.NewReaderSize(f, 64*1024)
	var records []sidecarRecord
	for {
		line, n, complete, err := readLine(reader, maxNativeLine)
		if err != nil {
			return nil, err
		}
		if !complete {
			return records, nil
		}
		lineStart := offset
		offset += n
		native, ok := nativeUsage(line, state)
		if !ok {
			continue
		}
		state.copied[native.UUID] = struct{}{}
		records = append(records, sidecarRecord{
			V:                    1,
			Agent:                AgentName,
			Event:                eventAssistantUsage,
			SessionID:            sessionID,
			TS:                   native.Timestamp,
			NativeTranscriptPath: nativePath,
			Model:                native.Model,
			NativeUUID:           native.UUID,
			NativeOffset:         lineStart,
			Usage:                native.UsageMetadata,
		})
	}
}

// nativeUsage decodes a native line that is an assistant record of this
// session with usage not yet copied.
func nativeUsage(line []byte, state usageSyncState) (nativeChatRecord, bool) {
	if !bytes.Contains(line, usageMetadataKey) {
		return nativeChatRecord{}, false
	}
	var record nativeChatRecord
	if json.Unmarshal(line, &record) != nil {
		return nativeChatRecord{}, false
	}
	if record.Type != nativeTypeAssistant || record.UsageMetadata == nil || record.UUID == "" ||
		record.IsSidechain || record.ForkedFrom != nil {
		return nativeChatRecord{}, false
	}
	if _, copied := state.copied[record.UUID]; copied {
		return nativeChatRecord{}, false
	}
	if at := parseTimestamp(record.Timestamp); !state.floor.IsZero() && !at.IsZero() && at.Before(state.floor) {
		return nativeChatRecord{}, false
	}
	return record, true
}

// resumeOffset returns the offset just past the anchor's line when that line
// still holds the anchored record, and 0 (read the whole log) otherwise.
func resumeOffset(f *os.File, anchor *sidecarRecord) int64 {
	if anchor == nil || anchor.NativeUUID == "" || anchor.NativeOffset < 0 {
		return 0
	}
	if _, err := f.Seek(anchor.NativeOffset, io.SeekStart); err != nil {
		return 0
	}
	line, n, complete, err := readLine(bufio.NewReader(f), maxNativeLine)
	if err != nil || !complete {
		return 0
	}
	var head struct {
		UUID string `json:"uuid"`
	}
	if json.Unmarshal(line, &head) != nil || head.UUID != anchor.NativeUUID {
		return 0
	}
	return anchor.NativeOffset + n
}

// readLine reads one newline-terminated line and reports how many bytes it
// consumed. complete is false at end of input, including after a trailing
// line with no newline, which is not consumed as a line. A line longer than
// limit is consumed and returned as nil.
func readLine(r *bufio.Reader, limit int) ([]byte, int64, bool, error) {
	var (
		line      []byte
		n         int64
		oversized bool
	)
	for {
		chunk, err := r.ReadSlice('\n')
		n += int64(len(chunk))
		if !oversized {
			if len(line)+len(chunk) > limit {
				oversized, line = true, nil
			} else {
				line = append(line, chunk...)
			}
		}
		switch {
		case err == nil:
			return line, n, true, nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF):
			return nil, n, false, nil
		default:
			return nil, n, false, err
		}
	}
}

func parseTimestamp(value string) time.Time {
	at, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}
	}
	return at
}

func isUsageRecord(record sidecarRecord) bool {
	return record.Event == eventAssistantUsage && record.Usage != nil
}

// CalculateTokens sums the AssistantUsage records at or after offset, a
// sidecar line index like every other offset this adapter reports. A native
// call recorded twice, by two hooks syncing at once or once before offset and
// again after it, is counted once.
func (a *Agent) CalculateTokens(data []byte, offset int) (protocol.TokenUsageResponse, error) {
	records, err := decodeSidecarRecords(bytes.NewReader(data))
	if err != nil {
		return protocol.TokenUsageResponse{}, err
	}
	offset = min(max(offset, 0), len(records))
	counted := map[string]struct{}{}
	for _, record := range records[:offset] {
		if isUsageRecord(record) && record.NativeUUID != "" {
			counted[record.NativeUUID] = struct{}{}
		}
	}
	var usage protocol.TokenUsageResponse
	for _, record := range records[offset:] {
		if !isUsageRecord(record) {
			continue
		}
		if record.NativeUUID != "" {
			if _, dup := counted[record.NativeUUID]; dup {
				continue
			}
			counted[record.NativeUUID] = struct{}{}
		}
		record.Usage.addTo(&usage)
	}
	return usage, nil
}

// addTo adds one API call to total. Qwen's promptTokenCount is the whole
// prompt with cachedContentTokenCount included, as with Gemini and OpenAI,
// while Entire's InputTokens is fresh input only, so the cached share moves to
// CacheReadTokens. Qwen records no cache writes separately: for Anthropic
// they are folded into promptTokenCount (buildAnthropicUsageMetadata) and so
// count as input here.
func (u *qwenUsageMetadata) addTo(total *protocol.TokenUsageResponse) {
	prompt := max(u.PromptTokenCount, 0)
	cached := min(max(u.CachedContentTokenCount, 0), prompt)
	total.InputTokens += prompt - cached
	total.CacheReadTokens += cached
	total.OutputTokens += u.outputTokens()
	total.APICallCount++
}

// outputTokens counts reasoning once. OpenAI-compatible and Anthropic
// providers include it in candidatesTokenCount, while Gemini reports it only
// in thoughtsTokenCount. As in Qwen's own outputTokenCount
// (packages/core/src/telemetry/gen-ai-usage.ts), a total that lies between
// the two readings decides; without one, the OpenAI-style reading Qwen's
// default providers use applies.
func (u *qwenUsageMetadata) outputTokens() int {
	output := max(u.CandidatesTokenCount, 0)
	thoughts := max(u.ThoughtsTokenCount, 0)
	if u.TotalTokenCount > 0 {
		if derived := u.TotalTokenCount - max(u.PromptTokenCount, 0); derived >= output && derived <= output+thoughts {
			return derived
		}
	}
	return output
}
