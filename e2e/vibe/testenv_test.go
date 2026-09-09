//go:build e2e

package vibe

import (
	"testing"

	e2e "github.com/entireio/external-agents/e2e"
)

// NewVibeTestEnv creates a test environment with .vibe/ and .entire/tmp/ directories.
func NewVibeTestEnv(t *testing.T) *e2e.TestEnv {
	t.Helper()
	te := e2e.NewTestEnvWithBinary(t, vibeBinary)
	te.MkdirAll(".vibe")
	te.MkdirAll(".entire/tmp")
	return te
}

// NewVibeGitEnv creates a Vibe test environment with git init.
func NewVibeGitEnv(t *testing.T) *e2e.TestEnv {
	t.Helper()
	te := NewVibeTestEnv(t)
	te.GitInit()
	return te
}
