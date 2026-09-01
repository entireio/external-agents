package agents

import (
	"context"
	"errors"
	"os"
	"strings"
)

// ZCode is an Electron desktop app with no headless CLI, so prompt-driven
// lifecycle automation is not possible. The adapter is registered only when
// explicitly opted in (ZCODE_E2E=1 and E2E_AGENT=zcode) and every prompt
// attempt reports the limitation instead of failing obscurely.
func init() {
	if os.Getenv("ZCODE_E2E") != "1" || os.Getenv("E2E_AGENT") != "zcode" {
		return
	}
	Register(&ZCode{})
	RegisterGate("zcode", 1)
}

var errHeadlessUnsupported = errors.New(
	"zcode is a desktop app with no headless mode; drive a ZCode session manually " +
		"and use agents/entire-agent-zcode/scripts/verify-zcode.sh to capture hook payloads")

type ZCode struct{}

func (z *ZCode) Name() string               { return "zcode" }
func (z *ZCode) Binary() string             { return "zcode" }
func (z *ZCode) EntireAgent() string        { return "zcode" }
func (z *ZCode) PromptPattern() string      { return `>` }
func (z *ZCode) TimeoutMultiplier() float64 { return 2.0 }
func (z *ZCode) IsExternalAgent() bool      { return true }
func (z *ZCode) Bootstrap() error           { return nil }

func (z *ZCode) IsTransientError(out Output, _ error) bool {
	combined := strings.ToLower(out.Stdout + out.Stderr)
	for _, pattern := range []string{"429", "rate limit", "overloaded", "503", "timeout", "econnreset"} {
		if strings.Contains(combined, pattern) {
			return true
		}
	}
	return false
}

func (z *ZCode) RunPrompt(_ context.Context, _ string, _ string, _ ...Option) (Output, error) {
	return Output{Command: "zcode", ExitCode: 1, Stderr: errHeadlessUnsupported.Error()}, errHeadlessUnsupported
}

func (z *ZCode) StartSession(_ context.Context, _ string) (Session, error) {
	return nil, errHeadlessUnsupported
}
