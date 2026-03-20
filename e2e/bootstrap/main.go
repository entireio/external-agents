// Package main provides a pre-test bootstrap command that runs agent-specific
// setup (auth config, warmup) before E2E tests. Usage: go run ./e2e/bootstrap
package main

import (
	"fmt"
	"os"

	"github.com/entireio/external-agents/e2e/agents"
)

func main() {
	for _, a := range agents.All() {
		fmt.Fprintf(os.Stderr, "bootstrapping %s...\n", a.Name())
		if err := a.Bootstrap(); err != nil {
			fmt.Fprintf(os.Stderr, "bootstrap %s: %v\n", a.Name(), err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "bootstrapping %s: done\n", a.Name())
	}
}
