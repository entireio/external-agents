package main

import (
	"fmt"
	"os"

	"github.com/entireio/external-agents/agents/entire-agent-pi/internal/pi"
	"github.com/entireio/external-agents/agents/entire-agent-pi/internal/protocol"
)

func main() {
	agent := pi.New()

	if len(os.Args) < 2 {
		fatalf("usage: entire-agent-pi <subcommand> [args]")
	}

	var err error

	switch os.Args[1] {
	case "info":
		err = protocol.WriteJSON(os.Stdout, agent.Info())
	case "detect":
		err = protocol.WriteJSON(os.Stdout, agent.Detect())
	case "get-session-id":
		err = protocol.HandleGetSessionID(os.Stdin, os.Stdout, agent)
	case "get-session-dir":
		err = protocol.HandleGetSessionDir(os.Args[2:], os.Stdout, agent)
	case "resolve-session-file":
		err = protocol.HandleResolveSessionFile(os.Args[2:], os.Stdout, agent)
	case "read-session":
		err = protocol.HandleReadSession(os.Stdin, os.Stdout, agent)
	case "write-session":
		err = protocol.HandleWriteSession(os.Stdin, agent)
	case "read-transcript":
		err = protocol.HandleReadTranscript(os.Args[2:], os.Stdout, agent)
	case "chunk-transcript":
		err = protocol.HandleChunkTranscript(os.Args[2:], os.Stdin, os.Stdout, agent)
	case "reassemble-transcript":
		err = protocol.HandleReassembleTranscript(os.Stdin, os.Stdout, agent)
	case "format-resume-command":
		err = protocol.HandleFormatResumeCommand(os.Args[2:], os.Stdout, agent)
	case "parse-hook":
		err = protocol.HandleParseHook(os.Args[2:], os.Stdin, os.Stdout, agent)
	case "install-hooks":
		err = protocol.HandleInstallHooks(os.Args[2:], os.Stdout, agent)
	case "uninstall-hooks":
		err = agent.UninstallHooks()
	case "are-hooks-installed":
		err = protocol.WriteJSON(os.Stdout, protocol.AreHooksInstalledResponse{
			Installed: agent.AreHooksInstalled(),
		})
	case "get-transcript-position":
		err = protocol.HandleGetTranscriptPosition(os.Args[2:], os.Stdout, agent)
	case "extract-modified-files":
		err = protocol.HandleExtractModifiedFiles(os.Args[2:], os.Stdout, agent)
	case "extract-prompts":
		err = protocol.HandleExtractPrompts(os.Args[2:], os.Stdout, agent)
	case "extract-summary":
		err = protocol.HandleExtractSummary(os.Args[2:], os.Stdout, agent)
	case "calculate-tokens":
		err = protocol.HandleCalculateTokens(os.Args[2:], os.Stdin, os.Stdout, agent)
	default:
		fatalf("unknown subcommand: %s", os.Args[1])
	}

	if err != nil {
		fatalf("%v", err)
	}
}

func fatalf(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
