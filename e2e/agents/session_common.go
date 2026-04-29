package agents

import (
	"strings"
	"time"
)

const (
	settleTime   = 2 * time.Second
	pollInterval = 500 * time.Millisecond
)

// stableContent returns the content with the last few lines stripped,
// so that TUI status bar updates don't prevent the settle timer.
func stableContent(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) > 3 {
		lines = lines[:len(lines)-3]
	}
	return strings.Join(lines, "\n")
}
