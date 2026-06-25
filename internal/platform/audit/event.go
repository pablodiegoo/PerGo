// Package audit provides the audit event type and buffered batch writer
// for OmniGo's audit logging subsystem.
package audit

import (
	"time"

	"github.com/google/uuid"
)

// Event represents a single audit log entry to be written to PostgreSQL.
type Event struct {
	WorkspaceID uuid.UUID
	TraceID     string
	EventType   string
	Payload     []byte
	CreatedAt   time.Time
}

// NewEvent creates an audit event with CreatedAt set to time.Now().
func NewEvent(wsID uuid.UUID, traceID, eventType string, payload []byte) Event {
	return Event{
		WorkspaceID: wsID,
		TraceID:     traceID,
		EventType:   eventType,
		Payload:     payload,
		CreatedAt:   time.Now(),
	}
}
