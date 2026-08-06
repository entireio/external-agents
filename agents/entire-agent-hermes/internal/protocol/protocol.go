package protocol

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
)

type Agent interface {
	Info() InfoResponse
	Detect() DetectResponse
	GetSessionID(*HookInputJSON) string
	GetSessionDir(string) (string, error)
	ResolveSessionFile(string, string) string
	ReadSession(*HookInputJSON) (AgentSessionJSON, error)
	WriteSession(AgentSessionJSON) error
	ReadTranscript(string) ([]byte, error)
	ChunkTranscript([]byte, int) ([][]byte, error)
	ReassembleTranscript([][]byte) ([]byte, error)
	CompactTranscript(string) (CompactTranscriptResponse, error)
	FormatResumeCommand(string) string
	ParseHook(string, []byte) (*EventJSON, error)
	InstallHooks(bool, bool) (int, error)
	UninstallHooks() error
	AreHooksInstalled() bool
	GetTranscriptPosition(string) (int, error)
	ExtractModifiedFiles(string, int) ([]string, int, error)
	ExtractPrompts(string, int) ([]string, error)
	ExtractSummary(string) (string, bool, error)
}

func WriteJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func RepoRoot() string {
	if root := os.Getenv("ENTIRE_REPO_ROOT"); root != "" {
		return root
	}
	root, _ := os.Getwd()
	return root
}

func Run(agent Agent, args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: entire-agent-hermes <subcommand> [args]")
	}
	switch args[0] {
	case "info":
		return WriteJSON(stdout, agent.Info())
	case "detect":
		return WriteJSON(stdout, agent.Detect())
	case "get-session-id":
		var in HookInputJSON
		if err := json.NewDecoder(stdin).Decode(&in); err != nil {
			return err
		}
		return WriteJSON(stdout, SessionIDResponse{SessionID: agent.GetSessionID(&in)})
	case "get-session-dir":
		fs := newFlags(args[0])
		repo := fs.String("repo-path", "", "repo path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		dir, err := agent.GetSessionDir(*repo)
		if err != nil {
			return err
		}
		return WriteJSON(stdout, SessionDirResponse{SessionDir: dir})
	case "resolve-session-file":
		fs := newFlags(args[0])
		dir := fs.String("session-dir", "", "session dir")
		id := fs.String("session-id", "", "session id")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return WriteJSON(stdout, SessionFileResponse{SessionFile: agent.ResolveSessionFile(*dir, *id)})
	case "read-session":
		var in HookInputJSON
		if err := json.NewDecoder(stdin).Decode(&in); err != nil {
			return err
		}
		session, err := agent.ReadSession(&in)
		if err != nil {
			return err
		}
		return WriteJSON(stdout, session)
	case "write-session":
		var in AgentSessionJSON
		if err := json.NewDecoder(stdin).Decode(&in); err != nil {
			return err
		}
		return agent.WriteSession(in)
	case "read-transcript":
		fs := newFlags(args[0])
		ref := fs.String("session-ref", "", "session ref")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		data, err := agent.ReadTranscript(*ref)
		if err != nil {
			return err
		}
		_, err = stdout.Write(data)
		return err
	case "chunk-transcript":
		fs := newFlags(args[0])
		maxSize := fs.Int("max-size", 0, "max size")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		data, err := io.ReadAll(stdin)
		if err != nil {
			return err
		}
		chunks, err := agent.ChunkTranscript(data, *maxSize)
		if err != nil {
			return err
		}
		return WriteJSON(stdout, ChunkResponse{Chunks: chunks})
	case "reassemble-transcript":
		var in ChunkResponse
		if err := json.NewDecoder(stdin).Decode(&in); err != nil {
			return err
		}
		data, err := agent.ReassembleTranscript(in.Chunks)
		if err != nil {
			return err
		}
		_, err = stdout.Write(data)
		return err
	case "compact-transcript":
		fs := newFlags(args[0])
		ref := fs.String("session-ref", "", "session ref")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		out, err := agent.CompactTranscript(*ref)
		if err != nil {
			return err
		}
		return WriteJSON(stdout, out)
	case "format-resume-command":
		fs := newFlags(args[0])
		id := fs.String("session-id", "", "session id")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return WriteJSON(stdout, ResumeCommandResponse{Command: agent.FormatResumeCommand(*id)})
	case "parse-hook":
		fs := newFlags(args[0])
		hook := fs.String("hook", "", "hook name")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		data, err := io.ReadAll(stdin)
		if err != nil {
			return err
		}
		event, err := agent.ParseHook(*hook, data)
		if err != nil {
			return err
		}
		if event == nil {
			_, err = io.WriteString(stdout, "null\n")
			return err
		}
		return WriteJSON(stdout, event)
	case "install-hooks":
		fs := newFlags(args[0])
		local := fs.Bool("local-dev", false, "local dev")
		force := fs.Bool("force", false, "force")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		count, err := agent.InstallHooks(*local, *force)
		if err != nil {
			return err
		}
		return WriteJSON(stdout, HooksInstalledCountResponse{HooksInstalled: count})
	case "uninstall-hooks":
		return agent.UninstallHooks()
	case "are-hooks-installed":
		return WriteJSON(stdout, AreHooksInstalledResponse{Installed: agent.AreHooksInstalled()})
	case "get-transcript-position":
		fs := newFlags(args[0])
		path := fs.String("path", "", "path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		position, err := agent.GetTranscriptPosition(*path)
		if err != nil {
			return err
		}
		return WriteJSON(stdout, TranscriptPositionResponse{Position: position})
	case "extract-modified-files":
		fs := newFlags(args[0])
		path := fs.String("path", "", "path")
		offset := fs.Int("offset", 0, "offset")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		files, position, err := agent.ExtractModifiedFiles(*path, *offset)
		if err != nil {
			return err
		}
		return WriteJSON(stdout, ExtractFilesResponse{Files: files, CurrentPosition: position})
	case "extract-prompts":
		fs := newFlags(args[0])
		ref := fs.String("session-ref", "", "session ref")
		offset := fs.Int("offset", 0, "offset")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		prompts, err := agent.ExtractPrompts(*ref, *offset)
		if err != nil {
			return err
		}
		return WriteJSON(stdout, ExtractPromptsResponse{Prompts: prompts})
	case "extract-summary":
		fs := newFlags(args[0])
		ref := fs.String("session-ref", "", "session ref")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		summary, ok, err := agent.ExtractSummary(*ref)
		if err != nil {
			return err
		}
		return WriteJSON(stdout, ExtractSummaryResponse{Summary: summary, HasSummary: ok})
	default:
		return fmt.Errorf("unknown subcommand: %s", args[0])
	}
}

func newFlags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

func ParseOffset(raw string) (int, error) {
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("invalid offset %q", raw)
	}
	return n, nil
}
