package protocol

import (
	"encoding/json"
	"flag"
	"io"
	"os"
	"path/filepath"
	"time"
)

type sessionDirResolver interface {
	GetSessionDir(repoPath string) (string, error)
}

type sessionFileResolver interface {
	ResolveSessionFile(sessionDir, sessionID string) string
}

type sessionIDProvider interface {
	GetSessionID(*HookInputJSON) string
}

type sessionReader interface {
	ReadSession(*HookInputJSON) (AgentSessionJSON, error)
}

type sessionWriter interface {
	WriteSession(AgentSessionJSON) error
}

type transcriptReader interface {
	ReadTranscript(sessionRef string) ([]byte, error)
}

type transcriptChunker interface {
	ChunkTranscript(content []byte, maxSize int) ([][]byte, error)
	ReassembleTranscript(chunks [][]byte) ([]byte, error)
}

type resumeFormatter interface {
	FormatResumeCommand(sessionID string) string
}

type hookParser interface {
	ParseHook(hookName string, input []byte) (*EventJSON, error)
	InstallHooks(localDev bool, force bool) (int, error)
	UninstallHooks() error
	AreHooksInstalled() bool
}

type transcriptAnalyzer interface {
	GetTranscriptPosition(path string) (int, error)
	ExtractModifiedFiles(path string, offset int) ([]string, int, error)
	ExtractPrompts(sessionRef string, offset int) ([]string, error)
	ExtractSummary(sessionRef string) (string, bool, error)
}

type tokenCalculator interface {
	CalculateTokens(data []byte, offset int) (TokenUsageResponse, error)
}

func WriteJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func ReadJSON[T any](r io.Reader) (*T, error) {
	var value T
	if err := json.NewDecoder(r).Decode(&value); err != nil {
		return nil, err
	}
	return &value, nil
}

func RepoRoot() string {
	if root := os.Getenv("ENTIRE_REPO_ROOT"); root != "" {
		return root
	}
	root, _ := os.Getwd()
	return root
}

func HandleGetSessionID(stdin io.Reader, stdout io.Writer, provider sessionIDProvider) error {
	input, err := ReadJSON[HookInputJSON](stdin)
	if err != nil {
		return err
	}
	return WriteJSON(stdout, SessionIDResponse{SessionID: provider.GetSessionID(input)})
}

