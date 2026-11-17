package vibe

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Vibe runs the turn-completed notification hook and the session-end hook in
// the background and, when a headless run exits, starts them within
// milliseconds of each other. Entire's session-end handling must not overtake
// its turn-end handling: if it does, the session is marked ended before the
// turn's checkpoint is recorded and the next commit links nothing.
//
// Both hooks pass through this binary first, so the turn-end hook leaves the
// PID of the `entire` process that is handling it, and the session-end hook
// waits for that process to exit before it lets Entire continue.

const (
	turnEndExt = ".turnend"
	// turnEndWait bounds the wait. Vibe itself stops hooks five seconds after
	// quitting, so this only matters when something has gone wrong.
	turnEndWait = 30 * time.Second
	turnEndPoll = 25 * time.Millisecond
)

func turnEndPath(sessionRef string) string {
	return strings.TrimSuffix(sessionRef, transcriptExt) + turnEndExt
}

// markTurnEnd records the process that is handling this turn end: our parent,
// the `entire hooks` process.
func markTurnEnd(sessionRef string) {
	if err := os.MkdirAll(filepath.Dir(sessionRef), 0o700); err != nil {
		return
	}
	_ = atomicWriteFile(turnEndPath(sessionRef), []byte(strconv.Itoa(os.Getppid())), 0o600)
}

// awaitTurnEnd blocks while a turn-end hook for this session is still running.
func awaitTurnEnd(sessionRef string) {
	path := turnEndPath(sessionRef)
	defer os.Remove(path)
	deadline := time.Now().Add(turnEndWait)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err != nil {
			return
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil || pid <= 1 || pid == os.Getppid() || !processAlive(pid) {
			return
		}
		time.Sleep(turnEndPoll)
	}
}
