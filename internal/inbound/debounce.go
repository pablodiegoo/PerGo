package inbound

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/pablojhp.pergo/internal/chat/activity"
	"github.com/redis/go-redis/v9"
)

// DefaultDebounceTTL is the default sliding window duration for message aggregation (12s).
const DefaultDebounceTTL = 12 * time.Second

// EventChatInboundCoalesced is the event name emitted when a customer's message burst is coalesced.
const EventChatInboundCoalesced = "chat.inbound.coalesced"

var flushScript = redis.NewScript(`
local messages = redis.call('LRANGE', KEYS[1], 0, -1)
if #messages > 0 then
    redis.call('DEL', KEYS[1])
    redis.call('DEL', KEYS[2])
    return messages
else
    return {}
end
`)

// DebouncedMessage represents a single message buffered in the Redis debounce list.
type DebouncedMessage struct {
	MessageID    string                 `json:"message_id"`
	TraceID      string                 `json:"trace_id"`
	WorkspaceID  uuid.UUID              `json:"workspace_id"`
	ConnectionID *uuid.UUID             `json:"connection_id,omitempty"`
	ChatID       uuid.UUID              `json:"chat_id"`
	ContactID    uuid.UUID              `json:"contact_id"`
	Channel      string                 `json:"channel"`
	From         string                 `json:"from"`
	To           string                 `json:"to"`
	SenderName   string                 `json:"sender_name,omitempty"`
	Body         string                 `json:"body"`
	OccurredAt   time.Time              `json:"occurred_at"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// CoalescedTurnEvent is the event payload dispatched to the AI consumer / NATS.
type CoalescedTurnEvent struct {
	Event        string                 `json:"event"` // "chat.inbound.coalesced"
	WorkspaceID  string                 `json:"workspace_id"`
	ContactID    string                 `json:"contact_id"`
	ConnectionID string                 `json:"connection_id,omitempty"`
	ChatID       string                 `json:"chat_id,omitempty"`
	Channel      string                 `json:"channel"`
	From         string                 `json:"from"`
	To           string                 `json:"to"`
	SenderName   string                 `json:"sender_name,omitempty"`
	Body         string                 `json:"body"` // coalesced with \n\n
	MessageIDs   []string               `json:"message_ids"`
	TraceID      string                 `json:"trace_id"`
	MessageCount int                    `json:"message_count"`
	OccurredAt   string                 `json:"occurred_at"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// CoalescedHandler defines a callback function invoked when a message window coalesces.
type CoalescedHandler func(ctx context.Context, ev *CoalescedTurnEvent) error

// Debouncer coordinates sliding-window buffering and coalescing for rapid sequential inbound messages.
type Debouncer struct {
	rdb       redis.Cmdable
	ttl       time.Duration
	publisher Publisher
	handler   CoalescedHandler
	activity  *activity.Recorder

	mu     sync.Mutex
	timers map[string]*time.Timer
}

// DebouncerOption configures a Debouncer instance.
type DebouncerOption func(*Debouncer)

// WithDebounceTTL overrides the default sliding window TTL.
func WithDebounceTTL(d time.Duration) DebouncerOption {
	return func(deb *Debouncer) {
		if d > 0 {
			deb.ttl = d
		}
	}
}

// WithDebouncePublisher configures a queue Publisher to emit chat.inbound.coalesced events.
func WithDebouncePublisher(p Publisher) DebouncerOption {
	return func(deb *Debouncer) {
		deb.publisher = p
	}
}

// WithDebounceHandler registers an in-process callback when messages coalesce.
func WithDebounceHandler(h CoalescedHandler) DebouncerOption {
	return func(deb *Debouncer) {
		deb.handler = h
	}
}

// WithDebounceActivity configures the Chat Activity recorder used to re-check AI engagement on Flush.
func WithDebounceActivity(rec *activity.Recorder) DebouncerOption {
	return func(deb *Debouncer) {
		deb.activity = rec
	}
}

// NewDebouncer creates and initializes a new Debouncer.
func NewDebouncer(rdb redis.Cmdable, opts ...DebouncerOption) *Debouncer {
	deb := &Debouncer{
		rdb:    rdb,
		ttl:    DefaultDebounceTTL,
		timers: make(map[string]*time.Timer),
	}
	for _, opt := range opts {
		opt(deb)
	}
	return deb
}

// SetPublisher configures the Publisher used to emit chat.inbound.coalesced events.
func (d *Debouncer) SetPublisher(pub Publisher) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.publisher = pub
}

