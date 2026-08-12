package main

import (
	"fmt"
	"os"

	"github.com/entireio/external-agents/agents/entire-agent-hermes/internal/hermes"
	"github.com/entireio/external-agents/agents/entire-agent-hermes/internal/protocol"
)

func main() {
	if err := protocol.Run(hermes.New(), os.Args[1:], os.Stdin, os.Stdout); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
