//go:build e2e

package kiro

import (
	"testing"

	e2e "github.com/entireio/external-agents/e2e"
)

// NewKiroTestEnv creates a test environment with .kiro/ and .entire/tmp/ directories.
func NewKiroTestEnv(t *testing.T) *e2e.TestEnv {
	t.Helper()
	te := e2e.NewTestEnvWithBinary(t, kiroBinary)
	te.MkdirAll(".kiro")
	te.MkdirAll(".entire/tmp")
	return te
}

// NewKiroGitEnv creates a Kiro test environment with git init.
func NewKiroGitEnv(t *testing.T) *e2e.TestEnv {
	t.Helper()
	te := NewKiroTestEnv(t)
	te.GitInit()
	return te
}
