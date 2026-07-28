//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entireio/external-agents/e2e/entire"
	"github.com/entireio/external-agents/e2e/testutil"
	"github.com/stretchr/testify/require"
)

func TestOMP_InProcessNewSessionLifecycle(t *testing.T) {
	testutil.ForEachAgent(t, 4*time.Minute, func(t *testing.T, s *testutil.RepoState, ctx context.Context) {
		if s.Agent.Name() != "omp" {
			t.Skip("omp-specific lifecycle test")
		}

		session := s.StartSession(t, ctx)
		require.NotNil(t, session, "omp must support interactive sessions")

		const firstPrompt = "Create omp-first.txt containing exactly 'first session'. Do not ask for confirmation."
		s.Send(t, session, firstPrompt)
		s.WaitFor(t, session, s.Agent.PromptPattern(), 60*time.Second)
		testutil.WaitForFileExists(t, s.Dir, "omp-first.txt", 10*time.Second)
		testutil.WaitForSessionIdle(t, s.Dir, 10*time.Second)

		firstEvents := ompLifecycleEvents(t, s.Dir)
		require.Len(t, firstEvents, 3)
		firstSessionID := firstEvents[0].SessionID
		require.NotEmpty(t, firstSessionID)
		require.Equal(t, []ompLifecycleEvent{
			{Event: "SessionStart", SessionID: firstSessionID, SessionRef: firstEvents[0].SessionRef},
			{Event: "TurnStart", SessionID: firstSessionID, SessionRef: firstEvents[1].SessionRef},
			{Event: "TurnEnd", SessionID: firstSessionID, SessionRef: firstEvents[2].SessionRef},
		}, firstEvents)
		require.NotEmpty(t, firstEvents[0].SessionRef)

		firstCheckpoint := ompCheckpointForSession(t, entire.RewindList(t, s.Dir), firstSessionID)

		commandSession, ok := session.(interface {
			SendAndWait(string, string, time.Duration) (string, error)
		})
		require.True(t, ok, "omp interactive session must expose SendAndWait")
		pane, err := commandSession.SendAndWait("/new", s.Agent.PromptPattern(), 30*time.Second)
		require.NoError(t, err, "wait for omp /new prompt\npane:\n%s", pane)

		switchEvents := ompLifecycleEvents(t, s.Dir)
		require.Len(t, switchEvents, 4, "/new must emit exactly one new SessionStart")
		secondSessionID := switchEvents[3].SessionID
		require.Equal(t, "SessionStart", switchEvents[3].Event)
		require.NotEmpty(t, secondSessionID)
		require.NotEqual(t, firstSessionID, secondSessionID)
		require.NotEmpty(t, switchEvents[3].SessionRef)
		require.NotEqual(t, firstEvents[0].SessionRef, switchEvents[3].SessionRef)

		const secondPrompt = "Create omp-second.txt containing exactly 'second session'. Do not ask for confirmation."
		s.Send(t, session, secondPrompt)
		s.WaitFor(t, session, s.Agent.PromptPattern(), 60*time.Second)
		testutil.WaitForFileExists(t, s.Dir, "omp-second.txt", 10*time.Second)
		testutil.WaitForSessionIdle(t, s.Dir, 10*time.Second)

		events := ompLifecycleEvents(t, s.Dir)
		require.Len(t, events, 6)
		require.Equal(t, []ompLifecycleEvent{
			firstEvents[0],
			firstEvents[1],
			firstEvents[2],
			switchEvents[3],
			{Event: "TurnStart", SessionID: secondSessionID, SessionRef: events[4].SessionRef},
			{Event: "TurnEnd", SessionID: secondSessionID, SessionRef: events[5].SessionRef},
		}, events, "the old turn must end before /new starts the new lifecycle")

		points := entire.RewindList(t, s.Dir)
		require.Contains(t, points, firstCheckpoint, "the old session checkpoint must remain addressable")
		secondCheckpoint := ompCheckpointForSession(t, points, secondSessionID)
		require.NotEqual(t, firstCheckpoint.ID, secondCheckpoint.ID, "the new session needs an independent checkpoint")
	})
}

type ompLifecycleEvent struct {
	Event      string `json:"event"`
	SessionID  string `json:"session_id"`
	SessionRef string `json:"session_ref"`
}

func ompLifecycleEvents(t *testing.T, repoDir string) []ompLifecycleEvent {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(repoDir, ".entire", "logs", "entire.log"))
	require.NoError(t, err)

	var events []ompLifecycleEvent
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var record struct {
			ompLifecycleEvent
			Component string `json:"component"`
		}
		require.NoError(t, json.Unmarshal([]byte(line), &record), "invalid Entire JSON log line")
		if record.Component == "lifecycle" && record.Event != "" {
			events = append(events, record.ompLifecycleEvent)
		}
	}
	return events
}

func ompCheckpointForSession(t *testing.T, points []entire.RewindPoint, sessionID string) entire.RewindPoint {
	t.Helper()

	for _, point := range points {
		if point.SessionID == sessionID && !point.IsLogsOnly {
			return point
		}
	}
	t.Fatalf("no live checkpoint for omp session %s; rewind points: %+v", sessionID, points)
	return entire.RewindPoint{}
}
