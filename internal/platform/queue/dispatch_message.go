package queue

import "time"

// DispatchMessage is the port abstracting a JetStream message.
// The orchestrator uses this to ack or nak without depending on
// concrete NATS types. Two adapters justify the seam: JetStream
// in production, a fake in tests.
type DispatchMessage interface {
	Data() []byte
	Headers() map[string]string
	Ack() error
	NakWithDelay(delay time.Duration) error
}
