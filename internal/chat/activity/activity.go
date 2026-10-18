package activity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pablojhp.pergo/internal/domain"
)

var (
	ErrInvalidWorkspaceID  = errors.New("invalid workspace_id: cannot be nil")
	ErrInvalidUID          = errors.New("invalid uid: cannot be empty")
	ErrChatNotFound        = errors.New("chat not found")
	ErrChatMessageNotFound = errors.New("chat message not found")
)

// Publisher publishes post-commit events (such as chat.handoff.*).
type Publisher interface {
	Publish(ctx context.Context, subject string, data []byte, traceID string) error
}

// DebounceCanceller cancels pending inbound debounce buffers keyed by (workspace_id, chat_id).
type DebounceCanceller interface {
	Cancel(ctx context.Context, workspaceID, chatID uuid.UUID) error
}

// PublishedMessage records a message emitted to MemoryPublisher.
type PublishedMessage struct {
	Subject string
	Data    []byte
	TraceID string
}

// MemoryPublisher is an in-memory Publisher adapter for tests.
type MemoryPublisher struct {
	mu   sync.Mutex
	msgs []PublishedMessage
}

// NewMemoryPublisher creates an in-memory Publisher.
func NewMemoryPublisher() *MemoryPublisher {
	return &MemoryPublisher{}
}

func (p *MemoryPublisher) Publish(_ context.Context, subject string, data []byte, traceID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	cp := make([]byte, len(data))
	copy(cp, data)
	p.msgs = append(p.msgs, PublishedMessage{
		Subject: subject,
		Data:    cp,
		TraceID: traceID,
	})
	return nil
}

// Messages returns a copy of all published messages.
func (p *MemoryPublisher) Messages() []PublishedMessage {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]PublishedMessage, len(p.msgs))
	copy(out, p.msgs)
	return out
}

// Reset clears all recorded messages.
func (p *MemoryPublisher) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.msgs = nil
}

// CancelledKey records a (WorkspaceID, ChatID) pair cancelled on MemoryDebounceCanceller.
type CancelledKey struct {
	WorkspaceID uuid.UUID
	ChatID      uuid.UUID
}

// MemoryDebounceCanceller is an in-memory DebounceCanceller adapter for tests.
type MemoryDebounceCanceller struct {
	mu        sync.Mutex
	cancelled []CancelledKey
}

// NewMemoryDebounceCanceller creates an in-memory DebounceCanceller.
func NewMemoryDebounceCanceller() *MemoryDebounceCanceller {
	return &MemoryDebounceCanceller{}
}

func (d *MemoryDebounceCanceller) Cancel(_ context.Context, workspaceID, chatID uuid.UUID) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cancelled = append(d.cancelled, CancelledKey{
		WorkspaceID: workspaceID,
		ChatID:      chatID,
	})
	return nil
}

// Cancelled returns a copy of all cancelled keys.
func (d *MemoryDebounceCanceller) Cancelled() []CancelledKey {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]CancelledKey, len(d.cancelled))
	copy(out, d.cancelled)
	return out
}

// Reset clears all recorded cancellations.
func (d *MemoryDebounceCanceller) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cancelled = nil
}

// ChatRef identifies or resolves a Chat in a workspace.
type ChatRef struct {
	id        uuid.UUID
	connID    uuid.UUID
	contactID uuid.UUID
	address   string
	channel   string
}

// ChatID identifies an existing Chat by its UUID.
func ChatID(id uuid.UUID) ChatRef {
	return ChatRef{id: id}
}

// ChatOf identifies or creates a Chat for (workspace, connection, contact).
// Pass uuid.Nil for connID when no specific connection is bound.
func ChatOf(connID, contactID uuid.UUID) ChatRef {
	return ChatRef{connID: connID, contactID: contactID}
}

// ChatAt identifies an existing Chat by (workspace, connection/channel, address).
// ChatAt is strictly read-only and never creates a Contact or Chat.
func ChatAt(connID uuid.UUID, address string) ChatRef {
	return ChatRef{connID: connID, address: strings.TrimSpace(address)}
}

// WithChannel attaches an explicit channel hint to a ChatRef.
func (r ChatRef) WithChannel(channel string) ChatRef {
	r.channel = strings.TrimSpace(channel)
	return r
}

// AIChange describes an explicit AI Engagement transition.
type AIChange struct {
	mode  domain.AIMode
	until *time.Time
}

// AIOff disables AI indefinitely until manual resume.
func AIOff() AIChange {
	return AIChange{mode: domain.AIModeOff}
}

// AIResume re-enables AI to active.
func AIResume() AIChange {
	return AIChange{mode: domain.AIModeActive}
}

// AIPauseUntil pauses AI until the given timestamp (unless already off).
func AIPauseUntil(t time.Time) AIChange {
	u := t.UTC()
	return AIChange{mode: domain.AIModePaused, until: &u}
}

// EffectiveStatusSQL returns a SQL expression evaluating the effective Chat status at nowExpr.
// When statusCol is 'snoozed' and snoozedUntilCol is at or before nowExpr, it evaluates to 'open'.
func EffectiveStatusSQL(statusCol, snoozedUntilCol, nowExpr string) string {
	return fmt.Sprintf(
		`(CASE WHEN %s = 'snoozed' AND %s IS NOT NULL AND %s <= %s THEN 'open' WHEN %s = '' OR %s IS NULL THEN 'open' ELSE %s END)`,
		statusCol, snoozedUntilCol, snoozedUntilCol, nowExpr, statusCol, statusCol, statusCol,
	)
}

