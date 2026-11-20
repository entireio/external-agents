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
// Every hook passes through this binary first, which keeps a small marker per
// session:
//
//   - turn start writes "open": a turn end is still owed;
//   - turn end replaces it with the PID of the `entire` process handling it;
//   - session end waits while the marker is "open" (briefly: the turn-end hook
//     may not have started yet) or names a live process, then removes it.
//
// This narrows the race rather than closing it: a turn-end hook that starts
// later than the "open" grace period is still overtaken. An interrupted turn
// never gets a turn end, so its session end waits out the grace period.

const (
	turnEndExt  = ".turnend"
	turnOpen    = "open"
	turnEndPoll = 25 * time.Millisecond
)

// The waits are variables so tests can shorten them. Vibe stops hooks five
// seconds after it quits, so together with the session-end fetch they stay
// under that.
var (
	// turnOpenGrace is how long session end waits for a turn-end hook that has
	// not announced itself yet.
	turnOpenGrace = 1500 * time.Millisecond
	// turnEndWait bounds the whole wait.
	turnEndWait = 3 * time.Second
)

func turnEndPath(sessionRef string) string {
	return strings.TrimSuffix(sessionRef, transcriptExt) + turnEndExt
}

func writeTurnMarker(sessionRef, value string) {
	if err := os.MkdirAll(filepath.Dir(sessionRef), 0o700); err != nil {
		return
	}
	_ = atomicWriteFile(turnEndPath(sessionRef), []byte(value), 0o600)
}

// markTurnOpen records that a turn has started and its end is still owed.
func markTurnOpen(sessionRef string) {
	writeTurnMarker(sessionRef, turnOpen)
}

// markTurnEnd records the process that is handling this turn end: our parent,
// the `entire hooks` process.
func markTurnEnd(sessionRef string) {
	writeTurnMarker(sessionRef, strconv.Itoa(os.Getppid()))
}

// awaitTurnEnd blocks while a turn-end hook for this session is pending or
// still running, within the bounds above.
func awaitTurnEnd(sessionRef string) {
	path := turnEndPath(sessionRef)
	defer os.Remove(path)
	start := time.Now()
	for time.Since(start) < turnEndWait {
		data, err := os.ReadFile(path)
		if err != nil {
			return
		}
		marker := strings.TrimSpace(string(data))
		if marker == turnOpen {
			if time.Since(start) >= turnOpenGrace {
				return
			}
		} else {
			pid, err := strconv.Atoi(marker)
			if err != nil || pid <= 1 || pid == os.Getppid() || !processAlive(pid) {
				return
			}
		}
		time.Sleep(turnEndPoll)
	}
}
