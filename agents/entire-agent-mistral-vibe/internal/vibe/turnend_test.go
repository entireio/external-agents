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

	// The process is reaped a moment after it dies, so compare with a margin
	// rather than requiring the reaper to have run first.
	if at := <-exited; returned.Before(at.Add(-50 * time.Millisecond)) {
		t.Errorf("session end returned %v before the turn end finished", at.Sub(returned))
	}
}

// shortWaits makes the session-end waits quick enough to test.
func shortWaits(t *testing.T, grace, total time.Duration) {
	t.Helper()
	oldGrace, oldTotal := turnOpenGrace, turnEndWait
	turnOpenGrace, turnEndWait = grace, total
	t.Cleanup(func() { turnOpenGrace, turnEndWait = oldGrace, oldTotal })
}

// The turn-end hook can start after the session-end hook. Session end must
// wait for it to announce itself, then for it to finish.
func TestAwaitTurnEndWaitsForATurnEndThatStartsLate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sleep")
	}
	isolate(t)
	shortWaits(t, 2*time.Second, 5*time.Second)
	ref := transcriptPath(testSessionID)
	if err := ensureTranscriptFile(ref); err != nil {
		t.Fatal(err)
	}
	markTurnOpen(ref)

	finished := make(chan time.Time, 1)
	go func() {
		// The turn-end hook only gets going after session end has started waiting.
		time.Sleep(150 * time.Millisecond)
		turnEnd := exec.Command("sleep", "0.3")
		if err := turnEnd.Start(); err != nil {
			finished <- time.Time{}
			return
		}
		_ = os.WriteFile(turnEndPath(ref), []byte(strconv.Itoa(turnEnd.Process.Pid)), 0o600)
		_ = turnEnd.Wait()
		finished <- time.Now()
	}()

	awaitTurnEnd(ref)
	returned := time.Now()

	at := <-finished
	if at.IsZero() {
		t.Fatal("could not start the stand-in process")
	}
	if returned.Before(at) {
		t.Error("session end overtook a turn end that started late")
	}
}

// An interrupted turn never gets a turn end; session end must not hang on it.
func TestAwaitTurnEndGivesUpOnATurnThatNeverEnds(t *testing.T) {
	isolate(t)
	shortWaits(t, 100*time.Millisecond, time.Second)
	ref := transcriptPath(testSessionID)
	if err := ensureTranscriptFile(ref); err != nil {
		t.Fatal(err)
	}
	markTurnOpen(ref)

	start := time.Now()
	awaitTurnEnd(ref)
	elapsed := time.Since(start)
	if elapsed < 100*time.Millisecond || elapsed > 700*time.Millisecond {
		t.Errorf("waited %v, want about the 100ms grace period", elapsed)
	}
	if _, err := os.Stat(turnEndPath(ref)); !os.IsNotExist(err) {
		t.Error("marker was not removed")
	}
}

// A stale marker naming a live, unrelated process must not block for long.
func TestAwaitTurnEndIsBounded(t *testing.T) {
	isolate(t)
	shortWaits(t, 50*time.Millisecond, 200*time.Millisecond)
	ref := transcriptPath(testSessionID)
	if err := ensureTranscriptFile(ref); err != nil {
		t.Fatal(err)
	}
	// This test process is alive and is not our parent.
	if err := os.WriteFile(turnEndPath(ref), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	awaitTurnEnd(ref)
	if elapsed := time.Since(start); elapsed < 200*time.Millisecond || elapsed > time.Second {
		t.Errorf("waited %v, want about the 200ms bound", elapsed)
	}
}

func TestParseHookMarksTurnOpenThenHandled(t *testing.T) {
	isolate(t)
	agent := &Agent{Fetcher: &fakeFetcher{snapshot: loadSnapshot(t)}}
	event, err := agent.ParseHook(HookNamePreAgent, hookPayloadFixture(t, "pre_agent"))
	if err != nil {
		t.Fatal(err)
	}
	marker := func() string {
		data, _ := os.ReadFile(turnEndPath(event.SessionRef))
		return string(data)
	}
	if marker() != turnOpen {
		t.Errorf("after turn start the marker is %q, want %q", marker(), turnOpen)
	}
	if _, err := agent.ParseHook(HookNameNotification, hookPayloadFixture(t, "notification")); err != nil {
		t.Fatal(err)
	}
	if marker() != strconv.Itoa(os.Getppid()) {
		t.Errorf("after turn end the marker is %q, want the handling process", marker())
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
