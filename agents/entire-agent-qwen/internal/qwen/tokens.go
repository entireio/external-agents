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
// a turn's last API call can still be on its way when the Stop hook reads the
// log. Stop therefore waits, up to finalRecordWait, until the log holds the
// model's answer to the turn's last prompt. A call that arrives later still is
// copied at the next sync and counted with the turn and the checkpoint in
// which it was copied: totals stay right, but one turn's share can move to the
// next.

const eventAssistantUsage = "AssistantUsage"

// eventUserPromptSubmit is the event of the sidecar record a turn starts with.
// Qwen 0.26.9 also fires UserPromptSubmit, with an empty prompt, before the
// call that follows a tool round, so each API call of a turn follows one.
const eventUserPromptSubmit = "UserPromptSubmit"

// Events of the sidecar records that end a turn or start or end a session.
const (
	eventStop         = "Stop"
	eventStopFailure  = "StopFailure"
	eventSessionStart = "SessionStart"
	eventSessionEnd   = "SessionEnd"
)

// Types of chat-log records. Assistant records carry usage; user and
// tool_result records come between them; system records (telemetry, titles,
// compression) come and go around them.
const (
	nativeTypeAssistant = "assistant"
	nativeTypeSystem    = "system"
)

// finalRecordWait bounds how long Stop waits for the turn's final assistant
// record to reach the log, polling every finalRecordPoll. Qwen queues that
// record before it fires Stop, so the wait ends once that queued write lands.
const (
	finalRecordWait = time.Second
	finalRecordPoll = 25 * time.Millisecond
)

// promptHead is how much of a sidecar line too long to hold is read for its
// event and ts, which the sidecar writes ahead of the prompt.
const promptHead = 4 * 1024

// maxNativeLine bounds one line read while syncing usage. A native record can
// embed a whole tool result or written file, and a sidecar record a whole tool
// input or response, so a line can be very long; a longer one is skipped
// rather than failing the sync.
const maxNativeLine = 64 * 1024 * 1024

// usageMetadataKey prefilters native lines: only a line containing it can
// carry usage, so unless a Stop is waiting for the final record the rest are
// never decoded.
var usageMetadataKey = []byte(`"usageMetadata"`)

// nativeUUIDKey prefilters sidecar lines the same way for AssistantUsage
// records, and promptEventKey for UserPromptSubmit records.
var (
	nativeUUIDKey  = []byte(`"native_uuid"`)
	promptEventKey = []byte(`"` + eventUserPromptSubmit + `"`)
)

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
	// Message is kept raw: only a Stop waiting for the turn's answer reads
	// it, and a shape it does not expect must not cost the record its usage.
	Message json.RawMessage `json:"message"`
}

// callsTools reports whether an assistant record asks for a tool call, after
// which Qwen sends the results back and the turn goes on.
func (r nativeChatRecord) callsTools() bool {
	var message struct {
		Parts []struct {
			FunctionCall json.RawMessage `json:"functionCall"`
		} `json:"parts"`
	}
	if json.Unmarshal(r.Message, &message) != nil {
		return false
	}
	for _, part := range message.Parts {
		if len(part.FunctionCall) > 0 && !bytes.Equal(part.FunctionCall, []byte("null")) {
			return true
		}
	}
	return false
}

// usageSyncState is what the sidecar already records about native usage.
type usageSyncState struct {
	// anchor is the last AssistantUsage record copied from the native log in
	// use: the sync resumes after its line when that line still holds the
	// same record.
	anchor *sidecarRecord
	// floor is the time of the sidecar's first record written by an adapter
	// that copies usage (sidecarVersion or later). A native record older than
	// it predates the part of the session whose usage the sidecar records and
	// is not copied: the history of a resumed session whose sidecar was
	// cleaned out of the temp directory, or of a session whose sidecar an
	// older adapter started, which would otherwise all land in the first turn
	// after the upgrade. A turn an older adapter's records leave open (a
	// typed prompt, not the empty one before a tool round's next call, with
	// no end of turn or session after it) moves the floor back to its prompt,
	// so an upgrade in the middle of a turn still copies that turn's calls,
	// whichever hook the upgraded adapter runs first.
	floor time.Time
	// prompt is the time of the sidecar's last UserPromptSubmit record, zero
	// when there is none. The answer to it is no older. A copy can come after
	// the prompt and still be older, when a log rewrite moved the anchored
	// line and a sync in the middle of the turn copied the log again.
	prompt time.Time
}

