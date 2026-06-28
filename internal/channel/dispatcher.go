// Package channel defines the dispatcher interface and registry for
// message channel adapters. Each adapter (WhatsApp Web, WABA, Telegram)
// implements the Dispatcher interface. The Registry provides lookup by
// channel name.
package channel

import (
	"context"
	"errors"
	"fmt"

	"github.com/pablojhp.pergo/internal/domain"
)

// MessagePayload is the channel-layer message contract, separate from the
// API's CreateMessageRequest. It carries all fields needed for dispatch.
type MessagePayload struct {
	MessageID    string
	TraceID      string
	To           string
	Channel      string
	Body         string
	Media        *domain.Media
	Metadata     map[string]string
	TemplateName string
	Language     string
	Components   []TemplateComponent
}

// TemplateComponent represents a template component inside MessagePayload.
type TemplateComponent struct {
	Type       string              `json:"type"`
	Parameters []TemplateParameter `json:"parameters"`
}

// TemplateParameter represents a template parameter inside MessagePayload.
type TemplateParameter struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}


// Dispatcher sends a message through a specific channel adapter.
// Implementations must be safe for concurrent use.
type Dispatcher interface {
	Dispatch(ctx context.Context, m *MessagePayload) error
}

// TerminalError marks an error as non-retryable. The worker and routing
// engine use errors.As to detect terminal errors and skip retries or
// advance fallback channels.
type TerminalError struct {
	Err error
}

func (e *TerminalError) Error() string {
	return fmt.Sprintf("terminal: %v", e.Err)
}

func (e *TerminalError) Unwrap() error {
	return e.Err
}

// Terminal returns true — satisfies the Terminal interface.
func (e *TerminalError) Terminal() bool {
	return true
}

// NewTerminalError wraps an error as terminal (non-retryable).
func NewTerminalError(err error) error {
	return &TerminalError{Err: err}
}

// IsTerminal checks if an error is terminal (non-retryable).
func IsTerminal(err error) bool {
	var t interface{ Terminal() bool }
	return errors.As(err, &t) && t.Terminal()
}
