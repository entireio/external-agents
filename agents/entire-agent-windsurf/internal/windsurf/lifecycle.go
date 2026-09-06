package windsurf

import "github.com/entireio/external-agents/agents/entire-agent-windsurf/internal/protocol"

// LifecycleEvent is the raw-event contract between Windsurf hook receiving and
// the existing Entire protocol adapter.
type LifecycleEvent struct {
	Name         string
	TrajectoryID string
	ExecutionID  string
	Timestamp    string
	SessionRef   string
	Prompt       string
	Metadata     map[string]string
}

// NormalizeEvent creates the protocol event after the lifecycle owner selects
// the Entire event type. trajectory_id is always used as the stable session ID.
func NormalizeEvent(eventType int, input LifecycleEvent) *protocol.EventJSON {
	if input.TrajectoryID == "" {
		return nil
	}
	metadata := make(map[string]string, len(input.Metadata)+2)
	for key, value := range input.Metadata {
		metadata[key] = value
	}
	if input.ExecutionID != "" {
		metadata["execution_id"] = input.ExecutionID
	}
	if input.Name != "" {
		metadata["windsurf_hook"] = input.Name
	}
	return &protocol.EventJSON{
		Type: eventType, SessionID: input.TrajectoryID, SessionRef: input.SessionRef,
		Prompt: input.Prompt, Timestamp: input.Timestamp, Metadata: metadata,
	}
}