// syncNativeUsage copies into the sidecar the usage of assistant records the
// native log gained since the last sync. At Stop it first gives the turn's
// final record finalRecordWait to arrive. It is best effort for the hook that
// calls it: an unreadable native log costs the usage of this sync, never the
// hook.
func (a *Agent) syncNativeUsage(hookName string, raw qwenHookInputRaw) error {
	nativePath := strings.TrimSpace(raw.TranscriptPath)
	if nativePath == "" {
		return nil
	}
	sidecarPath := a.sidecarPath(raw.SessionID)
	state, err := readUsageSyncState(sidecarPath, nativePath, raw.Timestamp)
	if err != nil {
		return err
	}
	scan := &nativeScan{path: nativePath, sessionID: raw.SessionID, state: state, offset: -1, waits: hookName == HookNameStop}
	pending, err := scan.next()
	for waited := time.Duration(0); err == nil && pending && waited < finalRecordWait; waited += finalRecordPoll {
		time.Sleep(finalRecordPoll)
		pending, err = scan.next()
	}
	if len(scan.records) == 0 {
		return err
	}
	return errors.Join(err, appendSidecarRecords(sidecarPath, scan.records))
}

// readUsageSyncState reads the floor from the start of the sidecar, and the
// anchor for nativePath and the last prompt from its end, so a hook
// reads what was appended since the last copy, not the whole session. An empty
// or missing sidecar takes its floor from the hook now running.
func readUsageSyncState(sidecarPath, nativePath, hookTimestamp string) (usageSyncState, error) {
	state := usageSyncState{floor: parseTimestamp(hookTimestamp)}
	f, err := os.Open(sidecarPath)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	defer func() { _ = f.Close() }()

	reader := bufio.NewReaderSize(f, 64*1024)
	var open time.Time // the prompt of the turn older records leave open
	for {
		line, _, complete, err := readLine(reader, maxNativeLine)
		if err != nil {
			return state, err
		}
		if !complete {
			break
		}
		var head struct {
			V      int    `json:"v"`
			Event  string `json:"event"`
			TS     string `json:"ts"`
			Prompt string `json:"prompt"`
		}
		if json.Unmarshal(line, &head) != nil {
			continue
		}
		typed := head.Event == eventUserPromptSubmit && strings.TrimSpace(head.Prompt) != ""
		if head.V >= sidecarVersion {
			// A session or turn the upgraded adapter starts closes the turn
			// older records left open.
			if typed || head.Event == eventSessionStart {
				open = time.Time{}
			}
			state.floor = parseTimestamp(head.TS)
			break
		}
		switch head.Event {
		case eventUserPromptSubmit:
			if at := parseTimestamp(head.TS); typed && !at.IsZero() {
				open = at
			}
		case eventStop, eventStopFailure, eventSessionStart, eventSessionEnd:
			open = time.Time{}
		}
	}
	if !open.IsZero() {
		state.floor = open
	}
	state.anchor, state.prompt, err = lastUsageRecord(f, nativePath)
	return state, err
}