func HandleGetSessionDir(args []string, stdout io.Writer, resolver sessionDirResolver) error {
	fs := flag.NewFlagSet("get-session-dir", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repoPath := fs.String("repo-path", "", "repo path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	sessionDir, err := resolver.GetSessionDir(*repoPath)
	if err != nil {
		return err
	}
	return WriteJSON(stdout, SessionDirResponse{SessionDir: sessionDir})
}

func HandleResolveSessionFile(args []string, stdout io.Writer, resolver sessionFileResolver) error {
	fs := flag.NewFlagSet("resolve-session-file", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	sessionDir := fs.String("session-dir", "", "session dir")
	sessionID := fs.String("session-id", "", "session id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return WriteJSON(stdout, SessionFileResponse{SessionFile: resolver.ResolveSessionFile(*sessionDir, *sessionID)})
}

func HandleReadSession(stdin io.Reader, stdout io.Writer, reader sessionReader) error {
	input, err := ReadJSON[HookInputJSON](stdin)
	if err != nil {
		return err
	}
	session, err := reader.ReadSession(input)
	if err != nil {
		return err
	}
	return WriteJSON(stdout, session)
}

func HandleWriteSession(stdin io.Reader, writer sessionWriter) error {
	session, err := ReadJSON[AgentSessionJSON](stdin)
	if err != nil {
		return err
	}
	return writer.WriteSession(*session)
}

func HandleReadTranscript(args []string, stdout io.Writer, reader transcriptReader) error {
	fs := flag.NewFlagSet("read-transcript", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	sessionRef := fs.String("session-ref", "", "session ref")
	if err := fs.Parse(args); err != nil {
		return err
	}
	data, err := reader.ReadTranscript(*sessionRef)
	if err != nil {
		return err
	}
	_, err = stdout.Write(data)
	return err
}

func HandleChunkTranscript(args []string, stdin io.Reader, stdout io.Writer, chunker transcriptChunker) error {
	fs := flag.NewFlagSet("chunk-transcript", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	maxSize := fs.Int("max-size", 0, "max size")
	if err := fs.Parse(args); err != nil {
		return err
	}
	content, err := io.ReadAll(stdin)
	if err != nil {
		return err
	}
	chunks, err := chunker.ChunkTranscript(content, *maxSize)
	if err != nil {
		return err
	}
	return WriteJSON(stdout, ChunkResponse{Chunks: chunks})
}

func HandleReassembleTranscript(stdin io.Reader, stdout io.Writer, chunker transcriptChunker) error {
	input, err := ReadJSON[ChunkResponse](stdin)
	if err != nil {
		return err
	}
	data, err := chunker.ReassembleTranscript(input.Chunks)
	if err != nil {
		return err
	}
	_, err = stdout.Write(data)
	return err
}

func HandleFormatResumeCommand(args []string, stdout io.Writer, formatter resumeFormatter) error {
	fs := flag.NewFlagSet("format-resume-command", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	sessionID := fs.String("session-id", "", "session id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return WriteJSON(stdout, ResumeCommandResponse{Command: formatter.FormatResumeCommand(*sessionID)})
}

func readStdinWithTimeout(r io.Reader, timeout time.Duration) ([]byte, error) {
	type result struct {
		data []byte
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		data, err := io.ReadAll(r)
		ch <- result{data, err}
	}()
	select {
	case res := <-ch:
		return res.data, res.err
	case <-time.After(timeout):
		return nil, nil
	}
}

func HandleParseHook(args []string, stdin io.Reader, stdout io.Writer, parser hookParser) error {
	fs := flag.NewFlagSet("parse-hook", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	hook := fs.String("hook", "", "hook name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	input, err := readStdinWithTimeout(stdin, 100*time.Millisecond)
	if err != nil {
		return err
	}
	event, err := parser.ParseHook(*hook, input)
	if err != nil {
		return err
	}
	if event == nil {
		_, err = io.WriteString(stdout, "null\n")
		return err
	}
	return WriteJSON(stdout, event)
}

func HandleInstallHooks(args []string, stdout io.Writer, parser hookParser) error {
	fs := flag.NewFlagSet("install-hooks", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	localDev := fs.Bool("local-dev", false, "local dev")
	force := fs.Bool("force", false, "force")
	if err := fs.Parse(args); err != nil {
		return err
	}
	count, err := parser.InstallHooks(*localDev, *force)
	if err != nil {
		return err
	}
	return WriteJSON(stdout, HooksInstalledCountResponse{HooksInstalled: count})
}

func HandleGetTranscriptPosition(args []string, stdout io.Writer, analyzer transcriptAnalyzer) error {
	fs := flag.NewFlagSet("get-transcript-position", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	path := fs.String("path", "", "path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	position, err := analyzer.GetTranscriptPosition(*path)
	if err != nil {
		return err
	}
	return WriteJSON(stdout, TranscriptPositionResponse{Position: position})
}

func HandleExtractModifiedFiles(args []string, stdout io.Writer, analyzer transcriptAnalyzer) error {
	fs := flag.NewFlagSet("extract-modified-files", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	path := fs.String("path", "", "path")
	offset := fs.Int("offset", 0, "offset")
	if err := fs.Parse(args); err != nil {
		return err
	}
	files, currentPosition, err := analyzer.ExtractModifiedFiles(*path, *offset)
	if err != nil {
		return err
	}
	return WriteJSON(stdout, ExtractFilesResponse{Files: files, CurrentPosition: currentPosition})
}

func HandleExtractPrompts(args []string, stdout io.Writer, analyzer transcriptAnalyzer) error {
	fs := flag.NewFlagSet("extract-prompts", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	sessionRef := fs.String("session-ref", "", "session ref")
	offset := fs.Int("offset", 0, "offset")
	if err := fs.Parse(args); err != nil {
		return err
	}
	prompts, err := analyzer.ExtractPrompts(*sessionRef, *offset)
	if err != nil {
		return err
	}
	return WriteJSON(stdout, ExtractPromptsResponse{Prompts: prompts})
}

func HandleExtractSummary(args []string, stdout io.Writer, analyzer transcriptAnalyzer) error {
	fs := flag.NewFlagSet("extract-summary", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	sessionRef := fs.String("session-ref", "", "session ref")
	if err := fs.Parse(args); err != nil {
		return err
	}
	summary, hasSummary, err := analyzer.ExtractSummary(*sessionRef)
	if err != nil {
		return err
	}
	return WriteJSON(stdout, ExtractSummaryResponse{Summary: summary, HasSummary: hasSummary})
}

func HandleCalculateTokens(args []string, stdin io.Reader, stdout io.Writer, calculator tokenCalculator) error {
	fs := flag.NewFlagSet("calculate-tokens", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	offset := fs.Int("offset", 0, "offset")
	if err := fs.Parse(args); err != nil {
		return err
	}
	data, err := io.ReadAll(stdin)
	if err != nil {
		return err
	}
	usage, err := calculator.CalculateTokens(data, *offset)
	if err != nil {
		return err
	}
	return WriteJSON(stdout, usage)
}

func DefaultSessionDir(repoPath string) string {
	return filepath.Join(repoPath, ".entire", "tmp")
}

func ResolveSessionFile(sessionDir, sessionID string) string {
	return filepath.Join(sessionDir, sessionID+".json")
}
