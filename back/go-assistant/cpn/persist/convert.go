package persist

import (
	"encoding/json"
	"fmt"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// EventToRecord converts a domain cpn.Event to a persistence EventRecord.
// Requires sessionID because cpn.Event may not always carry it (defensive).
// Returns error if Token or Payload serialization fails.
func EventToRecord(sessionID string, e *cpn.Event) (*EventRecord, error) {
	var tokenSnap json.RawMessage
	if e.Token != nil {
		b, err := json.Marshal(e.Token)
		if err != nil {
			return nil, fmt.Errorf("marshal token: %w", err)
		}
		tokenSnap = b
	}

	var payload json.RawMessage
	if e.Payload != nil {
		b, err := json.Marshal(e.Payload)
		if err != nil {
			return nil, fmt.Errorf("marshal payload: %w", err)
		}
		payload = b
	}

	sid := sessionID
	if e.SessionID != "" {
		sid = e.SessionID
	}

	return &EventRecord{
		ID:             e.ID,
		Type:           string(e.Type),
		SessionID:      sid,
		CPNID:          e.CPNID,
		CPNRole:        e.CPNRole,
		CPNDepth:       e.CPNDepth,
		TransitionID:   e.TransitionID,
		TransitionKind: string(e.TransitionKind),
		TokenSnapshot:  tokenSnap,
		Payload:        payload,
		Timestamp:      e.Timestamp,
	}, nil
}

// MessageToRecord converts a domain cpn.Message to a persistence MessageRecord.
// Injects sessionID because cpn.Message does not carry it.
func MessageToRecord(sessionID string, m *cpn.Message) *MessageRecord {
	return &MessageRecord{
		ID:              m.ID,
		SessionID:       sessionID,
		Role:            string(m.Role),
		Content:         m.Content,
		CPNID:           m.CPNID,
		CPNRole:         m.CPNRole,
		CPNDepth:        m.CPNDepth,
		Timestamp:       m.Timestamp,
		ParentMessageID: m.ParentMessageID,
	}
}
