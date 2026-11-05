package dsh

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

const (
	// materializeWait bounds how long a hook waits for DSH's write-behind
	// buffer to publish the event it just observed.
	materializeWait = 3000 * time.Millisecond
	// materializePoll is the interval between re-reads of a native log.
	materializePoll = 60 * time.Millisecond
)

type sessionHeader struct {
	Type            string `json:"type"`
	Version         int    `json:"version"`
	ID              string `json:"id"`
	CreatedAt       int64  `json:"createdAt"`
	CWD             string `json:"cwd"`
	ParentSession   string `json:"parentSession"`
	IsSeeded        bool   `json:"isSeeded"`
	DelegationDepth int    `json:"delegationDepth"`
	AgentPreset     string `json:"agentPreset"`
}

type messageSource struct {
	Kind     string `json:"kind"`
	Form     string `json:"form"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	CallID   string `json:"callId"`
}

type contentBlock struct {
	Type      string `json:"type"`
	Text      string `json:"text"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chatMessage struct {
	ID         string         `json:"id"`
	Role       string         `json:"role"`
	Content    []contentBlock `json:"content"`
	Source     messageSource  `json:"source"`
	ToolCallID string         `json:"toolCallId"`
	IsError    bool           `json:"isError"`
}

type tokenUsage struct {
	InputTokens      int `json:"inputTokens"`
	OutputTokens     int `json:"outputTokens"`
	CacheReadTokens  int `json:"cacheReadTokens"`
	CacheWriteTokens int `json:"cacheWriteTokens"`
	TotalTokens      int `json:"totalTokens"`
}

type userMessageData struct {
	ID      string         `json:"id"`
	Role    string         `json:"role"`
	Content []contentBlock `json:"content"`
	Source  messageSource  `json:"source"`
}

type assistantMessageData struct {
	Turn        int         `json:"turn"`
	Step        int         `json:"step"`
	Message     chatMessage `json:"message"`
	Usage       *tokenUsage `json:"usage"`
	Interrupted bool        `json:"interrupted"`
}

type toolCallData struct {
	Turn      int    `json:"turn"`
	Step      int    `json:"step"`
	CallID    string `json:"callId"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type toolResultData struct {
	Turn    int         `json:"turn"`
	Step    int         `json:"step"`
	Message chatMessage `json:"message"`
	Error   *struct {
		Name   string `json:"name"`
		Code   string `json:"code"`
		Reason string `json:"reason"`
	} `json:"error"`
	Meta json.RawMessage `json:"meta"`
}

type eventEnvelope[T any] struct {
	Type string `json:"type"`
	Seq  int    `json:"seq"`
	Time int64  `json:"time"`
	Data T      `json:"data"`
}

// parseSessionHeader reads the first JSONL record of a DSH log.
func parseSessionHeader(data []byte) (sessionHeader, error) {
	line := firstNonEmptyLine(data)
	if len(line) == 0 {
		return sessionHeader{}, errors.New("empty DSH transcript")
	}
	line = bytes.TrimPrefix(line, []byte{0xEF, 0xBB, 0xBF})
	var header sessionHeader
	if err := json.Unmarshal(line, &header); err != nil {
		return sessionHeader{}, fmt.Errorf("parse DSH session header: %w", err)
	}
	if header.Type != "session" || header.ID == "" {
		return sessionHeader{}, errors.New("not a DSH session header")
	}
	return header, nil
}

func firstNonEmptyLine(data []byte) []byte {
	for _, line := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		return line
	}
	return nil
}

// readTranscriptFile returns plaintext JSONL for a transcript reference. DSH
// stores its native log as Zstandard frames; a normalized cache is plaintext.
func readTranscriptFile(path string) ([]byte, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("transcript path is empty")
	}
	data, err := os.ReadFile(path) // #nosec G304 -- path is supplied by the Entire protocol or resolved from DSH storage
	if err != nil {
		return nil, fmt.Errorf("read transcript %s: %w", path, err)
	}
	if strings.HasSuffix(path, ".zstd") {
		return decodeZstdTolerant(data)
	}
	return data, nil
}

// decodeZstdTolerant decodes concatenated Zstandard frames and keeps the
// complete-line prefix when the final frame is still being written.
func decodeZstdTolerant(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}
	decoder, err := zstd.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("open DSH zstd log: %w", err)
	}
	plain, readErr := io.ReadAll(decoder)
	decoder.Close()
	if readErr != nil {
		plain = trimToLastNewline(plain)
		if len(plain) == 0 {
			return nil, fmt.Errorf("decode DSH zstd log: %w", readErr)
		}
		return plain, nil
	}
	return plain, nil
}

func trimToLastNewline(data []byte) []byte {
	if len(data) == 0 || data[len(data)-1] == '\n' {
		return data
	}
	index := bytes.LastIndexByte(data, '\n')
	if index < 0 {
		return nil
	}
	return data[:index+1]
}

// forEachEventLine walks non-blank JSONL lines, counting every line the same
// way regardless of whether it is a header or an event. The returned total is
// the transcript position; fn receives lines after offset.
func forEachEventLine(data []byte, offset int, fn func(line []byte)) int {
	if offset < 0 {
		offset = 0
	}
	lineNumber := 0
	for _, line := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		lineNumber++
		if fn != nil && lineNumber > offset {
			fn(line)
		}
	}
	return lineNumber
}

func countTranscriptLines(data []byte) int {
	return forEachEventLine(data, 0, nil)
}

// cacheMeta records the native artifact a normalized cache was built from.
type cacheMeta struct {
	NativePath string `json:"native_path"`
	Size       int64  `json:"size"`
	ModTimeNS  int64  `json:"mod_time_ns"`
}

func metaPathFor(ref string) string {
	return strings.TrimSuffix(ref, ".jsonl") + ".meta.json"
}

func cacheMatches(ref string, info os.FileInfo, nativePath string) bool {
	metaData, err := os.ReadFile(metaPathFor(ref)) // #nosec G304 -- sidecar beside the cache
	if err != nil {
		return false
	}
	var meta cacheMeta
	if err := json.Unmarshal(metaData, &meta); err != nil {
		return false
	}
	return meta.Size == info.Size() &&
		meta.ModTimeNS == info.ModTime().UnixNano() &&
		physicalPath(meta.NativePath) == physicalPath(nativePath)
}

type materializeOptions struct {
	// WaitTurn, when non-nil, waits for that turn's turn/end event.
	WaitTurn *int
	// WaitCompaction waits for a compaction/end event.
	WaitCompaction bool
	// WaitStable waits for the native file size to stop changing.
	WaitStable bool
}

// materializeTranscript resolves a session's native log, refreshes the
// normalized cache when needed, and returns both the cache path and bytes.
func (a *Agent) materializeTranscript(sessionID, repoRoot, cwdHint string, opts materializeOptions) (string, []byte, error) {
	dir, err := a.GetSessionDir(repoRoot)
	if err != nil {
		return "", nil, err
	}
	ref := a.ResolveSessionFile(dir, sessionID)
	if err := writeSessionIDSidecar(ref, sessionID); err != nil {
		return ref, nil, err
	}

	nativePath, nativeErr := findNativeSession(sessionID, repoRoot, cwdHint)
	if nativeErr != nil {
		if data, readErr := os.ReadFile(ref); readErr == nil { // #nosec G304 -- cache path derived by this package
			return ref, data, nil
		}
		return ref, nil, nativeErr
	}

	// Stat before the read, not after: if DSH appends between the two, the
	// recorded size stays older than the cache, so the next hook refreshes
	// instead of pinning the stale prefix.
	readNative := func() ([]byte, os.FileInfo, error) {
		info, statErr := os.Stat(nativePath)
		if statErr != nil {
			info = nil
		}
		data, readErr := readTranscriptFile(nativePath)
		if readErr != nil {
			return nil, info, readErr
		}
		return data, info, nil
	}

	data, info, readErr := readNative()
	if readErr != nil {
		if cached, cacheErr := os.ReadFile(ref); cacheErr == nil { // #nosec G304
			return ref, cached, nil
		}
		return ref, nil, readErr
	}

	if opts.WaitTurn != nil || opts.WaitCompaction || opts.WaitStable {
		deadline := time.Now().Add(materializeWait)
		lastSize := int64(-1)
		for {
			satisfied := true
			if opts.WaitTurn != nil && !hasTurnEnd(data, *opts.WaitTurn) {
				satisfied = false
			}
			if opts.WaitCompaction && !hasCompactionEnd(data) {
				satisfied = false
			}
			if opts.WaitStable {
				if info == nil || info.Size() == 0 || info.Size() != lastSize {
					satisfied = false
				}
			}
			if satisfied || time.Now().After(deadline) {
				break
			}
			time.Sleep(materializePoll)
			if refreshed, refreshedInfo, refreshErr := readNative(); refreshErr == nil {
				data, info = refreshed, refreshedInfo
				if info != nil {
					lastSize = info.Size()
				}
			}
		}
	}

	if info != nil && cacheMatches(ref, info, nativePath) {
		if cached, readErr := os.ReadFile(ref); readErr == nil { // #nosec G304
			return ref, cached, nil
		}
	}

	if len(data) > 0 && data[len(data)-1] != '\n' {
		data = append(data, '\n')
	}
	if err := writeFileAtomic(ref, data, 0o600); err != nil {
		return ref, nil, err
	}
	if info != nil {
		meta, marshalErr := json.MarshalIndent(cacheMeta{
			NativePath: physicalPath(nativePath),
			Size:       info.Size(),
			ModTimeNS:  info.ModTime().UnixNano(),
		}, "", "  ")
		if marshalErr == nil {
			_ = writeFileAtomic(metaPathFor(ref), append(meta, '\n'), 0o600)
		}
	}
	return ref, data, nil
}

func hasTurnEnd(data []byte, turn int) bool {
	found := false
	forEachEventLine(data, 0, func(line []byte) {
		if found {
			return
		}
		var event eventEnvelope[struct {
			Turn int `json:"turn"`
		}]
		if err := json.Unmarshal(line, &event); err != nil {
			return
		}
		if event.Type == "turn/end" && event.Data.Turn == turn {
			found = true
		}
	})
	return found
}

func hasCompactionEnd(data []byte) bool {
	found := false
	forEachEventLine(data, 0, func(line []byte) {
		if found {
			return
		}
		var event eventEnvelope[json.RawMessage]
		if err := json.Unmarshal(line, &event); err != nil {
			return
		}
		if event.Type == "compaction/end" {
			found = true
		}
	})
	return found
}

// writeFileAtomic writes through a temporary sibling and renames it into
// place, so a concurrent reader never observes a partial transcript.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create cache directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary cache file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("write cache file: %w", err)
	}
	if err := tmp.Chmod(perm); err != nil {
		cleanup()
		return fmt.Errorf("set cache file mode: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("sync cache file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close cache file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		// Windows cannot replace an existing file with Rename. Remove it and
		// retry once; a concurrent reader may briefly see no cache, which is
		// recoverable from the native log.
		if runtime.GOOS == "windows" && (os.IsExist(err) || errors.Is(err, os.ErrPermission)) {
			if removeErr := os.Remove(path); removeErr == nil || errors.Is(removeErr, os.ErrNotExist) {
				if retryErr := os.Rename(tmpName, path); retryErr == nil {
					return nil
				}
			}
		}
		_ = os.Remove(tmpName)
		return fmt.Errorf("publish cache file: %w", err)
	}
	return nil
}

// encodeZstdFrames re-frames normalized JSONL the way DSH writes a session:
// the header as its own frame and every event as its own frame.
func encodeZstdFrames(data []byte) ([]byte, error) {
	var out bytes.Buffer
	for _, line := range bytes.SplitAfter(data, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		frameLine := line
		if frameLine[len(frameLine)-1] != '\n' {
			frameLine = append(append([]byte(nil), frameLine...), '\n')
		}
		var frame bytes.Buffer
		encoder, err := zstd.NewWriter(&frame)
		if err != nil {
			return nil, fmt.Errorf("create zstd frame: %w", err)
		}
		if _, err := encoder.Write(frameLine); err != nil {
			_ = encoder.Close()
			return nil, fmt.Errorf("write zstd frame: %w", err)
		}
		if err := encoder.Close(); err != nil {
			return nil, fmt.Errorf("close zstd frame: %w", err)
		}
		out.Write(frame.Bytes())
	}
	return out.Bytes(), nil
}

func nativeGenerationName(version int) string {
	if version <= 0 {
		return "session.jsonl.zstd"
	}
	return fmt.Sprintf("session.v%d.jsonl.zstd", version)
}