// StatusPredicate returns a SQL boolean predicate matching rows whose effective status equals targetStatusExpr at nowExpr.
func StatusPredicate(statusCol, snoozedUntilCol, targetStatusExpr, nowExpr string) string {
	return fmt.Sprintf(
		`(CASE WHEN %s = 'open' THEN (%s = 'open' OR (%s = 'snoozed' AND %s IS NOT NULL AND %s <= %s)) WHEN %s = 'snoozed' THEN (%s = 'snoozed' AND (%s IS NULL OR %s > %s)) ELSE %s = %s END)`,
		targetStatusExpr, statusCol, statusCol, snoozedUntilCol, snoozedUntilCol, nowExpr,
		targetStatusExpr, statusCol, snoozedUntilCol, snoozedUntilCol, nowExpr,
		statusCol, targetStatusExpr,
	)
}

// InboundTurn holds the inputs for recording an inbound customer turn via Recorder.Inbound.
type InboundTurn struct {
	Chat           ChatRef
	UID            string
	SenderName     string
	SenderID       string
	ReplyToUID     *string
	Body           string
	MediaURL       *string
	MediaType      *string
	Channel        string
	EntryPointType string
	Metadata       map[string]interface{}
	At             time.Time
}

// InboundReceipt is returned by Recorder.Inbound.
type InboundReceipt struct {
	Chat               domain.ChatState
	Message            *domain.ChatMessage
	Duplicate          bool
	At                 time.Time
	PrevLastOutboundAt *time.Time
}

// AIMayAct reports whether AI automation (debounce and auto-reply routing) may run for this turn.
func (r InboundReceipt) AIMayAct() bool {
	if r.Duplicate {
		return false
	}
	at := r.At.UTC()
	if at.IsZero() {
		at = time.Now().UTC()
	}
	return r.Chat.AI.Effective(at).IsEngaged()
}

// OutboundTurn holds the inputs for recording an outbound turn via Recorder.Outbound.
type OutboundTurn struct {
	Chat         ChatRef
	UID          string
	SenderType   domain.SenderType
	SenderName   string
	SenderID     string
	ReplyToUID   *string
	Body         string
	Media        *domain.Media
	Product      *domain.ProductPayload
	TemplateName string
	Language     string
	MessageType  string
	Channel      string
	To           string
	Metadata     map[string]string
	At           time.Time
}

// OutboundReceipt is returned by Recorder.Outbound.
type OutboundReceipt struct {
	Chat      domain.ChatState
	Message   *domain.ChatMessage
	Duplicate bool
}

// ExpiringChat represents a whatsapp_cloud Chat whose customer service window is expiring within the claimed horizon.
type ExpiringChat struct {
	ChatID         uuid.UUID
	WorkspaceID    uuid.UUID
	ConnectionID   uuid.UUID
	ContactID      uuid.UUID
	RecipientPhone string
	Channel        string
	EntryPointType string
	ExpiresAt      time.Time
}

// Recorder is the single writer for activity-driven Chat state and ChatMessages.
type Recorder struct {
	pool *pgxpool.Pool
	pub  Publisher
	deb  DebounceCanceller
	log  *slog.Logger
}

func isNilInterface(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Slice, reflect.Map, reflect.Chan, reflect.Func:
		return rv.IsNil()
	default:
		return false
	}
}

// New constructs a Chat Activity Recorder.
func New(pool *pgxpool.Pool, pub Publisher, deb DebounceCanceller, log *slog.Logger) *Recorder {
	if log == nil {
		log = slog.Default()
	}
	if isNilInterface(pub) {
		pub = nil
	}
	if isNilInterface(deb) {
		deb = nil
	}
	return &Recorder{
		pool: pool,
		pub:  pub,
		deb:  deb,
		log:  log,
	}
}

// Pool returns the underlying database connection pool.
func (r *Recorder) Pool() *pgxpool.Pool {
	if r == nil {
		return nil
	}
	return r.pool
}

// SetPublisher updates the Publisher on the Recorder.
func (r *Recorder) SetPublisher(pub Publisher) {
	if r != nil {
		if isNilInterface(pub) {
			pub = nil
		}
		r.pub = pub
	}
}

// SetDebounceCanceller updates the DebounceCanceller on the Recorder.
func (r *Recorder) SetDebounceCanceller(deb DebounceCanceller) {
	if r != nil {
		if isNilInterface(deb) {
			deb = nil
		}
		r.deb = deb
	}
}

