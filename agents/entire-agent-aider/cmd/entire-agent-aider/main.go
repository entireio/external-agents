package main

import (
	"fmt"
	"os"

	"github.com/entireio/external-agents/agents/entire-agent-aider/internal/aider"
	"github.com/entireio/external-agents/agents/entire-agent-aider/internal/protocol"
)

func main() {
	agent := aider.New()
	if err := protocol.Run(os.Args[1:], os.Stdin, os.Stdout, agent, agent.Info()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
