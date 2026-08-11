package audit_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/platform/audit"
)

func TestBatchWriter_MemoryBufferAndClose(t *testing.T) {
	writer := audit.NewWriter(nil, 10, 2)
	event := audit.Event{
		WorkspaceID: uuid.New(),
		TraceID:     "trace_audit_1",
		EventType:   "message.sent",
		Payload:     []byte(`{"status":"ok"}`),
		CreatedAt:   time.Now(),
	}

	err := writer.Write(event)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	err = writer.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}