// SetActivity configures the Chat Activity recorder used to re-check AI engagement on Flush.
func (d *Debouncer) SetActivity(rec *activity.Recorder) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.activity = rec
}

// Enqueue appends an incoming message to the Redis buffer and refreshes the sliding window timer.
func (d *Debouncer) Enqueue(ctx context.Context, msg *DebouncedMessage) error {
	if msg == nil {
		return fmt.Errorf("debouncer: message is nil")
	}
	chatID := msg.ChatID
	if chatID == uuid.Nil {
		chatID = msg.ContactID
	}
	if msg.WorkspaceID == uuid.Nil || chatID == uuid.Nil {
		return fmt.Errorf("debouncer: workspace_id and chat_id are required")
	}
	msg.ChatID = chatID

	bufferKey := fmt.Sprintf("debounce:%s:%s", msg.WorkspaceID, chatID)
	timerKey := fmt.Sprintf("debounce:timer:%s:%s", msg.WorkspaceID, chatID)
	mapKey := fmt.Sprintf("%s:%s", msg.WorkspaceID, chatID)

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("debouncer: marshal message: %w", err)
	}

	// 1. Redis atomic pipeline: append to list, set/refresh timer key with TTL, set safety TTL on list
	pipe := d.rdb.Pipeline()
	pipe.RPush(ctx, bufferKey, data)
	pipe.Set(ctx, timerKey, "1", d.ttl)

	listSafetyTTL := d.ttl * 10
	if listSafetyTTL < time.Hour {
		listSafetyTTL = time.Hour
	}
	pipe.Expire(ctx, bufferKey, listSafetyTTL)

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("debouncer: redis exec: %w", err)
	}

	// 2. Schedule or reset the in-memory timer
	d.mu.Lock()
	if t, ok := d.timers[mapKey]; ok {
		t.Stop()
	}
	wsID := msg.WorkspaceID
	d.timers[mapKey] = time.AfterFunc(d.ttl, func() {
		d.checkAndFlush(context.Background(), wsID, chatID)
	})
	d.mu.Unlock()

	return nil
}

// checkAndFlush verifies if the sliding window in Redis has expired before flushing.
func (d *Debouncer) checkAndFlush(ctx context.Context, wsID, chatID uuid.UUID) {
	timerKey := fmt.Sprintf("debounce:timer:%s:%s", wsID, chatID)
	mapKey := fmt.Sprintf("%s:%s", wsID, chatID)

	remTTL, err := d.rdb.TTL(ctx, timerKey).Result()
	if err == nil && remTTL > 0 {
		// New message arrived and refreshed the Redis sliding window; reschedule timer if still active.
		d.mu.Lock()
		if _, exists := d.timers[mapKey]; exists {
			d.timers[mapKey] = time.AfterFunc(remTTL, func() {
				d.checkAndFlush(ctx, wsID, chatID)
			})
		}
		d.mu.Unlock()
		return
	}

	// Window has officially expired
	d.mu.Lock()
	delete(d.timers, mapKey)
	d.mu.Unlock()

	if _, err := d.Flush(ctx, wsID, chatID); err != nil {
		slog.Error("debouncer: error flushing expired buffer", "error", err, "workspace_id", wsID, "chat_id", chatID)
	}
}

// Cancel stops any active in-memory debounce timer for the given workspace and chat
// and deletes the pending Redis buffer and timer keys so mid-window buffered messages
// are never flushed after a Human Handoff Trigger.
func (d *Debouncer) Cancel(ctx context.Context, wsID, chatID uuid.UUID) error {
	if d == nil {
		return nil
	}
	if wsID == uuid.Nil || chatID == uuid.Nil {
		return fmt.Errorf("debouncer: workspace_id and chat_id are required")
	}

	mapKey := fmt.Sprintf("%s:%s", wsID, chatID)
	d.mu.Lock()
	if t, ok := d.timers[mapKey]; ok {
		t.Stop()
		delete(d.timers, mapKey)
	}
	d.mu.Unlock()

	if d.rdb == nil {
		return nil
	}

	bufferKey := fmt.Sprintf("debounce:%s:%s", wsID, chatID)
	msgsKey := fmt.Sprintf("debounce:msgs:%s:%s", wsID, chatID)
	timerKey := fmt.Sprintf("debounce:timer:%s:%s", wsID, chatID)

	if err := d.rdb.Del(ctx, bufferKey, msgsKey, timerKey).Err(); err != nil {
		return fmt.Errorf("debouncer: cancel redis del: %w", err)
	}
	return nil
}