// lastUsageRecord returns the last AssistantUsage record in the sidecar that
// was copied from nativePath, and the time of the last UserPromptSubmit record,
// reading the sidecar backwards from its end until it has found both. A
// trailing line without its newline is still being written and is skipped.
// AssistantUsage lines are small, so a line longer than one block is never
// held in memory: only its first promptHead bytes are read, in case it is a
// long prompt.
func lastUsageRecord(f *os.File, nativePath string) (*sidecarRecord, time.Time, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, time.Time{}, err
	}
	const block = 64 * 1024
	var (
		pos      = info.Size()
		carry    []byte // the end of a line whose start lies before pos
		trailing = true // the bytes after the last newline are no line yet
		long     bool   // the line in carry is longer than a block
		record   *sidecarRecord
		prompt   time.Time
	)
	found := func() bool { return record != nil && !prompt.IsZero() }
	// visit notes line when it is the anchor or the last prompt.
	visit := func(line []byte) {
		if record == nil {
			if record = usageRecordFrom(line, nativePath); record != nil {
				return
			}
		}
		if prompt.IsZero() {
			prompt = promptTime(line)
		}
	}
	for pos > 0 {
		n := min(int64(block), pos)
		pos -= n
		data := make([]byte, n, n+int64(len(carry)))
		if _, err := f.ReadAt(data, pos); err != nil {
			return nil, time.Time{}, err
		}
		data = append(data, carry...)
		for {
			i := bytes.LastIndexByte(data, '\n')
			if i < 0 {
				break
			}
			line := data[i+1:]
			data = data[:i]
			switch {
			case trailing:
				trailing, long = false, false
			case long:
				long = false
				if prompt.IsZero() {
					prompt = longPromptTime(f, pos+int64(i)+1)
				}
			default:
				visit(line)
			}
			if found() {
				return record, prompt, nil
			}
		}
		carry = data
		if len(carry) > block {
			carry, long = nil, true
		}
	}
	switch {
	case trailing:
	case long:
		if prompt.IsZero() {
			prompt = longPromptTime(f, 0)
		}
	default:
		visit(carry)
	}
	return record, prompt, nil
}

// longPromptTime is promptTime for the sidecar line that starts at start and
// is too long to hold, read from its first promptHead bytes.
func longPromptTime(f *os.File, start int64) time.Time {
	head := make([]byte, promptHead)
	n, err := f.ReadAt(head, start)
	if err != nil && !errors.Is(err, io.EOF) {
		return time.Time{}
	}
	return promptTime(head[:n])
}

// promptTime returns the time of a sidecar line that is a UserPromptSubmit
// record, or zero. The line may be cut off anywhere after its event and ts.
func promptTime(line []byte) time.Time {
	if !bytes.Contains(line, promptEventKey) {
		return time.Time{}
	}
	decoder := json.NewDecoder(bytes.NewReader(line))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return time.Time{}
	}
	var event, ts string
	for (event == "" || ts == "") && decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			break
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			break
		}
		switch key {
		case "event":
			_ = json.Unmarshal(value, &event)
		case "ts":
			_ = json.Unmarshal(value, &ts)
		}
	}
	if event != eventUserPromptSubmit {
		return time.Time{}
	}
	return parseTimestamp(ts)
}

// usageRecordFrom decodes a sidecar line that is an AssistantUsage record
// copied from nativePath, or returns nil.
func usageRecordFrom(line []byte, nativePath string) *sidecarRecord {
	if !bytes.Contains(line, nativeUUIDKey) {
		return nil
	}
	var record sidecarRecord
	if json.Unmarshal(line, &record) != nil || !isUsageRecord(record) || record.NativeUUID == "" ||
		record.NativeTranscriptPath != nativePath {
		return nil
	}
	return &record
}

// nativeScan reads the native log forward from where its last read stopped,
// collecting an AssistantUsage record for each assistant record that carries
// usage. Its first read resumes after the anchor when that line still holds
// the anchored record. Otherwise it reads the whole log (Qwen can rewrite it,
// e.g. when prompts are deleted) and copies again what an earlier sync
// copied; CalculateTokens counts each call once.
type nativeScan struct {
	path      string
	sessionID string
	state     usageSyncState
	// offset is where the next read starts, just past the last complete
	// line; -1 until the first read places it.
	offset int64
	// waits is set when the caller waits for the turn's final record: every
	// line is then decoded so that answered is known.
	waits bool
	// info is the log as the last read found it.
	info os.FileInfo
	// answered is set while the last record read other than a system record
	// is the model's answer to the turn's last prompt.
	answered bool
	records  []sidecarRecord
}

