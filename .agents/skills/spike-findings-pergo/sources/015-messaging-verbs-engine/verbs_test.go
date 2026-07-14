package verbs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"
)

// Verb represents a single executable action returned by a webhook flow response.
type Verb struct {
	Action string          `json:"action"`
	Params json.RawMessage `json:"params"`
}

// ReplyParams defines the payload structure for the 'reply' action.
type ReplyParams struct {
	Body string `json:"body"`
}

// WaitParams defines the payload structure for the 'wait' action.
type WaitParams struct {
	Duration string `json:"duration"` // e.g., "500ms", "1s"
}

// ForwardParams defines the payload structure for the 'forward' action.
type ForwardParams struct {
	To      string `json:"to"`
	Channel string `json:"channel"`
}

// MockSender simulates outbound message dispatching.
type MockSender struct {
	Sent []string
}

func (m *MockSender) Send(ctx context.Context, body string) error {
	m.Sent = append(m.Sent, body)
	return nil
}

func (m *MockSender) Forward(ctx context.Context, to, channel, originalBody string) error {
	m.Sent = append(m.Sent, fmt.Sprintf("FORWARD to %s via %s: %s", to, channel, originalBody))
	return nil
}

// Engine processes a slice of verbs sequentially.
type Engine struct {
	sender *MockSender
}

func NewEngine(sender *MockSender) *Engine {
	return &Engine{sender: sender}
}

func (e *Engine) Execute(ctx context.Context, verbs []Verb, originalBody string) error {
	for i, verb := range verbs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		switch verb.Action {
		case "reply":
			var p ReplyParams
			if err := json.Unmarshal(verb.Params, &p); err != nil {
				return fmt.Errorf("verb %d (reply) invalid params: %w", i, err)
			}
			if err := e.sender.Send(ctx, p.Body); err != nil {
				return fmt.Errorf("verb %d (reply) send failed: %w", i, err)
			}

		case "wait":
			var p WaitParams
			if err := json.Unmarshal(verb.Params, &p); err != nil {
				return fmt.Errorf("verb %d (wait) invalid params: %w", i, err)
			}
			d, err := time.ParseDuration(p.Duration)
			if err != nil {
				return fmt.Errorf("verb %d (wait) invalid duration '%s': %w", i, p.Duration, err)
			}
			select {
			case <-time.After(d):
			case <-ctx.Done():
				return ctx.Err()
			}

		case "forward":
			var p ForwardParams
			if err := json.Unmarshal(verb.Params, &p); err != nil {
				return fmt.Errorf("verb %d (forward) invalid params: %w", i, err)
			}
			if err := e.sender.Forward(ctx, p.To, p.Channel, originalBody); err != nil {
				return fmt.Errorf("verb %d (forward) failed: %w", i, err)
			}

		default:
			return fmt.Errorf("verb %d unknown action: %s", i, verb.Action)
		}
	}
	return nil
}

func TestEngine_Execute(t *testing.T) {
	t.Run("Executes reply wait and forward verbs sequentially", func(t *testing.T) {
		sender := &MockSender{}
		engine := NewEngine(sender)

		payload := `[
			{"action": "reply", "params": {"body": "Recebido! Processando..."}},
			{"action": "wait", "params": {"duration": "10ms"}},
			{"action": "forward", "params": {"to": "+5511999990001", "channel": "whatsapp_cloud"}}
		]`

		var verbs []Verb
		if err := json.Unmarshal([]byte(payload), &verbs); err != nil {
			t.Fatalf("failed to unmarshal test verbs: %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		start := time.Now()
		err := engine.Execute(ctx, verbs, "Mensagem original do cliente")
		elapsed := time.Since(start)

		if err != nil {
			t.Fatalf("execute failed: %v", err)
		}

		if elapsed < 10*time.Millisecond {
			t.Errorf("expected execution to take at least 10ms (due to wait verb), took: %v", elapsed)
		}

		if len(sender.Sent) != 2 {
			t.Fatalf("expected 2 sent/forwarded messages, got: %d", len(sender.Sent))
		}

		if sender.Sent[0] != "Recebido! Processando..." {
			t.Errorf("got sent[0] = %s, want 'Recebido! Processando...'", sender.Sent[0])
		}

		wantForward := "FORWARD to +5511999990001 via whatsapp_cloud: Mensagem original do cliente"
		if sender.Sent[1] != wantForward {
			t.Errorf("got sent[1] = %s, want '%s'", sender.Sent[1], wantForward)
		}
	})

	t.Run("Context cancellation aborts sequence", func(t *testing.T) {
		sender := &MockSender{}
		engine := NewEngine(sender)

		payload := `[
			{"action": "reply", "params": {"body": "Pass 1"}},
			{"action": "wait", "params": {"duration": "1s"}},
			{"action": "reply", "params": {"body": "Pass 2"}}
		]`

		var verbs []Verb
		if err := json.Unmarshal([]byte(payload), &verbs); err != nil {
			t.Fatalf("failed to unmarshal test verbs: %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		err := engine.Execute(ctx, verbs, "Original")
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("expected context.DeadlineExceeded, got: %v", err)
		}

		if len(sender.Sent) != 1 {
			t.Errorf("expected only 1 message to be sent before cancellation, got: %d", len(sender.Sent))
		}
	})
}
