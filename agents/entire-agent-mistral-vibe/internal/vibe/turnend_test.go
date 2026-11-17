package vibe

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"testing"
	"time"
)

func writeExecutable(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestAwaitTurnEndReturnsAtOnceWithoutATurnEnd(t *testing.T) {
	isolate(t)
	ref := transcriptPath(testSessionID)
	start := time.Now()
	awaitTurnEnd(ref)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("waited %v with no turn end in flight", elapsed)
	}
}

func TestAwaitTurnEndIgnoresFinishedAndGarbageMarkers(t *testing.T) {
	isolate(t)
	ref := transcriptPath(testSessionID)
	if err := ensureTranscriptFile(ref); err != nil {
		t.Fatal(err)
	}

	finished := exec.Command(os.Args[0], "-test.run=^$")
	if err := finished.Run(); err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{strconv.Itoa(finished.Process.Pid), "not a pid", "0", "1", ""} {
		if err := os.WriteFile(turnEndPath(ref), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		awaitTurnEnd(ref)
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Errorf("marker %q: waited %v", content, elapsed)
		}
		if _, err := os.Stat(turnEndPath(ref)); !os.IsNotExist(err) {
			t.Errorf("marker %q was not removed", content)
		}
	}
}

func TestAwaitTurnEndWaitsForTheTurnEndProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sleep")
	}
	isolate(t)
	ref := transcriptPath(testSessionID)
	if err := ensureTranscriptFile(ref); err != nil {
		t.Fatal(err)
	}

	// Stands in for the `entire hooks` process still recording the turn.
	turnEnd := exec.Command("sleep", "0.4")
	if err := turnEnd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan time.Time, 1)
	go func() {
		_ = turnEnd.Wait()
		exited <- time.Now()
	}()
	if err := os.WriteFile(turnEndPath(ref), []byte(strconv.Itoa(turnEnd.Process.Pid)), 0o600); err != nil {
		t.Fatal(err)
	}

	awaitTurnEnd(ref)
	returned := time.Now()

	select {
	case at := <-exited:
		if returned.Before(at) {
			t.Error("session end overtook the turn end")
		}
	default:
		t.Error("returned while the turn-end process was still running")
	}
}

func TestMarkTurnEndRecordsTheCallingProcess(t *testing.T) {
	isolate(t)
	ref := transcriptPath(testSessionID)
	markTurnEnd(ref)
	data, err := os.ReadFile(turnEndPath(ref))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != strconv.Itoa(os.Getppid()) {
		t.Errorf("marker = %q, want parent pid %d", data, os.Getppid())
	}
}
