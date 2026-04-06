package a2a

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// Mapper converts between CPN domain types and A2A wire format types.
// All A2A type conversions are centralized here to keep the adapter boundary
// clean (PAT-001 from the spec).
type Mapper struct{}

// NewMapper creates a new Mapper instance.
func NewMapper() *Mapper {
	return &Mapper{}
}

// StateToA2A maps a CPN State to an A2A TaskState string.
// Mapping per REQ-008:
//
//	StateIdle      -> submitted
//	StateRunning   -> working
//	StateWaiting   -> input-required
//	StateCompleted -> completed
//	StateFailed    -> failed
func (m *Mapper) StateToA2A(state cpn.State) string {
	switch state {
	case cpn.StateIdle:
		return TaskStateSubmitted
	case cpn.StateRunning:
		return TaskStateWorking
	case cpn.StateWaiting:
		return TaskStateInputRequired
	case cpn.StateCompleted:
		return TaskStateCompleted
	case cpn.StateFailed:
		return TaskStateFailed
	default:
		return TaskStateSubmitted
	}
}

// CPNMessageToA2A converts a CPN Message to an A2A Message.
func (m *Mapper) CPNMessageToA2A(msg cpn.Message) Message {
	role := RoleUser
	if msg.Role == cpn.RoleAssistant || msg.Role == cpn.RoleObserver {
		role = RoleAgent
	}

	return Message{
		Role: role,
		Parts: []Part{
			{Type: "text", Text: msg.Content},
		},
	}
}

// TokenPayloadToPart converts a CPN token payload to an A2A Part using a type switch.
// Follows GUD-002: string->TextPart, []byte->RawPart, map/struct->DataPart.
func (m *Mapper) TokenPayloadToPart(payload any) Part {
	switch v := payload.(type) {
	case string:
		return Part{Type: "text", Text: v}
	case []byte:
		return Part{Type: "raw", Raw: string(v), MimeType: "application/octet-stream"}
	case map[string]any:
		return Part{Type: "data", Data: v}
	default:
		// Try JSON marshaling for struct types.
		data, err := json.Marshal(v)
		if err != nil {
			return Part{Type: "text", Text: fmt.Sprintf("%v", v)}
		}
		var dataMap map[string]any
		if err := json.Unmarshal(data, &dataMap); err != nil {
			return Part{Type: "text", Text: string(data)}
		}
		return Part{Type: "data", Data: dataMap}
	}
}

// CPNEventToA2AEvent maps a CPN Event to an A2A SSE event payload.
// Returns either a TaskStatusUpdateEvent or TaskArtifactUpdateEvent depending
// on the event type.
//
// Per REQ-009: EventStreamChunk -> TaskArtifactUpdateEvent with append=true.
// Per REQ-010: EventHITLRequested -> TaskStatusUpdateEvent with INPUT_REQUIRED.
func (m *Mapper) CPNEventToA2AEvent(taskID, contextID string, evt cpn.Event) any {
	switch evt.Type {
	case cpn.EventStreamChunk:
		chunk, ok := evt.Payload.(cpn.StreamChunk)
		if !ok {
			return nil
		}
		return &TaskArtifactUpdateEvent{
			ID:        taskID,
			ContextID: contextID,
			Artifact: Artifact{
				Name:      "response",
				Parts:     []Part{{Type: "text", Text: chunk.Content}},
				Index:     0,
				Append:    true,
				LastChunk: chunk.Done,
			},
		}

	case cpn.EventHITLRequested:
		prompt := "Human review required"
		if evt.Token != nil {
			if s, ok := evt.Token.Payload.(string); ok && s != "" {
				prompt = s
			}
		}
		return &TaskStatusUpdateEvent{
			ID:        taskID,
			ContextID: contextID,
			Status: TaskStatus{
				State: TaskStateInputRequired,
				Message: &Message{
					Role:  RoleAgent,
					Parts: []Part{{Type: "text", Text: prompt}},
				},
				Timestamp: evt.Timestamp.Format(time.RFC3339),
			},
			Final: false,
		}

	case cpn.EventHITLResolved:
		return &TaskStatusUpdateEvent{
			ID:        taskID,
			ContextID: contextID,
			Status: TaskStatus{
				State:     TaskStateWorking,
				Timestamp: evt.Timestamp.Format(time.RFC3339),
			},
			Final: false,
		}

	case cpn.EventTransitionCompleted:
		return &TaskStatusUpdateEvent{
			ID:        taskID,
			ContextID: contextID,
			Status: TaskStatus{
				State:     TaskStateWorking,
				Timestamp: evt.Timestamp.Format(time.RFC3339),
			},
			Final: false,
		}

	default:
		return nil
	}
}

// SessionInfoToTask converts a SessionInfo snapshot to an A2A Task.
// Task ID uses composite format: "{sessionID}:0" per GUD-005.
func (m *Mapper) SessionInfoToTask(sessionID string, state cpn.State, createdAt time.Time, messages []cpn.Message) *Task {
	taskID := sessionID + ":0"
	contextID := sessionID

	a2aState := m.StateToA2A(state)

	// Build history from session messages.
	var history []Message
	var artifactParts []Part
	for _, msg := range messages {
		a2aMsg := m.CPNMessageToA2A(msg)
		history = append(history, a2aMsg)
		if msg.Role == cpn.RoleAssistant {
			artifactParts = append(artifactParts, Part{Type: "text", Text: msg.Content})
		}
	}

	// Build artifacts from assistant messages.
	var artifacts []Artifact
	if len(artifactParts) > 0 {
		artifacts = []Artifact{
			{
				Name:  "response",
				Parts: artifactParts,
				Index: 0,
			},
		}
	}

	return &Task{
		ID:        taskID,
		ContextID: contextID,
		Status: TaskStatus{
			State:     a2aState,
			Timestamp: createdAt.Format(time.RFC3339),
		},
		History:   history,
		Artifacts: artifacts,
	}
}