// next reads what the native log gained since the last read. When the scan
// waits, it reports whether the turn's final record may still be on its way:
// the log ends with a line still being written, which is left for a later
// read, or with anything other than the answer to the turn's last prompt.
func (s *nativeScan) next() (bool, error) {
	f, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		// Qwen creates the log with the first record it writes, through the
		// same queue, so it can still be missing when Stop fires. With chat
		// recording off, hooks name no log and no sync runs.
		return s.waits, nil
	}
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, nil
	}
	// A log replaced since the last read (Qwen rewrites it by renaming a new
	// file over it) or cut shorter is read again from the start.
	if s.info != nil && (!os.SameFile(s.info, info) || info.Size() < s.offset) {
		s.offset, s.answered = -1, false
	}
	s.info = info
	if s.offset < 0 {
		var anchored *nativeChatRecord
		s.offset, anchored = resumeOffset(f, s.state.anchor)
		s.answered = anchored != nil && s.state.answers(*anchored)
	}
	if _, err := f.Seek(s.offset, io.SeekStart); err != nil {
		return false, err
	}
	reader := bufio.NewReaderSize(f, 64*1024)
	for {
		line, n, complete, err := readLine(reader, maxNativeLine)
		if err != nil {
			return false, err
		}
		if !complete {
			return s.waits && (n > 0 || !s.answered), nil
		}
		lineStart := s.offset
		s.offset += n
		if !s.waits && !bytes.Contains(line, usageMetadataKey) {
			continue
		}
		var native nativeChatRecord
		if json.Unmarshal(line, &native) != nil {
			continue
		}
		if native.Type != nativeTypeSystem {
			s.answered = s.state.answers(native)
		}
		if !s.state.wants(native) {
			continue
		}
		s.records = append(s.records, sidecarRecord{
			V:                    sidecarVersion,
			Agent:                AgentName,
			Event:                eventAssistantUsage,
			SessionID:            s.sessionID,
			TS:                   native.Timestamp,
			NativeTranscriptPath: s.path,
			Model:                native.Model,
			NativeUUID:           native.UUID,
			NativeOffset:         lineStart,
			Usage:                native.UsageMetadata,
		})
	}
}

// wants reports whether a native record is an assistant record of this
// session, with usage, from no earlier than the floor.
func (s usageSyncState) wants(record nativeChatRecord) bool {
	if record.Type != nativeTypeAssistant || record.UsageMetadata == nil || record.UUID == "" ||
		record.IsSidechain || record.ForkedFrom != nil {
		return false
	}
	at := parseTimestamp(record.Timestamp)
	return s.floor.IsZero() || at.IsZero() || !at.Before(s.floor)
}

// answers reports whether a native record is the model's answer to the turn's
// last prompt: an assistant record that calls no tool, so the turn ends with
// it, from no earlier than that prompt.
func (s usageSyncState) answers(record nativeChatRecord) bool {
	if record.Type != nativeTypeAssistant || record.callsTools() {
		return false
	}
	at := parseTimestamp(record.Timestamp)
	return s.prompt.IsZero() || at.IsZero() || !at.Before(s.prompt)
}

// resumeOffset returns the offset just past the anchor's line, and the record
// on it, when that line still holds the anchored record, and 0 (read the
// whole log) otherwise.
func resumeOffset(f *os.File, anchor *sidecarRecord) (int64, *nativeChatRecord) {
	if anchor == nil || anchor.NativeUUID == "" || anchor.NativeOffset < 0 {
		return 0, nil
	}
	if _, err := f.Seek(anchor.NativeOffset, io.SeekStart); err != nil {
		return 0, nil
	}
	line, n, complete, err := readLine(bufio.NewReader(f), maxNativeLine)
	if err != nil || !complete {
		return 0, nil
	}
	var record nativeChatRecord
	if json.Unmarshal(line, &record) != nil || record.UUID != anchor.NativeUUID {
		return 0, nil
	}
	return anchor.NativeOffset + n, &record
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
// call recorded twice (by two hooks syncing at once, or copied again after Qwen
// rewrote its log), whether both copies follow offset or one precedes it, is
// counted once.
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