// Inbound locks the target Chat, enforces idempotency on turn.UID, applies domain.Apply(state, domain.InboundTurn),
// inserts the inbound ChatMessage, and updates the Chat state (including service window and notified_expiring_at reset) in one transaction.
func (r *Recorder) Inbound(ctx context.Context, ws uuid.UUID, turn InboundTurn) (InboundReceipt, error) {
	if r == nil || r.pool == nil {
		return InboundReceipt{}, errors.New("activity recorder pool is not configured")
	}
	uid := strings.TrimSpace(turn.UID)
	if uid == "" {
		return InboundReceipt{}, ErrInvalidUID
	}

	at := turn.At.UTC()
	if at.IsZero() {
		at = time.Now().UTC()
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return InboundReceipt{}, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	ref := turn.Chat
	if ref.channel == "" && turn.Channel != "" {
		ref.channel = turn.Channel
	}
	allowCreate := ref.contactID != uuid.Nil
	state, err := r.resolveAndLockChat(ctx, tx, ws, ref, allowCreate)
	if err != nil {
		return InboundReceipt{}, err
	}
	ws = state.WorkspaceID
	prevLastOutbound := state.LastOutboundAt

	// Idempotency check on (workspace_id, uid)
	existingMsg, err := loadMessageByUID(ctx, tx, ws, uid)
	if err == nil && existingMsg != nil {
		if err := tx.Commit(ctx); err != nil {
			return InboundReceipt{}, fmt.Errorf("commit duplicate inbound tx: %w", err)
		}
		return InboundReceipt{
			Chat:               state,
			Message:            existingMsg,
			Duplicate:          true,
			At:                 at,
			PrevLastOutboundAt: prevLastOutbound,
		}, nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return InboundReceipt{}, fmt.Errorf("check duplicate inbound message uid: %w", err)
	}

	channel := strings.TrimSpace(turn.Channel)
	if channel == "" {
		channel = ref.channel
	}
	if channel == "" {
		channel = state.Channel
	}
	if channel == "" && state.ConnectionID != nil && *state.ConnectionID != uuid.Nil {
		_ = tx.QueryRow(ctx, `SELECT channel FROM connections WHERE id = $1`, *state.ConnectionID).Scan(&channel)
	}

	nextState, msg, intents := domain.Apply(state, domain.InboundTurn{
		At:             at,
		UID:            uid,
		SenderName:     turn.SenderName,
		SenderID:       turn.SenderID,
		ReplyToUID:     turn.ReplyToUID,
		Body:           turn.Body,
		MediaURL:       turn.MediaURL,
		MediaType:      turn.MediaType,
		Channel:        channel,
		EntryPointType: turn.EntryPointType,
		Metadata:       turn.Metadata,
	})

	if err := insertChatMessage(ctx, tx, msg); err != nil {
		return InboundReceipt{}, err
	}

	if err := updateInboundChatState(ctx, tx, nextState, at); err != nil {
		return InboundReceipt{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return InboundReceipt{}, fmt.Errorf("commit inbound tx: %w", err)
	}

	r.executeIntents(ctx, intents)

	return InboundReceipt{
		Chat:               nextState,
		Message:            msg,
		Duplicate:          false,
		At:                 at,
		PrevLastOutboundAt: prevLastOutbound,
	}, nil
}

// Outbound locks the target Chat, enforces idempotency on turn.UID and the WABA service window gate,
// applies domain.Apply, inserts the queued ChatMessage and updates the Chat in one transaction,
// and executes post-commit intents (debounce cancellation and handoff event publication).
func (r *Recorder) Outbound(ctx context.Context, ws uuid.UUID, turn OutboundTurn) (OutboundReceipt, error) {
	if r == nil || r.pool == nil {
		return OutboundReceipt{}, errors.New("activity recorder pool is not configured")
	}
	uid := strings.TrimSpace(turn.UID)
	if uid == "" {
		return OutboundReceipt{}, ErrInvalidUID
	}

	at := turn.At.UTC()
	if at.IsZero() {
		at = time.Now().UTC()
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return OutboundReceipt{}, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	ref := turn.Chat
	if ref.channel == "" && turn.Channel != "" {
		ref.channel = turn.Channel
	}

	channel := strings.TrimSpace(turn.Channel)
	if channel == "" {
		channel = ref.channel
	}
	if channel == "" && ref.connID != uuid.Nil {
		_ = tx.QueryRow(ctx, `SELECT channel FROM connections WHERE id = $1`, ref.connID).Scan(&channel)
	}

	msgType := strings.TrimSpace(turn.MessageType)
	if strings.TrimSpace(turn.TemplateName) != "" {
		msgType = "template"
	}
	if msgType == "" {
		msgType = "text"
	}

	// First look up and lock an existing Chat without auto-creating so we can evaluate domain.Window(state, exists, at).
	state, err := r.resolveAndLockChat(ctx, tx, ws, ref, false)
	exists := err == nil
	if err != nil && !errors.Is(err, ErrChatNotFound) {
		return OutboundReceipt{}, err
	}

	if exists {
		ws = state.WorkspaceID
		if channel == "" {
			channel = state.Channel
		}
		if channel == "" && state.ConnectionID != nil && *state.ConnectionID != uuid.Nil {
			_ = tx.QueryRow(ctx, `SELECT channel FROM connections WHERE id = $1`, *state.ConnectionID).Scan(&channel)
		}
		state.Channel = channel

		// Idempotency check on (workspace_id, uid)
		existingMsg, mErr := loadMessageByUID(ctx, tx, ws, uid)
		if mErr == nil && existingMsg != nil {
			if cErr := tx.Commit(ctx); cErr != nil {
				return OutboundReceipt{}, fmt.Errorf("commit duplicate tx: %w", cErr)
			}
			return OutboundReceipt{
				Chat:      state,
				Message:   existingMsg,
				Duplicate: true,
			}, nil
		}
		if mErr != nil && !errors.Is(mErr, pgx.ErrNoRows) {
			return OutboundReceipt{}, fmt.Errorf("check duplicate message uid: %w", mErr)
		}
	} else {
		state.Channel = channel
	}

	// Enforce WABA customer service window before any Chat creation or message insert.
	w := domain.Window(state, exists, at)
	if err := domain.RequireOpenWindow(w, msgType); err != nil {
		return OutboundReceipt{}, err
	}

	if !exists {
		// Only ChatOf(conn, contact) may create a new Chat; ChatID and ChatAt are read-only when no Chat exists.
		if ref.contactID == uuid.Nil {
			return OutboundReceipt{}, ErrChatNotFound
		}
		state, err = r.resolveAndLockChat(ctx, tx, ws, ref, true)
		if err != nil {
			return OutboundReceipt{}, err
		}
		ws = state.WorkspaceID
		state.Channel = channel
	}

	nextState, msg, intents := domain.Apply(state, domain.OutboundActivity{
		At:           at,
		UID:          uid,
		SenderType:   turn.SenderType,
		SenderName:   turn.SenderName,
		SenderID:     turn.SenderID,
		ReplyToUID:   turn.ReplyToUID,
		Body:         turn.Body,
		Media:        turn.Media,
		Product:      turn.Product,
		TemplateName: turn.TemplateName,
		Language:     turn.Language,
		MessageType:  turn.MessageType,
		Channel:      channel,
		To:           turn.To,
		Metadata:     turn.Metadata,
	})

	if err := insertChatMessage(ctx, tx, msg); err != nil {
		return OutboundReceipt{}, err
	}

	if err := updateOutboundChatState(ctx, tx, nextState, at); err != nil {
		return OutboundReceipt{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return OutboundReceipt{}, fmt.Errorf("commit outbound tx: %w", err)
	}

	r.executeIntents(ctx, intents)

	return OutboundReceipt{
		Chat:      nextState,
		Message:   msg,
		Duplicate: false,
	}, nil
}

// TouchOutbound updates last_outbound_at on an existing Chat.
// If no Contact or Chat exists for ref, TouchOutbound is a no-op and never creates a Contact or Chat.
func (r *Recorder) TouchOutbound(ctx context.Context, ws uuid.UUID, ref ChatRef, at time.Time) error {
	if r == nil || r.pool == nil {
		return nil
	}
	if ws == uuid.Nil && ref.id == uuid.Nil {
		return ErrInvalidWorkspaceID
	}
	at = at.UTC()
	if at.IsZero() {
		at = time.Now().UTC()
	}

	if ref.id != uuid.Nil {
		if ws != uuid.Nil {
			_, err := r.pool.Exec(ctx, `
				UPDATE chats
				SET last_outbound_at = GREATEST(COALESCE(last_outbound_at, $3), $3),
				    updated_at = $3
				WHERE workspace_id = $1 AND id = $2
			`, ws, ref.id, at)
			return err
		}
		_, err := r.pool.Exec(ctx, `
			UPDATE chats
			SET last_outbound_at = GREATEST(COALESCE(last_outbound_at, $2), $2),
			    updated_at = $2
			WHERE id = $1
		`, ref.id, at)
		return err
	}

	contactID := ref.contactID
	if contactID == uuid.Nil && ref.address != "" {
		channel := ref.channel
		if channel == "" && ref.connID != uuid.Nil {
			_ = r.pool.QueryRow(ctx, `SELECT channel FROM connections WHERE workspace_id = $1 AND id = $2`, ws, ref.connID).Scan(&channel)
		}
		contactID = lookupContactIDByAddress(ctx, r.pool, ws, channel, ref.address)
	}
	if contactID == uuid.Nil {
		return nil
	}

	var connPtr *uuid.UUID
	if ref.connID != uuid.Nil {
		c := ref.connID
		connPtr = &c
	}

	_, err := r.pool.Exec(ctx, `
		UPDATE chats
		SET last_outbound_at = GREATEST(COALESCE(last_outbound_at, $4), $4),
		    updated_at = $4
		WHERE id = (
			SELECT id FROM chats
			WHERE workspace_id = $1 AND contact_id = $2
			  AND ($3::uuid IS NULL OR connection_id = $3 OR connection_id IS NULL)
			ORDER BY (connection_id = $3) DESC NULLS LAST, (connection_id IS NOT NULL) DESC, last_message_at DESC
			LIMIT 1
		)
	`, ws, contactID, connPtr, at)
	return err
}

// ClaimExpiring claims up to limit whatsapp_cloud Chats whose customer service window expires within
// (now, now+within] and have not yet been notified for the current window.
func (r *Recorder) ClaimExpiring(ctx context.Context, now time.Time, within time.Duration, limit int) ([]ExpiringChat, error) {
	if r == nil || r.pool == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}
	nowUTC := now.UTC()
	if nowUTC.IsZero() {
		nowUTC = time.Now().UTC()
	}
	horizonEnd := nowUTC.Add(within)

	rows, err := r.pool.Query(ctx, `
		WITH candidates AS (
			SELECT c.id
			FROM chats c
			LEFT JOIN connections conn ON conn.id = c.connection_id
			WHERE c.notified_expiring_at IS NULL
			  AND c.service_window_expires_at IS NOT NULL
			  AND c.service_window_expires_at > $1
			  AND c.service_window_expires_at <= $2
			  AND (
			      (c.connection_id IS NOT NULL AND conn.channel = 'whatsapp_cloud')
			      OR (c.connection_id IS NULL AND EXISTS (
			          SELECT 1 FROM contact_identities ci
			          WHERE ci.workspace_id = c.workspace_id
			            AND ci.contact_id = c.contact_id
			            AND ci.channel = 'whatsapp_cloud'
			      ))
			  )
			ORDER BY c.service_window_expires_at ASC
			LIMIT $3
			FOR UPDATE OF c SKIP LOCKED
		)
		UPDATE chats c
		SET notified_expiring_at = $1,
		    updated_at = $1
		FROM candidates
		WHERE c.id = candidates.id
		RETURNING
			c.id,
			c.workspace_id,
			COALESCE(c.connection_id, '00000000-0000-0000-0000-000000000000'::uuid),
			c.contact_id,
			COALESCE((
				SELECT ci.sender_identity
				FROM contact_identities ci
				WHERE ci.workspace_id = c.workspace_id
				  AND ci.contact_id = c.contact_id
				  AND ci.channel IN ('whatsapp_cloud', 'phone', 'whatsapp')
				ORDER BY (ci.channel = 'whatsapp_cloud') DESC, (ci.channel = 'phone') DESC, ci.created_at ASC
				LIMIT 1
			), ''),
			'whatsapp_cloud',
			COALESCE(NULLIF(c.entry_point_type, ''), 'standard'),
			c.service_window_expires_at
	`, nowUTC, horizonEnd, limit)
	if err != nil {
		return nil, fmt.Errorf("claim expiring chats: %w", err)
	}
	defer rows.Close()

	var out []ExpiringChat
	for rows.Next() {
		var ec ExpiringChat
		if err := rows.Scan(
			&ec.ChatID,
			&ec.WorkspaceID,
			&ec.ConnectionID,
			&ec.ContactID,
			&ec.RecipientPhone,
			&ec.Channel,
			&ec.EntryPointType,
			&ec.ExpiresAt,
		); err != nil {
			return nil, fmt.Errorf("scan expiring chat: %w", err)
		}
		out = append(out, ec)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// SetAI transitions the AI Engagement state of a Chat inside a locked transaction
// and executes post-commit intents (debounce cancellation and edge-triggered handoff event).
func (r *Recorder) SetAI(ctx context.Context, ws uuid.UUID, ref ChatRef, change AIChange) (domain.AIEngagement, error) {
	if r == nil || r.pool == nil {
		return domain.AIEngagement{}, errors.New("activity recorder pool is not configured")
	}
	if ws == uuid.Nil && ref.id == uuid.Nil {
		return domain.AIEngagement{}, ErrInvalidWorkspaceID
	}

	now := time.Now().UTC()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.AIEngagement{}, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Only ChatOf(conn, contact) may auto-create a Chat; ChatID and ChatAt do not auto-create.
	allowCreate := ref.contactID != uuid.Nil
	state, err := r.resolveAndLockChat(ctx, tx, ws, ref, allowCreate)
	if err != nil {
		return domain.AIEngagement{}, err
	}

	traceID := fmt.Sprintf("handoff-%s", uuid.New().String())
	nextState, _, intents := domain.Apply(state, domain.AIChangeActivity{
		At:          now,
		Mode:        change.mode,
		PausedUntil: change.until,
		TraceID:     traceID,
	})

	modeStr := string(nextState.AI.Mode)
	if modeStr == "" {
		modeStr = string(domain.AIModeActive)
	}
	statusStr := string(nextState.Status)
	if statusStr == "" {
		statusStr = string(domain.ChatStatusOpen)
	}
	_, err = tx.Exec(ctx, `
		UPDATE chats
		SET ai_mode = $3,
		    ai_paused_until = $4,
		    status = $5,
		    snoozed_until = $6,
		    updated_at = $7
		WHERE workspace_id = $1 AND id = $2
	`, state.WorkspaceID, state.ID, modeStr, nextState.AI.PausedUntil, statusStr, nextState.SnoozedUntil, now)
	if err != nil {
		return domain.AIEngagement{}, fmt.Errorf("update chat ai state: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.AIEngagement{}, fmt.Errorf("commit set_ai tx: %w", err)
	}

	r.executeIntents(ctx, intents)

	return nextState.AI.Effective(now), nil
}

// DispatchFailed marks an outbound ChatMessage's metadata status as "failed" after a queue publish error.
func (r *Recorder) DispatchFailed(ctx context.Context, ws uuid.UUID, uid string) error {
	if r == nil || r.pool == nil {
		return errors.New("activity recorder pool is not configured")
	}
	if ws == uuid.Nil {
		return ErrInvalidWorkspaceID
	}
	uid = strings.TrimSpace(uid)
	if uid == "" {
		return ErrInvalidUID
	}

	res, err := r.pool.Exec(ctx, `
		UPDATE chat_messages
		SET metadata = COALESCE(metadata, '{}'::jsonb) || '{"status":"failed"}'::jsonb
		WHERE workspace_id = $1 AND uid = $2
	`, ws, uid)
	if err != nil {
		return fmt.Errorf("mark dispatch failed: %w", err)
	}
	if res.RowsAffected() == 0 {
		return ErrChatMessageNotFound
	}
	return nil
}

// Snapshot reads the current ChatState for ref without mutating storage.
// When no Chat exists yet, Snapshot returns a default active ChatState and exists=false.
func (r *Recorder) Snapshot(ctx context.Context, ws uuid.UUID, ref ChatRef) (domain.ChatState, bool, error) {
	defaultState := domain.ChatState{
		WorkspaceID:    ws,
		Channel:        ref.channel,
		Status:         domain.ChatStatusOpen,
		AI:             domain.AIEngagement{Mode: domain.AIModeActive},
		EntryPointType: domain.EntryPointStandard,
	}
	if r == nil || r.pool == nil {
		return defaultState, false, nil
	}
	if ws == uuid.Nil && ref.id == uuid.Nil {
		return defaultState, false, ErrInvalidWorkspaceID
	}

	if ref.id != uuid.Nil {
		var row pgx.Row
		if ws != uuid.Nil {
			row = r.pool.QueryRow(ctx, `
				SELECT c.id, c.workspace_id, c.connection_id, c.contact_id, COALESCE(conn.channel, ''),
				       c.status, c.snoozed_until, c.ai_mode, c.ai_paused_until,
				       c.service_window_expires_at, COALESCE(c.entry_point_type, 'standard'),
				       c.unread_count, c.last_message_at, c.last_outbound_at
				FROM chats c
				LEFT JOIN connections conn ON conn.id = c.connection_id
				WHERE c.workspace_id = $1 AND c.id = $2
			`, ws, ref.id)
		} else {
			row = r.pool.QueryRow(ctx, `
				SELECT c.id, c.workspace_id, c.connection_id, c.contact_id, COALESCE(conn.channel, ''),
				       c.status, c.snoozed_until, c.ai_mode, c.ai_paused_until,
				       c.service_window_expires_at, COALESCE(c.entry_point_type, 'standard'),
				       c.unread_count, c.last_message_at, c.last_outbound_at
				FROM chats c
				LEFT JOIN connections conn ON conn.id = c.connection_id
				WHERE c.id = $1
			`, ref.id)
		}
		st, err := scanChatState(row)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return defaultState, false, nil
			}
			return defaultState, false, err
		}
		if st.Channel == "" && ref.channel != "" {
			st.Channel = ref.channel
		}
		return st, true, nil
	}

	channel := ref.channel
	if channel == "" && ref.connID != uuid.Nil {
		_ = r.pool.QueryRow(ctx, `SELECT channel FROM connections WHERE workspace_id = $1 AND id = $2`, ws, ref.connID).Scan(&channel)
	}
	defaultState.Channel = channel

	contactID := ref.contactID
	if contactID == uuid.Nil && ref.address != "" {
		contactID = lookupContactIDByAddress(ctx, r.pool, ws, channel, ref.address)
		if contactID == uuid.Nil {
			return defaultState, false, nil
		}
	}

	if contactID == uuid.Nil {
		return defaultState, false, nil
	}

	var connPtr *uuid.UUID
	if ref.connID != uuid.Nil {
		c := ref.connID
		connPtr = &c
	}

	row := r.pool.QueryRow(ctx, `
		SELECT c.id, c.workspace_id, c.connection_id, c.contact_id, COALESCE(conn.channel, ''),
		       c.status, c.snoozed_until, c.ai_mode, c.ai_paused_until,
		       c.service_window_expires_at, COALESCE(c.entry_point_type, 'standard'),
		       c.unread_count, c.last_message_at, c.last_outbound_at
		FROM chats c
		LEFT JOIN connections conn ON conn.id = c.connection_id
		WHERE c.workspace_id = $1 AND c.contact_id = $2
		  AND ($3::uuid IS NULL OR c.connection_id = $3 OR c.connection_id IS NULL)
		ORDER BY (c.connection_id = $3) DESC NULLS LAST, (c.connection_id IS NOT NULL) DESC, c.last_message_at DESC
		LIMIT 1
	`, ws, contactID, connPtr)

	st, err := scanChatState(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			defaultState.ContactID = contactID
			defaultState.ConnectionID = connPtr
			return defaultState, false, nil
		}
		return defaultState, false, err
	}
	if st.Channel == "" && channel != "" {
		st.Channel = channel
	}
	return st, true, nil
}

type rowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func lookupContactIDByAddress(ctx context.Context, q rowQuerier, ws uuid.UUID, channel, address string) uuid.UUID {
	address = strings.TrimSpace(address)
	if address == "" || ws == uuid.Nil {
		return uuid.Nil
	}
	var contactID uuid.UUID
	if channel != "" {
		_ = q.QueryRow(ctx, `
			SELECT contact_id FROM contact_identities
			WHERE workspace_id = $1 AND channel = $2
			  AND (sender_identity = $3 OR LTRIM(sender_identity, '+') = LTRIM($3, '+'))
			ORDER BY (sender_identity = $3) DESC
			LIMIT 1
		`, ws, channel, address).Scan(&contactID)
	}
	if contactID == uuid.Nil {
		_ = q.QueryRow(ctx, `
			SELECT contact_id FROM contact_identities
			WHERE workspace_id = $1
			  AND (sender_identity = $2 OR LTRIM(sender_identity, '+') = LTRIM($2, '+'))
			ORDER BY (sender_identity = $2) DESC
			LIMIT 1
		`, ws, address).Scan(&contactID)
	}
	return contactID
}

func (r *Recorder) resolveAndLockChat(ctx context.Context, tx pgx.Tx, ws uuid.UUID, ref ChatRef, allowCreate bool) (domain.ChatState, error) {
	if ref.id != uuid.Nil {
		var row pgx.Row
		if ws != uuid.Nil {
			row = tx.QueryRow(ctx, `
				SELECT id, workspace_id, connection_id, contact_id, ''::text, status, snoozed_until,
				       ai_mode, ai_paused_until, service_window_expires_at, COALESCE(entry_point_type, 'standard'),
				       unread_count, last_message_at, last_outbound_at
				FROM chats
				WHERE workspace_id = $1 AND id = $2
				FOR UPDATE
			`, ws, ref.id)
		} else {
			row = tx.QueryRow(ctx, `
				SELECT id, workspace_id, connection_id, contact_id, ''::text, status, snoozed_until,
				       ai_mode, ai_paused_until, service_window_expires_at, COALESCE(entry_point_type, 'standard'),
				       unread_count, last_message_at, last_outbound_at
				FROM chats
				WHERE id = $1
				FOR UPDATE
			`, ref.id)
		}
		st, err := scanChatState(row)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.ChatState{}, ErrChatNotFound
			}
			return domain.ChatState{}, fmt.Errorf("lock chat by id: %w", err)
		}
		if st.ConnectionID != nil && *st.ConnectionID != uuid.Nil {
			_ = tx.QueryRow(ctx, `SELECT channel FROM connections WHERE id = $1`, *st.ConnectionID).Scan(&st.Channel)
		}
		return st, nil
	}

	if ws == uuid.Nil {
		return domain.ChatState{}, ErrInvalidWorkspaceID
	}

	var connPtr *uuid.UUID
	var connChannel string
	if ref.connID != uuid.Nil {
		err := tx.QueryRow(ctx, `SELECT channel FROM connections WHERE id = $1`, ref.connID).Scan(&connChannel)
		if err == nil {
			c := ref.connID
			connPtr = &c
		}
	}

	channel := ref.channel
	if channel == "" {
		channel = connChannel
	}

	contactID := ref.contactID
	if contactID == uuid.Nil && ref.address != "" {
		// ChatAt is strictly read-only: look up existing contact without creating.
		allowCreate = false
		contactID = lookupContactIDByAddress(ctx, tx, ws, channel, ref.address)
	}

	if contactID == uuid.Nil {
		return domain.ChatState{}, ErrChatNotFound
	}

	// Try to find and lock an existing chat for (ws, connPtr, contactID)
	row := tx.QueryRow(ctx, `
		SELECT id, workspace_id, connection_id, contact_id, ''::text, status, snoozed_until,
		       ai_mode, ai_paused_until, service_window_expires_at, COALESCE(entry_point_type, 'standard'),
		       unread_count, last_message_at, last_outbound_at
		FROM chats
		WHERE workspace_id = $1 AND contact_id = $2
		  AND ($3::uuid IS NULL OR connection_id = $3 OR connection_id IS NULL)
		ORDER BY (connection_id = $3) DESC NULLS LAST, (connection_id IS NOT NULL) DESC, last_message_at DESC
		LIMIT 1
		FOR UPDATE
	`, ws, contactID, connPtr)

	st, err := scanChatState(row)
	if err == nil {
		if connPtr != nil && st.ConnectionID == nil {
			_, _ = tx.Exec(ctx, `
				UPDATE chats SET connection_id = $3, updated_at = NOW()
				WHERE id = $1 AND workspace_id = $2
				  AND NOT EXISTS (
				      SELECT 1 FROM chats WHERE workspace_id = $2 AND connection_id = $3 AND contact_id = $4
				  )
			`, st.ID, ws, *connPtr, contactID)
			st.ConnectionID = connPtr
		}
		st.Channel = channel
		if st.Channel == "" && st.ConnectionID != nil && *st.ConnectionID != uuid.Nil {
			_ = tx.QueryRow(ctx, `SELECT channel FROM connections WHERE id = $1`, *st.ConnectionID).Scan(&st.Channel)
		}
		return st, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return domain.ChatState{}, fmt.Errorf("lock chat by contact: %w", err)
	}
	if !allowCreate {
		return domain.ChatState{}, ErrChatNotFound
	}

	createRow := tx.QueryRow(ctx, `
		INSERT INTO chats (workspace_id, connection_id, contact_id, status, tags, unread_count, ai_mode, entry_point_type, metadata)
		VALUES ($1, $2, $3, 'open', '{}', 0, 'active', 'standard', '{}')
		ON CONFLICT (workspace_id, connection_id, contact_id)
		DO UPDATE SET updated_at = NOW()
		RETURNING id, workspace_id, connection_id, contact_id, ''::text, status, snoozed_until,
		          ai_mode, ai_paused_until, service_window_expires_at, COALESCE(entry_point_type, 'standard'),
		          unread_count, last_message_at, last_outbound_at
	`, ws, connPtr, contactID)

	st, err = scanChatState(createRow)
	if err != nil {
		return domain.ChatState{}, fmt.Errorf("create chat in tx: %w", err)
	}
	st.Channel = channel
	return st, nil
}

func scanChatState(row pgx.Row) (domain.ChatState, error) {
	var st domain.ChatState
	var aiMode string
	var aiPausedUntil *time.Time
	err := row.Scan(
		&st.ID,
		&st.WorkspaceID,
		&st.ConnectionID,
		&st.ContactID,
		&st.Channel,
		&st.Status,
		&st.SnoozedUntil,
		&aiMode,
		&aiPausedUntil,
		&st.ServiceWindowExpiresAt,
		&st.EntryPointType,
		&st.UnreadCount,
		&st.LastMessageAt,
		&st.LastOutboundAt,
	)
	if err != nil {
		return domain.ChatState{}, err
	}
	mode := domain.AIMode(aiMode)
	if mode == "" {
		mode = domain.AIModeActive
	}
	st.AI = domain.AIEngagement{
		Mode:        mode,
		PausedUntil: aiPausedUntil,
	}
	if st.EntryPointType == "" {
		st.EntryPointType = domain.EntryPointStandard
	}
	return st, nil
}

func loadMessageByUID(ctx context.Context, tx pgx.Tx, ws uuid.UUID, uid string) (*domain.ChatMessage, error) {
	var m domain.ChatMessage
	var rxBytes, metaBytes []byte
	err := tx.QueryRow(ctx, `
		SELECT id, chat_id, workspace_id, uid, direction, sender_type, sender_name, sender_id,
		       body, media_url, media_type, is_private, reactions, reply_to_uid, metadata, created_at
		FROM chat_messages
		WHERE workspace_id = $1 AND uid = $2
	`, ws, uid).Scan(
		&m.ID, &m.ChatID, &m.WorkspaceID, &m.UID, &m.Direction, &m.SenderType,
		&m.SenderName, &m.SenderID, &m.Body, &m.MediaURL, &m.MediaType, &m.IsPrivate,
		&rxBytes, &m.ReplyToUID, &metaBytes, &m.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	if len(rxBytes) > 0 {
		_ = json.Unmarshal(rxBytes, &m.Reactions)
	}
	if m.Reactions == nil {
		m.Reactions = []domain.Reaction{}
	}
	if len(metaBytes) > 0 {
		_ = json.Unmarshal(metaBytes, &m.Metadata)
	}
	if m.Metadata == nil {
		m.Metadata = make(map[string]interface{})
	}
	return &m, nil
}

func insertChatMessage(ctx context.Context, tx pgx.Tx, msg *domain.ChatMessage) error {
	if msg.ID == uuid.Nil {
		msg.ID = uuid.New()
	}
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now().UTC()
	}
	if msg.Reactions == nil {
		msg.Reactions = []domain.Reaction{}
	}
	if msg.Metadata == nil {
		msg.Metadata = make(map[string]interface{})
	}

	rxJSON, err := json.Marshal(msg.Reactions)
	if err != nil {
		return fmt.Errorf("marshal reactions: %w", err)
	}
	metaJSON, err := json.Marshal(msg.Metadata)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO chat_messages (
			id, chat_id, workspace_id, uid, direction, sender_type, sender_name, sender_id,
			body, media_url, media_type, is_private, reactions, reply_to_uid, metadata, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8,
			$9, $10, $11, $12, $13, $14, $15, $16
		)
	`, msg.ID, msg.ChatID, msg.WorkspaceID, msg.UID, msg.Direction, msg.SenderType, msg.SenderName, msg.SenderID,
		msg.Body, msg.MediaURL, msg.MediaType, msg.IsPrivate, rxJSON, msg.ReplyToUID, metaJSON, msg.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert chat message: %w", err)
	}
	return nil
}

func updateInboundChatState(ctx context.Context, tx pgx.Tx, state domain.ChatState, now time.Time) error {
	modeStr := string(state.AI.Mode)
	if modeStr == "" {
		modeStr = string(domain.AIModeActive)
	}
	lastMsgAt := state.LastMessageAt
	if lastMsgAt.IsZero() {
		lastMsgAt = now
	}
	ep := domain.NormalizeEntryPointType(state.EntryPointType)

	_, err := tx.Exec(ctx, `
		UPDATE chats
		SET status = $3,
		    snoozed_until = $4,
		    ai_mode = $5,
		    ai_paused_until = $6,
		    unread_count = $7,
		    service_window_expires_at = $8,
		    entry_point_type = $9,
		    notified_expiring_at = NULL,
		    last_message_at = $10,
		    updated_at = $11
		WHERE workspace_id = $1 AND id = $2
	`, state.WorkspaceID, state.ID, string(state.Status), state.SnoozedUntil, modeStr, state.AI.PausedUntil, state.UnreadCount, state.ServiceWindowExpiresAt, ep, lastMsgAt, now)
	if err != nil {
		return fmt.Errorf("update inbound chat state: %w", err)
	}
	return nil
}

func updateOutboundChatState(ctx context.Context, tx pgx.Tx, state domain.ChatState, now time.Time) error {
	modeStr := string(state.AI.Mode)
	if modeStr == "" {
		modeStr = string(domain.AIModeActive)
	}
	lastMsgAt := state.LastMessageAt
	if lastMsgAt.IsZero() {
		lastMsgAt = now
	}
	outAt := now
	if state.LastOutboundAt != nil && !state.LastOutboundAt.IsZero() {
		outAt = *state.LastOutboundAt
	}

	_, err := tx.Exec(ctx, `
		UPDATE chats
		SET status = $3,
		    snoozed_until = $4,
		    ai_mode = $5,
		    ai_paused_until = $6,
		    unread_count = $7,
		    last_outbound_at = GREATEST(COALESCE(last_outbound_at, $8), $8),
		    last_message_at = $9,
		    updated_at = $10
		WHERE workspace_id = $1 AND id = $2
	`, state.WorkspaceID, state.ID, string(state.Status), state.SnoozedUntil, modeStr, state.AI.PausedUntil, state.UnreadCount, outAt, lastMsgAt, now)
	if err != nil {
		return fmt.Errorf("update outbound chat state: %w", err)
	}
	return nil
}


func (r *Recorder) executeIntents(ctx context.Context, intents []domain.Intent) {
	for _, in := range intents {
		switch in.Kind {
		case domain.IntentCancelDebounce:
			if r.deb != nil && in.WorkspaceID != uuid.Nil && in.ChatID != uuid.Nil {
				if err := r.deb.Cancel(ctx, in.WorkspaceID, in.ChatID); err != nil && r.log != nil {
					r.log.Warn("failed to cancel debounce buffer", "workspace_id", in.WorkspaceID, "chat_id", in.ChatID, "error", err)
				}
			}
		case domain.IntentPublishHandoff:
			if r.pub != nil && in.WorkspaceID != uuid.Nil {
				payload, err := json.Marshal(map[string]any{
					"event":        "chat.handoff.human_takeover",
					"workspace_id": in.WorkspaceID.String(),
					"chat_id":      in.ChatID.String(),
					"contact_id":   in.ContactID.String(),
					"sender_name":  in.SenderName,
					"trace_id":     in.TraceID,
					"timestamp":    in.At.UTC().Format(time.RFC3339),
				})
				if err == nil {
					_ = r.pub.Publish(ctx, "chat.handoff.human_takeover", payload, in.TraceID)
					_ = r.pub.Publish(ctx, fmt.Sprintf("chat.handoff.%s", in.WorkspaceID.String()), payload, in.TraceID)
				}
			}
		}
	}
}
