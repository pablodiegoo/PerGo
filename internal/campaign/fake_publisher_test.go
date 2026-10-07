package campaign

import (
	"context"
	"sync"
)

// PublishedMessage records an outbound message published through Publisher.
type PublishedMessage struct {
	Subject string
	Data    []byte
	TraceID string
}

// FakePublisher is a thread-safe in-memory test double for Publisher.
type FakePublisher struct {
	mu       sync.Mutex
	messages []PublishedMessage
	err      error
}

// NewFakePublisher creates a new FakePublisher instance.
func NewFakePublisher() *FakePublisher {
	return &FakePublisher{}
}

// Publish stores the published message or returns the preconfigured error.
func (f *FakePublisher) Publish(ctx context.Context, subject string, data []byte, traceID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.messages = append(f.messages, PublishedMessage{
		Subject: subject,
		Data:    data,
		TraceID: traceID,
	})
	return nil
}

// SetError configures an error to return on future Publish calls.
func (f *FakePublisher) SetError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

// Messages returns a copy of all published messages.
func (f *FakePublisher) Messages() []PublishedMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]PublishedMessage, len(f.messages))
	copy(cp, f.messages)
	return cp
}

// MessagesBySubject returns all messages matching the given subject.
func (f *FakePublisher) MessagesBySubject(subject string) []PublishedMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []PublishedMessage
	for _, m := range f.messages {
		if m.Subject == subject {
			out = append(out, m)
		}
	}
	return out
}

// Reset clears recorded messages and any configured error.
func (f *FakePublisher) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.messages = nil
	f.err = nil
}
