// Package channel defines the dispatcher interface and registry for
// message channel adapters. Each adapter (WhatsApp Web, WABA, Telegram)
// implements the Dispatcher interface. The Registry provides lookup by
// channel name.
package channel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/domain"
)

// MessagePayload is the channel-layer message contract, separate from the
// API's CreateMessageRequest. It carries all fields needed for dispatch.
type MessagePayload struct {
	MessageID        string
	ConnectionID     uuid.UUID
	SenderIdentity   string
	TraceID          string
	To               string
	Channel          string
	Body             string
	Media            *domain.Media
	Metadata         map[string]string
	TemplateName     string
	Language         string
	Components       []domain.TemplateComponent
	Interactive      *domain.Interactive
	ChannelOverrides map[string]json.RawMessage
	FallbackBehavior string
	Type             string
	Product          *domain.ProductPayload
}

// Dispatcher sends a message through a specific channel adapter.
// Implementations must be safe for concurrent use.
type Dispatcher interface {
	Dispatch(ctx context.Context, m *MessagePayload) (string, error)
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

// UncertainError marks an error as uncertain (the adapter cannot verify if the provider received the message).
// The worker skips auto-retry on uncertain errors to prevent double dispatch.
type UncertainError struct {
	Err error
}

func (e *UncertainError) Error() string {
	return fmt.Sprintf("uncertain: %v", e.Err)
}

func (e *UncertainError) Unwrap() error {
	return e.Err
}

func (e *UncertainError) Uncertain() bool {
	return true
}

func NewUncertainError(err error) error {
	return &UncertainError{Err: err}
}

func IsUncertain(err error) bool {
	var u interface{ Uncertain() bool }
	return errors.As(err, &u) && u.Uncertain()
}
