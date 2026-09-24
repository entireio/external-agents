package protocol

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
)

// Agent keeps wire handling separate from native Aider storage and parsing.
type Agent interface {
	Detect() bool
	GetSessionID(*HookInput) (string, error)
	GetSessionDir(string) (string, error)
	ResolveSessionFile(string, string) string
	ReadSession(*HookInput) (Session, error)
	WriteSession(Session) error
	ReadTranscript(string) ([]byte, error)
	ChunkTranscript([]byte, int) ([][]byte, error)
	ReassembleTranscript([][]byte) ([]byte, error)
	FormatResumeCommand(string) string
	GetTranscriptPosition(string) (int, error)
	ExtractModifiedFiles(string, int) ([]string, int, error)
	CommitModifiedFiles(string) ([]string, error)
	ExtractPrompts(string, int) ([]string, error)
	ExtractSummary(string) (string, bool, error)
	CalculateTokens([]byte, int) (Tokens, error)
}

func Run(args []string, stdin io.Reader, stdout io.Writer, agent Agent, info any) error {
	if len(args) == 0 {
		return errors.New("usage: entire-agent-aider <subcommand> [flags]")
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var repo, dir, id, ref, path, commit, revisionRange string
	var offset, maxSize int
	switch args[0] {
	case "get-session-dir":
		fs.StringVar(&repo, "repo-path", "", "repository")
	case "resolve-session-file":
		fs.StringVar(&dir, "session-dir", "", "session directory")
		fs.StringVar(&id, "session-id", "", "session ID")
	case "read-transcript", "extract-summary":
		fs.StringVar(&ref, "session-ref", "", "transcript")
	case "extract-prompts":
		fs.StringVar(&ref, "session-ref", "", "transcript")
		fs.IntVar(&offset, "offset", 0, "byte offset")
	case "format-resume-command":
		fs.StringVar(&id, "session-id", "", "session ID")
	case "get-transcript-position":
		fs.StringVar(&path, "path", "", "transcript")
	case "extract-modified-files":
		fs.StringVar(&path, "path", "", "transcript or revision")
		fs.StringVar(&commit, "commit", "", "commit")
		fs.StringVar(&revisionRange, "range", "", "two-dot range")
		fs.IntVar(&offset, "offset", 0, "byte offset")
	case "calculate-tokens":
		fs.IntVar(&offset, "offset", 0, "byte offset")
	case "chunk-transcript":
		fs.IntVar(&maxSize, "max-size", 0, "maximum raw chunk size")
	case "parse-hook":
		fs.String("hook", "", "ignored")
	case "install-hooks":
		fs.Bool("force", false, "ignored")
		fs.Bool("local-dev", false, "ignored")
	case "info", "detect", "get-session-id", "read-session", "write-session", "reassemble-transcript", "uninstall-hooks", "are-hooks-installed":
	default:
		return fmt.Errorf("unknown subcommand: %s", args[0])
	}
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if offset < 0 {
		return errors.New("offset must be non-negative")
	}
	write := func(v any) error { return json.NewEncoder(stdout).Encode(v) }
	raw := func(data []byte, err error) error {
		if err != nil {
			return err
		}
		_, err = stdout.Write(data)
		return err
	}
	switch args[0] {
	case "info":
		return write(info)
	case "detect":
		return write(map[string]any{"present": agent.Detect()})
	case "get-session-id", "read-session":
		var input HookInput
		if err := json.NewDecoder(stdin).Decode(&input); err != nil {
			return err
		}
		if args[0] == "read-session" {
			session, err := agent.ReadSession(&input)
			if err != nil {
				return err
			}
			return write(session)
		}
		id, err := agent.GetSessionID(&input)
		if err != nil {
			return err
		}
		return write(map[string]string{"session_id": id})
	case "get-session-dir":
		dir, err := agent.GetSessionDir(repo)
		if err != nil {
			return err
		}
		return write(map[string]string{"session_dir": dir})
	case "resolve-session-file":
		return write(map[string]string{"session_file": agent.ResolveSessionFile(dir, id)})
	case "write-session":
		var session Session
		if err := json.NewDecoder(stdin).Decode(&session); err != nil {
			return err
		}
		return agent.WriteSession(session)
	case "read-transcript":
		return raw(agent.ReadTranscript(ref))
	case "chunk-transcript":
		data, err := io.ReadAll(stdin)
		if err != nil {
			return err
		}
		chunks, err := agent.ChunkTranscript(data, maxSize)
		if err != nil {
			return err
		}
		return write(Chunks{Chunks: chunks})
	case "reassemble-transcript":
		var chunks Chunks
		if err := json.NewDecoder(stdin).Decode(&chunks); err != nil {
			return err
		}
		return raw(agent.ReassembleTranscript(chunks.Chunks))
	case "format-resume-command":
		return write(map[string]string{"command": agent.FormatResumeCommand(id)})
	case "parse-hook":
		return write(nil)
	case "install-hooks":
		return write(map[string]int{"hooks_installed": 0})
	case "uninstall-hooks":
		return nil
	case "are-hooks-installed":
		return write(map[string]bool{"installed": false})
	case "get-transcript-position":
		n, err := agent.GetTranscriptPosition(path)
		if err != nil {
			return err
		}
		return write(map[string]int{"position": n})
	case "extract-modified-files":
		var files []string
		var position int
		var err error
		if commit != "" && revisionRange != "" {
			return errors.New("specify only one of --commit and --range")
		}
		if commit != "" {
			files, err = agent.CommitModifiedFiles(commit)
		} else if revisionRange != "" {
			files, err = agent.CommitModifiedFiles(revisionRange)
		} else {
			files, position, err = agent.ExtractModifiedFiles(path, offset)
		}
		if err != nil {
			return err
		}
		return write(map[string]any{"files": files, "current_position": position})
	case "extract-prompts":
		prompts, err := agent.ExtractPrompts(ref, offset)
		if err != nil {
			return err
		}
		return write(map[string]any{"prompts": prompts})
	case "extract-summary":
		summary, found, err := agent.ExtractSummary(ref)
		if err != nil {
			return err
		}
		return write(map[string]any{"summary": summary, "has_summary": found})
	case "calculate-tokens":
		data, err := io.ReadAll(stdin)
		if err != nil {
			return err
		}
		tokens, err := agent.CalculateTokens(data, offset)
		if err != nil {
			return err
		}
		return write(tokens)
	}
	return nil
}
