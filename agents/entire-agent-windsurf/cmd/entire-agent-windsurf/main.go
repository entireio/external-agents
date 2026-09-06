package main

import (
	"fmt"
	"io"
	"os"

	"github.com/entireio/external-agents/agents/entire-agent-windsurf/internal/protocol"
	"github.com/entireio/external-agents/agents/entire-agent-windsurf/internal/windsurf"
)

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, in io.Reader, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: entire-agent-windsurf <subcommand> [args]")
	}
	agent := windsurf.New()
	switch args[0] {
	case "info":
		return protocol.WriteJSON(out, agent.Info())
	case "detect":
		return protocol.WriteJSON(out, agent.Detect())
	case "get-session-id":
		return protocol.HandleGetSessionID(in, out, agent)
	case "get-session-dir":
		return protocol.HandleGetSessionDir(args[1:], out, agent)
	case "resolve-session-file":
		return protocol.HandleResolveSessionFile(args[1:], out, agent)
	case "read-session":
		return protocol.HandleReadSession(in, out, agent)
	case "write-session":
		return protocol.HandleWriteSession(in, agent)
	case "read-transcript":
		return protocol.HandleReadTranscript(args[1:], out, agent)
	case "chunk-transcript":
		return protocol.HandleChunkTranscript(args[1:], in, out, agent)
	case "reassemble-transcript":
		return protocol.HandleReassembleTranscript(in, out, agent)
	case "format-resume-command":
		return protocol.HandleFormatResumeCommand(args[1:], out, agent)
	case "parse-hook":
		return protocol.HandleParseHook(args[1:], in, out, agent)
	case "install-hooks":
		return protocol.HandleInstallHooks(args[1:], out, agent)
	case "uninstall-hooks":
		return agent.UninstallHooks()
	case "are-hooks-installed":
		return protocol.WriteJSON(out, protocol.AreHooksInstalledResponse{Installed: agent.AreHooksInstalled()})
	default:
		return fmt.Errorf("unknown subcommand: %s", args[0])
	}
}