// Flush atomically extracts all messages currently in the buffer, coalesces them chronologically,
// publishes the chat.inbound.coalesced event, and invokes the registered handler.
// If an activity.Recorder is configured, Flush re-checks Snapshot(wsID, ChatID(chatID)) and drops
// the turn if AI is no longer engaged (cross-process safety net against a human turn committed on another replica).
func (d *Debouncer) Flush(ctx context.Context, wsID, chatID uuid.UUID) (*CoalescedTurnEvent, error) {
	bufferKey := fmt.Sprintf("debounce:%s:%s", wsID, chatID)
	timerKey := fmt.Sprintf("debounce:timer:%s:%s", wsID, chatID)
	mapKey := fmt.Sprintf("%s:%s", wsID, chatID)

	d.mu.Lock()
	if t, ok := d.timers[mapKey]; ok {
		t.Stop()
		delete(d.timers, mapKey)
	}
	rec := d.activity
	d.mu.Unlock()

	res, err := flushScript.Run(ctx, d.rdb, []string{bufferKey, timerKey}).StringSlice()
	if err != nil {
		return nil, fmt.Errorf("debouncer: flush script error: %w", err)
	}
	if len(res) == 0 {
		return nil, nil
	}

	if rec != nil && chatID != uuid.Nil {
		state, ok, sErr := rec.Snapshot(ctx, wsID, activity.ChatID(chatID))
		if sErr == nil && ok && !state.AI.Effective(time.Now().UTC()).IsEngaged() {
			return nil, nil
		}
	}

	var messages []DebouncedMessage
	for _, raw := range res {
		var m DebouncedMessage
		if err := json.Unmarshal([]byte(raw), &m); err == nil {
			messages = append(messages, m)
		}
	}
	if len(messages) == 0 {
		return nil, nil
	}

	// Chronological ordering
	sort.SliceStable(messages, func(i, j int) bool {
		return messages[i].OccurredAt.Before(messages[j].OccurredAt)
	})

	var bodies []string
	var messageIDs []string
	var lastMsg DebouncedMessage

	for _, m := range messages {
		trimmed := strings.TrimSpace(m.Body)
		if trimmed != "" {
			bodies = append(bodies, trimmed)
		}
		if m.MessageID != "" {
			messageIDs = append(messageIDs, m.MessageID)
		}
		lastMsg = m
	}

	var connIDStr string
	if lastMsg.ConnectionID != nil {
		connIDStr = lastMsg.ConnectionID.String()
	}

	contactIDStr := lastMsg.ContactID.String()
	if lastMsg.ContactID == uuid.Nil {
		contactIDStr = chatID.String()
	}

	coalesced := &CoalescedTurnEvent{
		Event:        EventChatInboundCoalesced,
		WorkspaceID:  wsID.String(),
		ContactID:    contactIDStr,
		ConnectionID: connIDStr,
		ChatID:       chatID.String(),
		Channel:      lastMsg.Channel,
		From:         lastMsg.From,
		To:           lastMsg.To,
		SenderName:   lastMsg.SenderName,
		Body:         strings.Join(bodies, "\n\n"),
		MessageIDs:   messageIDs,
		TraceID:      lastMsg.TraceID,
		MessageCount: len(messages),
		OccurredAt:   lastMsg.OccurredAt.UTC().Format(time.RFC3339),
		Metadata:     lastMsg.Metadata,
	}

	d.mu.Lock()
	pub := d.publisher
	d.mu.Unlock()

	if pub != nil {
		eventData, mErr := json.Marshal(coalesced)
		if mErr == nil {
			_ = pub.Publish(ctx, EventChatInboundCoalesced, eventData, lastMsg.TraceID)
			_ = pub.Publish(ctx, fmt.Sprintf("%s.%s", EventChatInboundCoalesced, wsID.String()), eventData, lastMsg.TraceID)
		}
	}

	if d.handler != nil {
		if hErr := d.handler(ctx, coalesced); hErr != nil {
			slog.Error("debouncer: handler error", "error", hErr, "workspace_id", wsID, "chat_id", chatID)
		}
	}

	return coalesced, nil
}

// Close stops all active in-memory debounce timers.
func (d *Debouncer) Close() {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for k, t := range d.timers {
		t.Stop()
		delete(d.timers, k)
	}
}
