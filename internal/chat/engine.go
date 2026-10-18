package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pablojhp.pergo/internal/chat/activity"
	"github.com/pablojhp.pergo/internal/domain"
	"github.com/pablojhp.pergo/internal/outbound"
	"github.com/pablojhp.pergo/internal/platform/postgres/tenant"
	"github.com/pablojhp.pergo/internal/repository"
)

var (
	// ErrChatNotFound is returned when a chat cannot be found within the workspace scope.
	ErrChatNotFound = errors.New("chat not found")
	// ErrMessageNotFound is returned when a chat message cannot be found within the workspace scope.
	ErrMessageNotFound = errors.New("message not found")
	// ErrChatMismatch is returned when a message does not belong to the specified chat thread.
	ErrChatMismatch = errors.New("message does not belong to the specified chat thread")
	// ErrInvalidStatus is returned when a status transition uses an unsupported status value.
	ErrInvalidStatus = errors.New("invalid chat status: must be 'open', 'pending', 'snoozed', or 'closed'")
	// ErrInvalidMember is returned when assigning a chat to an email not in the workspace team.
	ErrInvalidMember = errors.New("teammate email not found in workspace")
	// ErrEmptyBody is returned when a message or note has no body, media, or template.
	ErrEmptyBody = errors.New("message body, media, or template_name is required")
	// ErrContactNotFound is returned when the chat's contact cannot be resolved.
	ErrContactNotFound = errors.New("associated contact could not be resolved")
	// ErrServiceWindowExpired is returned when a free-form WABA message is sent outside the 24h window.
	ErrServiceWindowExpired = errors.New("Meta WhatsApp 24-hour customer service window expired. A pre-approved template message is required.")
)

// ChatRepository defines the chat persistence operations required by Engine.
type ChatRepository interface {
	GetChat(ctx context.Context, workspaceID, chatID uuid.UUID) (*domain.Chat, error)
	ListChats(ctx context.Context, workspaceID uuid.UUID, filter domain.ChatFilter, limit, offset int) ([]domain.Chat, error)
	ListChatMessages(ctx context.Context, workspaceID, chatID uuid.UUID, beforeUID, afterUID string, limit int) ([]domain.ChatMessage, error)
	CreateInternalNote(ctx context.Context, workspaceID, chatID uuid.UUID, authorName, senderType, body string) (*domain.ChatMessage, error)
	AssignChat(ctx context.Context, workspaceID, chatID uuid.UUID, memberID *uuid.UUID, email *string) error
	SetChatTags(ctx context.Context, workspaceID, chatID uuid.UUID, tags []string) error
	UpdateChatStatus(ctx context.Context, workspaceID, chatID uuid.UUID, status string) error
	SnoozeChat(ctx context.Context, workspaceID, chatID uuid.UUID, snoozedUntil *time.Time) error
	GetChatMessageByUID(ctx context.Context, workspaceID uuid.UUID, uid string) (*domain.ChatMessage, error)
	GetReactions(ctx context.Context, workspaceID uuid.UUID, messageUID string) ([]domain.Reaction, error)
	MutateReaction(ctx context.Context, workspaceID uuid.UUID, messageUID, emoji, sender, action string) (uuid.UUID, string, []domain.Reaction, error)
	AddReaction(ctx context.Context, workspaceID uuid.UUID, messageUID string, reaction domain.Reaction) ([]domain.Reaction, error)
	RemoveReaction(ctx context.Context, workspaceID uuid.UUID, messageUID string, emoji, sender string) ([]domain.Reaction, error)
}

// Optional extensions implemented by *repository.ChatRepository.
type chatByContactGetter interface {
	GetChatByContact(ctx context.Context, workspaceID, contactID uuid.UUID) (*domain.Chat, error)
}

type chatFinderCreator interface {
	FindOrCreateChat(ctx context.Context, workspaceID uuid.UUID, connectionID *uuid.UUID, contactID uuid.UUID) (*domain.Chat, error)
}

type chatByIDGetter interface {
	GetChatByID(ctx context.Context, chatID uuid.UUID) (*domain.Chat, error)
}

type messageByUIDFinder interface {
	FindMessageByUID(ctx context.Context, uid string) (*domain.ChatMessage, error)
}

type conversationLister interface {
	ListConversations(ctx context.Context, workspaceID uuid.UUID, channelFilter string) ([]repository.ConversationSummary, error)
}

type threadLister interface {
	ListThreadByContact(ctx context.Context, workspaceID, contactID uuid.UUID, afterID *uuid.UUID) ([]repository.ThreadMessage, error)
}

type chatUnreadResetter interface {
	ResetUnread(ctx context.Context, workspaceID, chatID uuid.UUID) error
}

type messageMetadataUpdater interface {
	UpdateMessageMetadata(ctx context.Context, workspaceID uuid.UUID, uid string, metadata map[string]interface{}) error
}

type poolProvider interface {
	Pool() *pgxpool.Pool
}

// ContactRepository defines contact operations required by Engine.
type ContactRepository interface {
	GetByID(ctx context.Context, workspaceID, contactID uuid.UUID) (*domain.Contact, error)
	SetTags(ctx context.Context, workspaceID, contactID uuid.UUID, tags []string) error
	MarkAsRead(ctx context.Context, workspaceID, contactID uuid.UUID) error
}

type contactsByIDsGetter interface {
	GetByIDs(ctx context.Context, workspaceID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]*domain.Contact, error)
}

type contactResolver interface {
	ResolveContact(ctx context.Context, workspaceID uuid.UUID, channel, senderIdentity, name, username, phone string) (*domain.Contact, error)
}

// ConnectionRepository defines connection operations required by Engine.
type ConnectionRepository interface {
	GetByID(ctx context.Context, id uuid.UUID) (*repository.Connection, error)
	GetDefaultChannelConnection(ctx context.Context, workspaceID uuid.UUID, channel string) (*repository.Connection, error)
	GetBySenderIdentity(ctx context.Context, workspaceID uuid.UUID, senderIdentity string) (*repository.Connection, error)
	ListByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]*repository.Connection, error)
}

// WorkspaceRepository defines workspace member validation operations.
type WorkspaceRepository interface {
	GetMemberByEmail(ctx context.Context, workspaceID uuid.UUID, email string) (*repository.WorkspaceMember, error)
}

// AuditRepository defines audit logging operations.
type AuditRepository interface {
	InsertAuditLog(ctx context.Context, entry *repository.AuditEntry) error
}

// MessagePublisher defines NATS JetStream publish operations.
type MessagePublisher interface {
	Publish(ctx context.Context, subject string, data []byte, traceID string) error
}

// ReactionWebhookDispatcher defines reaction webhook dispatch operations.
type ReactionWebhookDispatcher interface {
	DispatchReaction(ctx context.Context, workspaceID uuid.UUID, messageUID string, payload []byte, traceID string) error
}

// DebounceCanceller cancels any pending inbound debounce buffer for a chat.
type DebounceCanceller interface {
	Cancel(ctx context.Context, workspaceID, chatID uuid.UUID) error
}

// ChatRef polymorphically identifies a chat thread within a workspace.
type ChatRef struct {
	ChatID       uuid.UUID
	ContactID    uuid.UUID
	ConnectionID *uuid.UUID
	MessageUID   string
	Channel      string
	Phone        string
}

// ChatSummary wraps domain.Chat with enriched contact summary attributes.
type ChatSummary struct {
	domain.Chat
	ContactName  string   `json:"contact_name,omitempty"`
	ContactPhone string   `json:"contact_phone,omitempty"`
	Identities   []string `json:"identities,omitempty"`
}

// GetChatResult encapsulates a resolved chat with its contact, connection, and optional messages.
type GetChatResult struct {
	Chat       *domain.Chat           `json:"chat"`
	Contact    *domain.Contact        `json:"contact,omitempty"`
	Connection *repository.Connection `json:"connection,omitempty"`
	Messages   []domain.ChatMessage   `json:"messages,omitempty"`
}

// StatusUpdate describes a chat status transition.
type StatusUpdate struct {
	Status       string
	SnoozedUntil *time.Time
}

// TagMutation describes an atomic tag addition, removal, or full replacement.
type TagMutation struct {
	Add    []string
	Remove []string
	Set    []string
}

// SendRequest describes an outbound message dispatch request on a chat thread.
type SendRequest struct {
	Ref              ChatRef
	Body             string
	Media            *domain.Media
	SenderName       string
	SenderType       string
	ReplyToUID       *string
	TemplateName     string
	Language         string
	Components       []domain.TemplateComponent
	Interactive      *domain.Interactive
	ChannelOverrides map[string]json.RawMessage
	FallbackChannels []string
	FallbackBehavior string
	Type             string
	Product          *domain.ProductPayload
	ConnectionID     *uuid.UUID
	SenderIdentity   string
	TraceIDPrefix    string
	AuditEventType   string
	Metadata         map[string]string
}

// SendResult contains the result of a conversational outbound send.
type SendResult struct {
	TraceID      string              `json:"trace_id"`
	Status       string              `json:"status"`
	QueuedAt     time.Time           `json:"queued_at"`
	ChatID       uuid.UUID           `json:"chat_id"`
	ContactID    uuid.UUID           `json:"contact_id"`
	ConnectionID uuid.UUID           `json:"connection_id,omitempty"`
	Channel      string              `json:"channel,omitempty"`
	From         string              `json:"from,omitempty"`
	To           string              `json:"to,omitempty"`
	Body         string              `json:"body,omitempty"`
	ReplyToUID   *string             `json:"reply_to_uid,omitempty"`
	AIDisabled   bool                `json:"ai_disabled"`
	MessageID    uuid.UUID           `json:"message_id"`
	Message      *domain.ChatMessage `json:"message,omitempty"`
}

// NoteRequest describes an internal note creation request.
type NoteRequest struct {
	Ref        ChatRef
	Body       string
	AuthorName string
	SenderType string
}

// ReactRequest describes a message reaction mutation request.
type ReactRequest struct {
	ChatRef    ChatRef
	MessageUID string
	Emoji      string
	Action     string
	Sender     string
}

// ReactResult contains the result of a reaction mutation.
type ReactResult struct {
	ChatID     uuid.UUID         `json:"chat_id"`
	MessageUID string            `json:"message_uid"`
	Action     string            `json:"action"`
	Emoji      string            `json:"emoji"`
	Sender     string            `json:"sender"`
	Reactions  []domain.Reaction `json:"reactions"`
}

func isNilInterface(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}

// Option configures optional dependencies on Engine.
type Option func(*Engine)

// WithContactRepo configures the contact repository.
func WithContactRepo(r ContactRepository) Option {
	return func(e *Engine) {
		if !isNilInterface(r) {
			e.contactRepo = r
		}
	}
}

// WithConnectionRepo configures the connection repository.
func WithConnectionRepo(r ConnectionRepository) Option {
	return func(e *Engine) {
		if !isNilInterface(r) {
			e.connRepo = r
		}
	}
}

// WithWorkspaceRepo configures the workspace repository.
func WithWorkspaceRepo(r WorkspaceRepository) Option {
	return func(e *Engine) {
		if !isNilInterface(r) {
			e.wsRepo = r
		}
	}
}

// WithAuditRepo configures the audit repository.
func WithAuditRepo(r AuditRepository) Option {
	return func(e *Engine) {
		if !isNilInterface(r) {
			e.auditRepo = r
		}
	}
}

// WithMessagePublisher configures the NATS message publisher.
func WithMessagePublisher(p MessagePublisher) Option {
	return func(e *Engine) {
		if !isNilInterface(p) {
			e.publisher = p
		}
	}
}

// WithReactionWebhookDispatcher configures the reaction webhook dispatcher.
func WithReactionWebhookDispatcher(d ReactionWebhookDispatcher) Option {
	return func(e *Engine) {
		if !isNilInterface(d) {
			e.webhookDispatcher = d
		}
	}
}

// WithMessageIngestor configures the unified outbound message ingestor.
func WithMessageIngestor(i outbound.OutboundProcessor) Option {
	return func(e *Engine) {
		if !isNilInterface(i) {
			e.ingestor = i
		}
	}
}

// WithDebounceCanceller configures the inbound debounce buffer canceller.
func WithDebounceCanceller(d DebounceCanceller) Option {
	return func(e *Engine) {
		if !isNilInterface(d) {
			e.debouncer = d
		}
	}
}

// WithActivity configures the Chat Activity recorder.
func WithActivity(r *activity.Recorder) Option {
	return func(e *Engine) {
		if !isNilInterface(r) {
			e.activity = r
		}
	}
}

// Config groups dependencies used to construct a Chat Engine.
type Config struct {
	ChatRepo          ChatRepository
	ContactRepo       ContactRepository
	ConnRepo          ConnectionRepository
	WorkspaceRepo     WorkspaceRepository
	AuditRepo         AuditRepository
	Publisher         MessagePublisher
	WebhookDispatcher ReactionWebhookDispatcher
	Ingestor          outbound.OutboundProcessor
	Debouncer         DebounceCanceller
	Activity          *activity.Recorder
}

// NewEngineFromConfig constructs a Chat Engine from a Config struct, safely ignoring nil or typed-nil fields.
func NewEngineFromConfig(cfg Config) *Engine {
	return NewEngine(
		cfg.ChatRepo,
		WithContactRepo(cfg.ContactRepo),
		WithConnectionRepo(cfg.ConnRepo),
		WithWorkspaceRepo(cfg.WorkspaceRepo),
		WithAuditRepo(cfg.AuditRepo),
		WithMessagePublisher(cfg.Publisher),
		WithReactionWebhookDispatcher(cfg.WebhookDispatcher),
		WithMessageIngestor(cfg.Ingestor),
		WithDebounceCanceller(cfg.Debouncer),
		WithActivity(cfg.Activity),
	)
}

// Engine is the deep domain module owning chat resolution, lazy snooze reopen,
// triage mutations, note/draft management, reactions, summarization, and
// conversational outbound sends across REST and MCP.
type Engine struct {
	chatRepo          ChatRepository
	contactRepo       ContactRepository
	connRepo          ConnectionRepository
	wsRepo            WorkspaceRepository
	auditRepo         AuditRepository
	publisher         MessagePublisher
	webhookDispatcher ReactionWebhookDispatcher
	ingestor          outbound.OutboundProcessor
	debouncer         DebounceCanceller
	activity          *activity.Recorder
}

// NewEngine creates a new Chat Engine with the given ChatRepository and options.
func NewEngine(chatRepo ChatRepository, opts ...Option) *Engine {
	var repo ChatRepository
	if !isNilInterface(chatRepo) {
		repo = chatRepo
	}
	e := &Engine{
		chatRepo: repo,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(e)
		}
	}
	return e
}

// SetWebhookDispatcher updates the reaction webhook dispatcher on the Engine.
func (e *Engine) SetWebhookDispatcher(d ReactionWebhookDispatcher) {
	if isNilInterface(d) {
		e.webhookDispatcher = nil
		return
	}
	e.webhookDispatcher = d
}

// SetMessagePublisher updates the NATS message publisher on the Engine.
func (e *Engine) SetMessagePublisher(p MessagePublisher) {
	if isNilInterface(p) {
		e.publisher = nil
		return
	}
	e.publisher = p
}

// SetMessageIngestor updates the outbound processor on the Engine.
func (e *Engine) SetMessageIngestor(i outbound.OutboundProcessor) {
	e.ingestor = i
}

// SetDebounceCanceller updates the debounce canceller on the Engine.
func (e *Engine) SetDebounceCanceller(d DebounceCanceller) {
	e.debouncer = d
}

// SetActivity updates the Chat Activity recorder on the Engine.
func (e *Engine) SetActivity(r *activity.Recorder) {
	e.activity = r
}

func (e *Engine) ensureActivity() *activity.Recorder {
	if e.activity != nil {
		return e.activity
	}
	if e.chatRepo != nil {
		if pp, ok := e.chatRepo.(poolProvider); ok && pp.Pool() != nil {
			e.activity = activity.New(pp.Pool(), e.publisher, e.debouncer, slog.Default())
			return e.activity
		}
	}
	return nil
}

// EnforceWorkspaceScope checks that the given workspaceID matches any authenticated tenant context.
func EnforceWorkspaceScope(ctx context.Context, workspaceID uuid.UUID) (uuid.UUID, error) {
	var authWS uuid.UUID
	if scope, err := domain.Require(ctx); err == nil && scope.WorkspaceID() != uuid.Nil {
		authWS = scope.WorkspaceID()
	} else if id, ok := tenant.WorkspaceIDFrom(ctx); ok && id != uuid.Nil {
		authWS = id
	}
	if authWS != uuid.Nil {
		if workspaceID != uuid.Nil && workspaceID != authWS {
			return uuid.Nil, errors.New("workspace_id does not match authenticated context")
		}
		return authWS, nil
	}
	return workspaceID, nil
}

func deriveEffectiveStatus(c *domain.Chat) *domain.Chat {
	if c == nil {
		return nil
	}
	cp := *c
	cp.NormalizeEffectiveStatus(time.Now().UTC())
	return &cp
}

// Window evaluates the customer service window for a Chat by reading activity.Snapshot
// (when an activity Recorder is available) and passing the result to domain.Window(state, exists, now).
func (e *Engine) Window(ctx context.Context, workspaceID uuid.UUID, c *domain.Chat, channel string, now time.Time) domain.ServiceWindow {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	wsID, err := EnforceWorkspaceScope(ctx, workspaceID)
	if err != nil || wsID == uuid.Nil {
		if c != nil && c.WorkspaceID != uuid.Nil {
			wsID = c.WorkspaceID
		} else {
			wsID = workspaceID
		}
	}
	if rec := e.ensureActivity(); rec != nil && wsID != uuid.Nil {
		if c != nil && c.ID != uuid.Nil {
			ref := activity.ChatID(c.ID).WithChannel(channel)
			if state, exists, sErr := rec.Snapshot(ctx, wsID, ref); sErr == nil {
				if state.Channel == "" && channel != "" {
					state.Channel = channel
				}
				return domain.Window(state, exists, now)
			}
		} else {
			return domain.Window(domain.ChatState{WorkspaceID: wsID, Channel: channel}, false, now)
		}
	}
	if c == nil {
		return domain.Window(domain.ChatState{WorkspaceID: wsID, Channel: channel}, false, now)
	}
	state := domain.ChatState{
		ID:                     c.ID,
		WorkspaceID:            c.WorkspaceID,
		ConnectionID:           c.ConnectionID,
		ContactID:              c.ContactID,
		Channel:                channel,
		Status:                 c.Status,
		SnoozedUntil:           c.SnoozedUntil,
		AI:                     c.AI,
		ServiceWindowExpiresAt: c.ServiceWindowExpiresAt,
		EntryPointType:         domain.NormalizeEntryPointType(c.EntryPointType),
		UnreadCount:            c.UnreadCount,
		LastMessageAt:          c.LastMessageAt,
		LastInboundAt:          c.LastInboundAt,
		LastOutboundAt:         c.LastOutboundAt,
	}
	return domain.Window(state, true, now)
}

// ResolveChat resolves a ChatRef (by ChatID, ContactID, MessageUID, or Channel+Phone)
// strictly scoped to workspaceID.
func (e *Engine) ResolveChat(ctx context.Context, workspaceID uuid.UUID, ref ChatRef) (*domain.Chat, error) {
	if e.chatRepo == nil {
		return nil, errors.New("chat repository not configured")
	}

	wsID, err := EnforceWorkspaceScope(ctx, workspaceID)
	if err != nil {
		return nil, ErrChatNotFound
	}

	// 1. Resolve by ChatID
	if ref.ChatID != uuid.Nil {
		if wsID == uuid.Nil {
			if byID, ok := e.chatRepo.(chatByIDGetter); ok {
				c, gErr := byID.GetChatByID(ctx, ref.ChatID)
				if gErr != nil {
					if errors.Is(gErr, repository.ErrChatNotFound) || errors.Is(gErr, ErrChatNotFound) {
						return nil, ErrChatNotFound
					}
					return nil, gErr
				}
				if c == nil {
					return nil, ErrChatNotFound
				}
				if _, sErr := EnforceWorkspaceScope(ctx, c.WorkspaceID); sErr != nil {
					return nil, ErrChatNotFound
				}
				return deriveEffectiveStatus(c), nil
			}
			return nil, ErrChatNotFound
		}
		c, gErr := e.chatRepo.GetChat(ctx, wsID, ref.ChatID)
		if gErr != nil {
			if errors.Is(gErr, repository.ErrChatNotFound) || errors.Is(gErr, ErrChatNotFound) {
				return nil, ErrChatNotFound
			}
			return nil, gErr
		}
		if c == nil || c.WorkspaceID != wsID {
			return nil, ErrChatNotFound
		}
		return deriveEffectiveStatus(c), nil
	}

	// 2. Resolve by ContactID
	if ref.ContactID != uuid.Nil && wsID != uuid.Nil {
		wantConn := ref.ConnectionID != nil && *ref.ConnectionID != uuid.Nil
		if !wantConn {
			if byContact, ok := e.chatRepo.(chatByContactGetter); ok {
				c, gErr := byContact.GetChatByContact(ctx, wsID, ref.ContactID)
				if gErr != nil && !errors.Is(gErr, repository.ErrChatNotFound) && !errors.Is(gErr, ErrChatNotFound) {
					return nil, gErr
				}
				if gErr == nil && c != nil && c.WorkspaceID == wsID {
					return deriveEffectiveStatus(c), nil
				}
			}
		}
		contactID := ref.ContactID
		filter := domain.ChatFilter{ContactID: &contactID}
		if wantConn {
			filter.ConnectionID = ref.ConnectionID
		}
		chats, lErr := e.chatRepo.ListChats(ctx, wsID, filter, 20, 0)
		if lErr != nil {
			return nil, lErr
		}
		for i := range chats {
			if chats[i].WorkspaceID != wsID || chats[i].ContactID != ref.ContactID {
				continue
			}
			if wantConn && (chats[i].ConnectionID == nil || *chats[i].ConnectionID != *ref.ConnectionID) {
				continue
			}
			return deriveEffectiveStatus(&chats[i]), nil
		}
		if wantConn {
			if byContact, ok := e.chatRepo.(chatByContactGetter); ok {
				c, gErr := byContact.GetChatByContact(ctx, wsID, ref.ContactID)
				if gErr != nil && !errors.Is(gErr, repository.ErrChatNotFound) && !errors.Is(gErr, ErrChatNotFound) {
					return nil, gErr
				}
				if gErr == nil && c != nil && c.WorkspaceID == wsID {
					if c.ConnectionID != nil && *c.ConnectionID == *ref.ConnectionID {
						return deriveEffectiveStatus(c), nil
					}
				}
			}
		}
		return nil, ErrChatNotFound
	}

	// 3. Resolve by MessageUID
	if uid := strings.TrimSpace(ref.MessageUID); uid != "" {
		var msg *domain.ChatMessage
		var mErr error
		if wsID != uuid.Nil {
			msg, mErr = e.chatRepo.GetChatMessageByUID(ctx, wsID, uid)
		} else if finder, ok := e.chatRepo.(messageByUIDFinder); ok {
			msg, mErr = finder.FindMessageByUID(ctx, uid)
		}
		if mErr != nil && !errors.Is(mErr, repository.ErrChatMessageNotFound) && !errors.Is(mErr, ErrMessageNotFound) {
			return nil, mErr
		}
		if msg == nil || msg.ChatID == uuid.Nil {
			return nil, ErrChatNotFound
		}
		if wsID != uuid.Nil && msg.WorkspaceID != wsID {
			return nil, ErrChatNotFound
		}
		if _, sErr := EnforceWorkspaceScope(ctx, msg.WorkspaceID); sErr != nil {
			return nil, ErrChatNotFound
		}
		c, gErr := e.chatRepo.GetChat(ctx, msg.WorkspaceID, msg.ChatID)
		if gErr != nil {
			if errors.Is(gErr, repository.ErrChatNotFound) || errors.Is(gErr, ErrChatNotFound) {
				return nil, ErrChatNotFound
			}
			return nil, gErr
		}
		if c == nil {
			return nil, ErrChatNotFound
		}
		return deriveEffectiveStatus(c), nil
	}

	// 4. Resolve by Channel + Phone
	if phone := strings.TrimSpace(ref.Phone); phone != "" && wsID != uuid.Nil {
		channel := strings.ToLower(strings.TrimSpace(ref.Channel))
		wantConn := ref.ConnectionID != nil && *ref.ConnectionID != uuid.Nil
		filter := domain.ChatFilter{
			Channel: channel,
			Phone:   phone,
		}
		if wantConn {
			filter.ConnectionID = ref.ConnectionID
		}
		chats, lErr := e.chatRepo.ListChats(ctx, wsID, filter, 50, 0)
		if lErr != nil {
			return nil, lErr
		}
		for i := range chats {
			c := chats[i]
			if c.WorkspaceID != wsID {
				continue
			}
			if wantConn && (c.ConnectionID == nil || *c.ConnectionID != *ref.ConnectionID) {
				continue
			}
			if e.contactRepo == nil {
				return deriveEffectiveStatus(&c), nil
			}
			contact, cErr := e.contactRepo.GetByID(ctx, wsID, c.ContactID)
			if cErr == nil && contact != nil {
				if matchesContactPhone(contact, channel, phone) {
					return deriveEffectiveStatus(&c), nil
				}
			}
		}
	}

	return nil, ErrChatNotFound
}

func matchesContactPhone(contact *domain.Contact, channel, phone string) bool {
	if contact == nil {
		return false
	}
	cleanPhone := strings.TrimPrefix(strings.TrimSpace(phone), "+")
	for _, id := range contact.Identities {
		if channel != "" && !strings.EqualFold(id.Channel, channel) {
			continue
		}
		cleanIdent := strings.TrimPrefix(strings.TrimSpace(id.SenderIdentity), "+")
		if strings.EqualFold(id.SenderIdentity, phone) || (cleanPhone != "" && cleanIdent == cleanPhone) {
			return true
		}
	}
	return false
}

// EnsureChatForContact resolves or creates the chat thread for a contact within workspaceID,
// derives effective snooze status, and optionally resets unread count on that chat.
func (e *Engine) EnsureChatForContact(ctx context.Context, workspaceID, contactID uuid.UUID, markRead bool) (*domain.Chat, error) {
	if contactID == uuid.Nil {
		return nil, ErrChatNotFound
	}
	wsID, err := EnforceWorkspaceScope(ctx, workspaceID)
	if err != nil || wsID == uuid.Nil {
		return nil, ErrChatNotFound
	}

	c, err := e.ResolveChat(ctx, wsID, ChatRef{ContactID: contactID})
	if err != nil {
		if !errors.Is(err, ErrChatNotFound) {
			return nil, err
		}
		if e.contactRepo != nil {
			contact, cErr := e.contactRepo.GetByID(ctx, wsID, contactID)
			if cErr != nil || contact == nil {
				return nil, ErrChatNotFound
			}
		}
		creator, ok := e.chatRepo.(chatFinderCreator)
		if !ok {
			return nil, ErrChatNotFound
		}
		var connID *uuid.UUID
		if e.contactRepo != nil && e.connRepo != nil {
			if contact, cErr := e.contactRepo.GetByID(ctx, wsID, contactID); cErr == nil && contact != nil {
				for _, id := range contact.Identities {
					if conn, connErr := e.connRepo.GetDefaultChannelConnection(ctx, wsID, id.Channel); connErr == nil && conn != nil {
						connID = &conn.ID
						break
					}
				}
			}
		}
		c, err = creator.FindOrCreateChat(ctx, wsID, connID, contactID)
		if err != nil || c == nil {
			return nil, ErrChatNotFound
		}
		c = deriveEffectiveStatus(c)
	}

	if markRead {
		_ = e.resetChatUnread(ctx, c.WorkspaceID, c.ID, c.ContactID)
		c.UnreadCount = 0
	}

	return c, nil
}

// resetChatUnread resets unread_count = 0 on the specified chatID and updates the contact's last_read_at timestamp.
func (e *Engine) resetChatUnread(ctx context.Context, workspaceID, chatID, contactID uuid.UUID) error {
	if chatID == uuid.Nil {
		return nil
	}
	wsID, err := EnforceWorkspaceScope(ctx, workspaceID)
	if err != nil || wsID == uuid.Nil {
		return err
	}
	var resetErr error
	if e.chatRepo != nil {
		if resetter, ok := e.chatRepo.(chatUnreadResetter); ok {
			resetErr = resetter.ResetUnread(ctx, wsID, chatID)
		}
	}
	if e.contactRepo != nil && contactID != uuid.Nil {
		if mErr := e.contactRepo.MarkAsRead(ctx, wsID, contactID); mErr != nil && resetErr == nil {
			resetErr = mErr
		}
	}
	return resetErr
}

// ListConversations returns all active conversation summaries for a workspace in a single SQL query
// against chats and chat_messages, deriving effective status for expired snoozes without mutating storage.
func (e *Engine) ListConversations(ctx context.Context, workspaceID uuid.UUID, channelFilter string) ([]repository.ConversationSummary, error) {
	if e.chatRepo == nil {
		return nil, nil
	}
	wsID, err := EnforceWorkspaceScope(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if wsID == uuid.Nil {
		return nil, nil
	}
	lister, ok := e.chatRepo.(conversationLister)
	if !ok {
		return nil, nil
	}
	return lister.ListConversations(ctx, wsID, channelFilter)
}

// ListThread returns chronological ThreadMessage rows for a contact in a workspace,
// optionally resetting unread_count = 0 on the resolved chat when markRead is true.
func (e *Engine) ListThread(ctx context.Context, workspaceID, contactID uuid.UUID, afterID *uuid.UUID, markRead bool) ([]repository.ThreadMessage, error) {
	if e.chatRepo == nil || contactID == uuid.Nil {
		return nil, nil
	}
	wsID, err := EnforceWorkspaceScope(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if wsID == uuid.Nil {
		return nil, nil
	}
	resolvedChat, rErr := e.ResolveChat(ctx, wsID, ChatRef{ContactID: contactID})
	if rErr != nil && !errors.Is(rErr, ErrChatNotFound) {
		return nil, rErr
	}
	lister, ok := e.chatRepo.(threadLister)
	if !ok {
		return nil, nil
	}
	msgs, err := lister.ListThreadByContact(ctx, wsID, contactID, afterID)
	if err != nil {
		return nil, err
	}
	if markRead && resolvedChat != nil {
		_ = e.resetChatUnread(ctx, wsID, resolvedChat.ID, contactID)
	}
	return msgs, nil
}

// List retrieves chats for a workspace with filtering, pagination, and contact enrichment.
// Effective status for expired snoozes is derived on read without writing to storage.
func (e *Engine) List(ctx context.Context, workspaceID uuid.UUID, filter domain.ChatFilter, limit, offset int) ([]ChatSummary, error) {
	if e.chatRepo == nil {
		return nil, errors.New("chat repository not configured")
	}

	chats, err := e.chatRepo.ListChats(ctx, workspaceID, filter, limit, offset)
	if err != nil {
		return nil, err
	}

	contactMap := make(map[uuid.UUID]*domain.Contact, len(chats))
	if e.contactRepo != nil && len(chats) > 0 {
		uniqueIDs := make([]uuid.UUID, 0, len(chats))
		seen := make(map[uuid.UUID]struct{}, len(chats))
		for i := range chats {
			cid := chats[i].ContactID
			if cid != uuid.Nil {
				if _, ok := seen[cid]; !ok {
					seen[cid] = struct{}{}
					uniqueIDs = append(uniqueIDs, cid)
				}
			}
		}
		if batchGetter, ok := e.contactRepo.(contactsByIDsGetter); ok && len(uniqueIDs) > 0 {
			if fetched, bErr := batchGetter.GetByIDs(ctx, workspaceID, uniqueIDs); bErr == nil && fetched != nil {
				contactMap = fetched
			}
		}
	}

	results := make([]ChatSummary, 0, len(chats))
	for i := range chats {
		chatItem := *deriveEffectiveStatus(&chats[i])
		if filter.Status != "" && !strings.EqualFold(string(chatItem.Status), filter.Status) {
			continue
		}

		dto := ChatSummary{Chat: chatItem}
		if e.contactRepo != nil && chatItem.ContactID != uuid.Nil {
			contact, ok := contactMap[chatItem.ContactID]
			if !ok {
				if fetched, cErr := e.contactRepo.GetByID(ctx, workspaceID, chatItem.ContactID); cErr == nil && fetched != nil {
					contact = fetched
					contactMap[chatItem.ContactID] = fetched
				}
			}
			if contact != nil {
				dto.ContactName = contact.Name
				for _, id := range contact.Identities {
					dto.Identities = append(dto.Identities, fmt.Sprintf("%s:%s", id.Channel, id.SenderIdentity))
					if dto.ContactPhone == "" && (id.Channel == "whatsapp" || id.Channel == "whatsapp_cloud") {
						dto.ContactPhone = id.SenderIdentity
					}
				}
			}
		}
		results = append(results, dto)
	}

	return results, nil
}

// Get resolves a chat thread, derives effective snooze status, optionally marks it as read,
// and returns the chat along with its contact, connection, and optional message history.
func (e *Engine) Get(ctx context.Context, workspaceID uuid.UUID, ref ChatRef, messageLimit int, markRead bool) (*GetChatResult, error) {
	c, err := e.ResolveChat(ctx, workspaceID, ref)
	if err != nil {
		return nil, err
	}

	if markRead {
		_ = e.resetChatUnread(ctx, c.WorkspaceID, c.ID, c.ContactID)
		c.UnreadCount = 0
	}

	res := &GetChatResult{
		Chat: c,
	}

	if e.contactRepo != nil && c.ContactID != uuid.Nil {
		if contact, cErr := e.contactRepo.GetByID(ctx, c.WorkspaceID, c.ContactID); cErr == nil && contact != nil {
			res.Contact = contact
		}
	}

	if e.connRepo != nil && c.ConnectionID != nil && *c.ConnectionID != uuid.Nil {
		if conn, connErr := e.connRepo.GetByID(ctx, *c.ConnectionID); connErr == nil && conn != nil {
			res.Connection = conn
		}
	}

	if messageLimit > 0 {
		if msgs, mErr := e.chatRepo.ListChatMessages(ctx, c.WorkspaceID, c.ID, "", "", messageLimit); mErr == nil {
			res.Messages = msgs
		}
	}

	return res, nil
}

// GetMessage resolves a single message by UID within a workspace, returning the message,
// its parent chat (with derived effective snooze status), and the associated contact.
func (e *Engine) GetMessage(ctx context.Context, workspaceID uuid.UUID, messageUID string) (*domain.ChatMessage, *domain.Chat, *domain.Contact, error) {
	if e.chatRepo == nil {
		return nil, nil, nil, errors.New("chat repository not configured")
	}
	wsID, err := EnforceWorkspaceScope(ctx, workspaceID)
	if err != nil {
		return nil, nil, nil, err
	}
	uid := strings.TrimSpace(messageUID)
	if uid == "" {
		return nil, nil, nil, ErrMessageNotFound
	}

	var msg *domain.ChatMessage
	if wsID != uuid.Nil {
		m, gErr := e.chatRepo.GetChatMessageByUID(ctx, wsID, uid)
		if gErr == nil && m != nil {
			msg = m
		}
	}
	if msg == nil {
		if finder, ok := e.chatRepo.(messageByUIDFinder); ok {
			if m, fErr := finder.FindMessageByUID(ctx, uid); fErr == nil && m != nil {
				if wsID == uuid.Nil || m.WorkspaceID == wsID {
					msg = m
				}
			}
		}
	}
	if msg == nil {
		return nil, nil, nil, ErrMessageNotFound
	}
	if wsID != uuid.Nil && msg.WorkspaceID != wsID {
		return nil, nil, nil, ErrMessageNotFound
	}
	if _, sErr := EnforceWorkspaceScope(ctx, msg.WorkspaceID); sErr != nil {
		return nil, nil, nil, sErr
	}

	var c *domain.Chat
	if msg.ChatID != uuid.Nil {
		if ch, chErr := e.chatRepo.GetChat(ctx, msg.WorkspaceID, msg.ChatID); chErr == nil && ch != nil {
			c = deriveEffectiveStatus(ch)
		}
	}

	var contact *domain.Contact
	if c != nil && e.contactRepo != nil && c.ContactID != uuid.Nil {
		if ct, ctErr := e.contactRepo.GetByID(ctx, c.WorkspaceID, c.ContactID); ctErr == nil && ct != nil {
			contact = ct
		}
	}

	return msg, c, contact, nil
}

// ListMessages lists chronological messages for a resolved chat thread.
func (e *Engine) ListMessages(ctx context.Context, workspaceID uuid.UUID, ref ChatRef, beforeUID, afterUID string, limit int) (*domain.Chat, []domain.ChatMessage, error) {
	c, err := e.ResolveChat(ctx, workspaceID, ref)
	if err != nil {
		return nil, nil, err
	}

	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}

	messages, err := e.chatRepo.ListChatMessages(ctx, c.WorkspaceID, c.ID, beforeUID, afterUID, limit)
	if err != nil {
		return nil, nil, err
	}
	return c, messages, nil
}

// UpdateStatus transitions a chat between open, pending, snoozed, and closed,
// writing an audit log.
func (e *Engine) UpdateStatus(ctx context.Context, workspaceID uuid.UUID, ref ChatRef, upd StatusUpdate) (*domain.Chat, *time.Time, error) {
	c, err := e.ResolveChat(ctx, workspaceID, ref)
	if errors.Is(err, ErrChatNotFound) && ref.ContactID != uuid.Nil {
		c, err = e.EnsureChatForContact(ctx, workspaceID, ref.ContactID, false)
	}
	if err != nil {
		return nil, nil, err
	}

	status := strings.ToLower(strings.TrimSpace(upd.Status))
	switch status {
	case "resolved":
		status = string(domain.ChatStatusClosed)
	case "reopen":
		status = string(domain.ChatStatusOpen)
	}
	if status != string(domain.ChatStatusOpen) &&
		status != string(domain.ChatStatusPending) &&
		status != string(domain.ChatStatusSnoozed) &&
		status != string(domain.ChatStatusClosed) {
		return nil, nil, ErrInvalidStatus
	}

	var snoozedUntil *time.Time
	if status == string(domain.ChatStatusSnoozed) {
		if upd.SnoozedUntil == nil || upd.SnoozedUntil.IsZero() || !upd.SnoozedUntil.UTC().After(time.Now().UTC()) {
			return nil, nil, fmt.Errorf("%w: snoozed_until must be a future timestamp when status is snoozed", ErrInvalidStatus)
		}
		t := upd.SnoozedUntil.UTC()
		snoozedUntil = &t
		if err := e.chatRepo.SnoozeChat(ctx, c.WorkspaceID, c.ID, snoozedUntil); err != nil {
			return nil, nil, err
		}
		c.Status = domain.ChatStatusSnoozed
		c.SnoozedUntil = snoozedUntil
	} else {
		if err := e.chatRepo.UpdateChatStatus(ctx, c.WorkspaceID, c.ID, status); err != nil {
			return nil, nil, err
		}
		c.Status = domain.ChatStatus(status)
		c.SnoozedUntil = nil
	}


	if e.auditRepo != nil {
		auditPayload, _ := json.Marshal(map[string]interface{}{
			"chat_id":       c.ID.String(),
			"contact_id":    c.ContactID.String(),
			"status":        status,
			"snoozed_until": snoozedUntil,
		})
		_ = e.auditRepo.InsertAuditLog(ctx, &repository.AuditEntry{
			ID:          uuid.New(),
			WorkspaceID: c.WorkspaceID,
			TraceID:     fmt.Sprintf("chat-status-%s", uuid.New().String()),
			EventType:   "chat.status.updated",
			Payload:     auditPayload,
			CreatedAt:   time.Now().UTC(),
		})
	}

	return c, snoozedUntil, nil
}

// UpdateTags adds, removes, or replaces tags on a chat thread,
// synchronizing the resulting tag set onto the contact and writing an audit log.
func (e *Engine) UpdateTags(ctx context.Context, workspaceID uuid.UUID, ref ChatRef, mut TagMutation) ([]string, error) {
	c, err := e.ResolveChat(ctx, workspaceID, ref)
	if errors.Is(err, ErrChatNotFound) && ref.ContactID != uuid.Nil {
		c, err = e.EnsureChatForContact(ctx, workspaceID, ref.ContactID, false)
	}
	if err != nil {
		return nil, err
	}

	var newTags []string
	if mut.Set != nil {
		newTags = mut.Set
	} else {
		tagMap := make(map[string]bool)
		for _, t := range c.Tags {
			clean := strings.TrimSpace(t)
			if clean != "" {
				tagMap[strings.ToLower(clean)] = true
				newTags = append(newTags, clean)
			}
		}
		for _, addTag := range mut.Add {
			clean := strings.TrimSpace(addTag)
			if clean != "" && !tagMap[strings.ToLower(clean)] {
				tagMap[strings.ToLower(clean)] = true
				newTags = append(newTags, clean)
			}
		}
		if len(mut.Remove) > 0 {
			removeMap := make(map[string]bool)
			for _, remTag := range mut.Remove {
				removeMap[strings.ToLower(strings.TrimSpace(remTag))] = true
			}
			filtered := make([]string, 0, len(newTags))
			for _, t := range newTags {
				if !removeMap[strings.ToLower(t)] {
					filtered = append(filtered, t)
				}
			}
			newTags = filtered
		}
	}

	if newTags == nil {
		newTags = []string{}
	}

	if err := e.chatRepo.SetChatTags(ctx, c.WorkspaceID, c.ID, newTags); err != nil {
		return nil, err
	}

	if e.contactRepo != nil && c.ContactID != uuid.Nil {
		if sErr := e.contactRepo.SetTags(ctx, c.WorkspaceID, c.ContactID, newTags); sErr != nil {
			slog.WarnContext(ctx, "failed to sync tags with contact", "error", sErr, "contact_id", c.ContactID)
		}
	}

	if e.auditRepo != nil {
		auditPayload, _ := json.Marshal(map[string]interface{}{
			"chat_id":    c.ID.String(),
			"contact_id": c.ContactID.String(),
			"tags":       newTags,
		})
		_ = e.auditRepo.InsertAuditLog(ctx, &repository.AuditEntry{
			ID:          uuid.New(),
			WorkspaceID: c.WorkspaceID,
			TraceID:     fmt.Sprintf("chat-labels-%s", uuid.New().String()),
			EventType:   "chat.tag.updated",
			Payload:     auditPayload,
			CreatedAt:   time.Now().UTC(),
		})
	}

	return newTags, nil
}

// Assign assigns a chat to a verified workspace member (or unassigns when email is empty)
// and records an audit entry.
func (e *Engine) Assign(ctx context.Context, workspaceID uuid.UUID, ref ChatRef, email string) (*domain.Chat, *repository.WorkspaceMember, error) {
	c, err := e.ResolveChat(ctx, workspaceID, ref)
	if errors.Is(err, ErrChatNotFound) && ref.ContactID != uuid.Nil {
		c, err = e.EnsureChatForContact(ctx, workspaceID, ref.ContactID, false)
	}
	if err != nil {
		return nil, nil, err
	}

	trimmed := strings.TrimSpace(email)
	if trimmed == "" {
		if err := e.chatRepo.AssignChat(ctx, c.WorkspaceID, c.ID, nil, nil); err != nil {
			return nil, nil, err
		}
		c.AssignedUserID = nil
		c.AssignedEmail = nil

		if e.auditRepo != nil {
			auditPayload, _ := json.Marshal(map[string]interface{}{
				"chat_id":        c.ID.String(),
				"contact_id":     c.ContactID.String(),
				"assigned_email": "",
				"action":         "unassign",
			})
			_ = e.auditRepo.InsertAuditLog(ctx, &repository.AuditEntry{
				ID:          uuid.New(),
				WorkspaceID: c.WorkspaceID,
				TraceID:     fmt.Sprintf("chat-unassign-%s", uuid.New().String()),
				EventType:   "chat.assigned",
				Payload:     auditPayload,
				CreatedAt:   time.Now().UTC(),
			})
		}
		if updated, gErr := e.chatRepo.GetChat(ctx, c.WorkspaceID, c.ID); gErr == nil && updated != nil {
			c = updated
		}
		return c, nil, nil
	}

	var member *repository.WorkspaceMember
	var memberID *uuid.UUID
	targetEmail := trimmed
	if e.wsRepo != nil {
		m, mErr := e.wsRepo.GetMemberByEmail(ctx, c.WorkspaceID, trimmed)
		if mErr != nil || m == nil {
			return nil, nil, ErrInvalidMember
		}
		member = m
		memberID = &m.ID
		if m.Email != "" {
			targetEmail = m.Email
		}
	}

	if err := e.chatRepo.AssignChat(ctx, c.WorkspaceID, c.ID, memberID, &targetEmail); err != nil {
		return nil, nil, err
	}
	c.AssignedUserID = memberID
	c.AssignedEmail = &targetEmail

	if e.auditRepo != nil {
		auditMap := map[string]interface{}{
			"chat_id":        c.ID.String(),
			"contact_id":     c.ContactID.String(),
			"assigned_email": targetEmail,
			"action":         "assign",
		}
		if memberID != nil {
			auditMap["assigned_user_id"] = memberID.String()
		}
		auditPayload, _ := json.Marshal(auditMap)
		_ = e.auditRepo.InsertAuditLog(ctx, &repository.AuditEntry{
			ID:          uuid.New(),
			WorkspaceID: c.WorkspaceID,
			TraceID:     fmt.Sprintf("chat-assign-%s", uuid.New().String()),
			EventType:   "chat.assigned",
			Payload:     auditPayload,
			CreatedAt:   time.Now().UTC(),
		})
	}

	if updated, gErr := e.chatRepo.GetChat(ctx, c.WorkspaceID, c.ID); gErr == nil && updated != nil {
		c = updated
	}

	return c, member, nil
}

// SetAI transitions AI engagement on a chat via the Chat Activity recorder and writes an audit log.
func (e *Engine) SetAI(ctx context.Context, workspaceID uuid.UUID, ref ChatRef, change activity.AIChange) (*domain.Chat, error) {
	c, err := e.ResolveChat(ctx, workspaceID, ref)
	if errors.Is(err, ErrChatNotFound) && ref.ContactID != uuid.Nil {
		c, err = e.EnsureChatForContact(ctx, workspaceID, ref.ContactID, false)
	}
	if err != nil {
		return nil, err
	}

	rec := e.ensureActivity()
	if rec == nil {
		return nil, errors.New("chat activity recorder not configured")
	}

	targetWS := c.WorkspaceID
	if targetWS == uuid.Nil {
		targetWS, _ = EnforceWorkspaceScope(ctx, workspaceID)
	}

	ai, err := rec.SetAI(ctx, targetWS, activity.ChatID(c.ID), change)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	engaged := ai.Effective(now).IsEngaged()
	c.AI = ai
	c.AIDisabled = !engaged

	if e.auditRepo != nil {
		auditPayload, _ := json.Marshal(map[string]interface{}{
			"chat_id":     c.ID.String(),
			"contact_id":  c.ContactID.String(),
			"enabled":     engaged,
			"ai_disabled": !engaged,
			"ai_mode":     string(ai.Mode),
		})
		_ = e.auditRepo.InsertAuditLog(ctx, &repository.AuditEntry{
			ID:          uuid.New(),
			WorkspaceID: c.WorkspaceID,
			TraceID:     fmt.Sprintf("chat-ai-%s", uuid.New().String()),
			EventType:   "chat.ai_status.updated",
			Payload:     auditPayload,
			CreatedAt:   now,
		})
	}

	return c, nil
}

// CreateNote creates a private internal note (or AI whisper draft) on a chat thread and writes an audit entry.
func (e *Engine) CreateNote(ctx context.Context, workspaceID uuid.UUID, req NoteRequest) (*domain.ChatMessage, error) {
	body := strings.TrimSpace(req.Body)
	if body == "" {
		return nil, ErrEmptyBody
	}

	authorName := strings.TrimSpace(req.AuthorName)
	if authorName == "" {
		authorName = "Agent"
	}

	senderType := strings.TrimSpace(req.SenderType)
	if senderType == "" {
		senderType = string(domain.SenderTypeHumanAgent)
	}

	var chatID uuid.UUID
	wsID, err := EnforceWorkspaceScope(ctx, workspaceID)
	if err != nil {
		return nil, ErrChatNotFound
	}

	if req.Ref.ChatID != uuid.Nil {
		chatID = req.Ref.ChatID
	} else {
		c, rErr := e.ResolveChat(ctx, wsID, req.Ref)
		if errors.Is(rErr, ErrChatNotFound) && req.Ref.ContactID != uuid.Nil {
			c, rErr = e.EnsureChatForContact(ctx, wsID, req.Ref.ContactID, false)
		}
		if rErr != nil {
			return nil, rErr
		}
		chatID = c.ID
		wsID = c.WorkspaceID
	}

	note, err := e.chatRepo.CreateInternalNote(ctx, wsID, chatID, authorName, senderType, body)
	if err != nil {
		if errors.Is(err, repository.ErrChatNotFound) || strings.Contains(err.Error(), "not found") {
			return nil, ErrChatNotFound
		}
		return nil, err
	}

	if _, sErr := EnforceWorkspaceScope(ctx, note.WorkspaceID); sErr != nil {
		return nil, sErr
	}

	if e.auditRepo != nil {
		targetWs := wsID
		if targetWs == uuid.Nil {
			targetWs = note.WorkspaceID
		}
		auditPayload, _ := json.Marshal(map[string]interface{}{
			"chat_id":     chatID.String(),
			"note_id":     note.ID.String(),
			"author_name": authorName,
			"body":        body,
			"sender_type": senderType,
			"is_private":  true,
		})
		_ = e.auditRepo.InsertAuditLog(ctx, &repository.AuditEntry{
			ID:          uuid.New(),
			WorkspaceID: targetWs,
			TraceID:     note.UID,
			EventType:   "chat.note.created",
			Payload:     auditPayload,
			CreatedAt:   note.CreatedAt,
		})
	}

	return note, nil
}

// React mutates a reaction on a chat message, publishes a NATS reaction event,
// dispatches reaction webhooks, and writes an audit log.
func (e *Engine) React(ctx context.Context, workspaceID uuid.UUID, req ReactRequest) (*ReactResult, error) {
	if e.chatRepo == nil {
		return nil, errors.New("chat repository not configured")
	}

	messageUID := strings.TrimSpace(req.MessageUID)
	emoji := strings.TrimSpace(req.Emoji)
	if messageUID == "" || emoji == "" {
		return nil, errors.New("message_uid and emoji are required")
	}

	action := strings.ToLower(strings.TrimSpace(req.Action))
	if action != "" && action != "add" && action != "remove" && action != "toggle" {
		return nil, errors.New("action must be 'add', 'remove', or 'toggle'")
	}

	sender := strings.TrimSpace(req.Sender)
	if sender == "" {
		sender = "operator"
	}

	wsID, err := EnforceWorkspaceScope(ctx, workspaceID)
	if err != nil {
		return nil, err
	}

	if wsID == uuid.Nil {
		if finder, ok := e.chatRepo.(messageByUIDFinder); ok {
			msg, mErr := finder.FindMessageByUID(ctx, messageUID)
			if mErr != nil || msg == nil {
				return nil, ErrMessageNotFound
			}
			wsID = msg.WorkspaceID
		}
	}

	if _, sErr := EnforceWorkspaceScope(ctx, wsID); sErr != nil {
		return nil, sErr
	}

	if req.ChatRef.ChatID != uuid.Nil {
		msg, mErr := e.chatRepo.GetChatMessageByUID(ctx, wsID, messageUID)
		if mErr != nil || msg == nil {
			return nil, ErrMessageNotFound
		}
		if msg.ChatID != req.ChatRef.ChatID {
			return nil, ErrChatMismatch
		}
	}

	chatID, resolvedAction, updatedReactions, err := e.chatRepo.MutateReaction(ctx, wsID, messageUID, emoji, sender, action)
	if err != nil {
		if errors.Is(err, repository.ErrChatMessageNotFound) {
			return nil, ErrMessageNotFound
		}
		return nil, err
	}

	now := time.Now().UTC()
	traceID := uuid.New().String()
	rxEvent := domain.ReactionUpdatedPayload{
		Event:       "message.reaction.updated",
		WorkspaceID: wsID.String(),
		ChatID:      chatID.String(),
		MessageUID:  messageUID,
		Emoji:       emoji,
		Sender:      sender,
		Action:      resolvedAction,
		Reactions:   updatedReactions,
		Timestamp:   now.Format(time.RFC3339),
	}

	if payloadBytes, mErr := json.Marshal(rxEvent); mErr == nil {
		if e.publisher != nil {
			_ = e.publisher.Publish(ctx, "messages.events.reaction_updated", payloadBytes, traceID)
		}
		if e.webhookDispatcher != nil {
			_ = e.webhookDispatcher.DispatchReaction(ctx, wsID, messageUID, payloadBytes, traceID)
		}
		if e.auditRepo != nil {
			_ = e.auditRepo.InsertAuditLog(ctx, &repository.AuditEntry{
				ID:          uuid.New(),
				WorkspaceID: wsID,
				TraceID:     traceID,
				EventType:   "reaction_updated",
				Payload:     payloadBytes,
				CreatedAt:   now,
			})
		}
	}

	return &ReactResult{
		ChatID:     chatID,
		MessageUID: messageUID,
		Action:     resolvedAction,
		Emoji:      emoji,
		Sender:     sender,
		Reactions:  updatedReactions,
	}, nil
}

// PromoteDraft promotes an internal draft note (stripped of any "[AI Draft]: " prefix
// or replaced by editedBody) into an outbound customer message via SendMessage.
func (e *Engine) PromoteDraft(ctx context.Context, workspaceID uuid.UUID, ref ChatRef, draftUID, editedBody string) (*SendResult, error) {
	c, err := e.ResolveChat(ctx, workspaceID, ref)
	if errors.Is(err, ErrChatNotFound) && strings.TrimSpace(draftUID) != "" {
		c, err = e.ResolveChat(ctx, workspaceID, ChatRef{MessageUID: strings.TrimSpace(draftUID)})
	}
	if err != nil {
		return nil, err
	}

	var draftMsg *domain.ChatMessage
	if strings.TrimSpace(draftUID) != "" && e.chatRepo != nil {
		var mErr error
		draftMsg, mErr = e.chatRepo.GetChatMessageByUID(ctx, c.WorkspaceID, strings.TrimSpace(draftUID))
		if mErr != nil || draftMsg == nil || draftMsg.ChatID != c.ID {
			return nil, ErrMessageNotFound
		}
	}

	finalBody := strings.TrimSpace(editedBody)
	if finalBody == "" && draftMsg != nil {
		finalBody = strings.TrimSpace(draftMsg.Body)
	}
	finalBody = strings.TrimSpace(strings.TrimPrefix(finalBody, "[AI Draft]: "))
	finalBody = strings.TrimSpace(strings.TrimPrefix(finalBody, "[AI Draft]:"))
	if finalBody == "" {
		return nil, ErrEmptyBody
	}

	res, err := e.SendMessage(ctx, c.WorkspaceID, SendRequest{
		Ref:        ChatRef{ChatID: c.ID},
		Body:       finalBody,
		SenderType: string(domain.SenderTypeHumanAgent),
		SenderName: "Operator (Approved AI Draft)",
	})
	if err != nil {
		return nil, err
	}

	if draftMsg != nil && e.chatRepo != nil {
		if draftMsg.Metadata == nil {
			draftMsg.Metadata = make(map[string]interface{})
		}
		draftMsg.Metadata["draft_promoted"] = true
		draftMsg.Metadata["draft_status"] = "promoted"
		if res != nil && res.TraceID != "" {
			draftMsg.Metadata["promoted_message_uid"] = res.TraceID
		}
		if updater, ok := e.chatRepo.(messageMetadataUpdater); ok {
			_ = updater.UpdateMessageMetadata(ctx, c.WorkspaceID, draftMsg.UID, draftMsg.Metadata)
		}
	}

	return res, nil
}

// SendMessage dispatches an outbound message on a chat thread, delegating
// outbound turn persistence, AI state transition, debounce cancellation,
// handoff event publishing, WABA 24h window enforcement, and queue backpressure
// solely to OutboundProcessor.Ingest, and recording an immutable audit entry.
func (e *Engine) SendMessage(ctx context.Context, workspaceID uuid.UUID, req SendRequest) (*SendResult, error) {
	body := strings.TrimSpace(req.Body)
	if body == "" && req.Media == nil && strings.TrimSpace(req.TemplateName) == "" && req.Interactive == nil && req.Product == nil {
		return nil, ErrEmptyBody
	}

	var contact *domain.Contact
	var wsID uuid.UUID
	c, err := e.ResolveChat(ctx, workspaceID, req.Ref)
	if err != nil {
		if !errors.Is(err, ErrChatNotFound) || (req.Ref.ContactID == uuid.Nil && strings.TrimSpace(req.Ref.Phone) == "") {
			return nil, err
		}
		var sErr error
		wsID, sErr = EnforceWorkspaceScope(ctx, workspaceID)
		if sErr != nil || wsID == uuid.Nil {
			return nil, ErrChatNotFound
		}
		if e.contactRepo != nil && req.Ref.ContactID != uuid.Nil {
			contact, _ = e.contactRepo.GetByID(ctx, wsID, req.Ref.ContactID)
		}
	} else {
		wsID = c.WorkspaceID
		if e.contactRepo != nil && c.ContactID != uuid.Nil {
			contact, _ = e.contactRepo.GetByID(ctx, wsID, c.ContactID)
		}
	}

	// Resolve channel and connection
	var channel string
	var connID uuid.UUID
	var senderIdentity string

	if req.ConnectionID != nil && *req.ConnectionID != uuid.Nil {
		connID = *req.ConnectionID
		if e.connRepo != nil {
			if conn, cErr := e.connRepo.GetByID(ctx, *req.ConnectionID); cErr == nil && conn != nil && conn.WorkspaceID == wsID {
				connID = conn.ID
				senderIdentity = conn.SenderIdentity
				channel = conn.Channel
			}
		}
	}
	if connID == uuid.Nil && c != nil && c.ConnectionID != nil && *c.ConnectionID != uuid.Nil {
		connID = *c.ConnectionID
		if e.connRepo != nil {
			if conn, cErr := e.connRepo.GetByID(ctx, *c.ConnectionID); cErr == nil && conn != nil {
				connID = conn.ID
				senderIdentity = conn.SenderIdentity
				channel = conn.Channel
			}
		}
	}

	// Resolve recipient 'to'
	to := strings.TrimSpace(req.Ref.Phone)
	if contact != nil {
		if to == "" && channel != "" {
			for _, id := range contact.Identities {
				if id.Channel == channel {
					to = id.SenderIdentity
					break
				}
			}
		}
		if to == "" {
			for _, id := range contact.Identities {
				if id.Channel == "whatsapp_cloud" || id.Channel == "whatsapp" || id.Channel == "telegram" {
					if channel == "" {
						channel = id.Channel
					}
					to = id.SenderIdentity
					break
				}
			}
		}
		if to == "" && len(contact.Identities) > 0 {
			if channel == "" {
				channel = contact.Identities[0].Channel
			}
			to = contact.Identities[0].SenderIdentity
		}
		if to == "" && contact.Attributes != nil {
			if p := strings.TrimSpace(contact.Attributes["phone"]); p != "" {
				to = p
			}
		}
	}

	if connID == uuid.Nil && e.connRepo != nil && strings.TrimSpace(req.SenderIdentity) != "" {
		if conn, cErr := e.connRepo.GetBySenderIdentity(ctx, wsID, strings.TrimSpace(req.SenderIdentity)); cErr == nil && conn != nil {
			connID = conn.ID
			senderIdentity = conn.SenderIdentity
			if channel == "" {
				channel = conn.Channel
			}
		}
	}
	if channel == "" && req.Ref.Channel != "" {
		channel = req.Ref.Channel
	}
	if connID == uuid.Nil && e.connRepo != nil && channel != "" {
		if defaultConn, cErr := e.connRepo.GetDefaultChannelConnection(ctx, wsID, channel); cErr == nil && defaultConn != nil {
			connID = defaultConn.ID
			senderIdentity = defaultConn.SenderIdentity
		}
	}
	if senderIdentity == "" && req.SenderIdentity != "" {
		senderIdentity = req.SenderIdentity
	}

	if e.ingestor == nil {
		return nil, errors.New("outbound message ingestor not configured")
	}
	if to == "" {
		return nil, errors.New("contact has no phone number or channel identity")
	}

	if channel == "" {
		channel = "whatsapp"
	}

	senderName := strings.TrimSpace(req.SenderName)
	if senderName == "" {
		senderName = "Agent"
	}

	senderType := domain.NormalizeSenderType(req.SenderType)
	if senderType == "" {
		senderType = domain.SenderTypeHumanAgent
	}

	prefix := strings.TrimSpace(req.TraceIDPrefix)
	if prefix == "" {
		prefix = "api-chat"
	}
	traceID := fmt.Sprintf("%s-%s", prefix, uuid.New().String())
	now := time.Now().UTC()

	displayBody := body
	if displayBody == "" && req.TemplateName != "" {
		if req.Language != "" {
			displayBody = fmt.Sprintf("[Template: %s (%s)]", req.TemplateName, req.Language)
		} else {
			displayBody = fmt.Sprintf("[Template: %s]", req.TemplateName)
		}
	}

	meta := make(map[string]string)
	for k, v := range req.Metadata {
		meta[k] = v
	}
	var chatIDPtr *uuid.UUID
	if c != nil {
		meta["chat_id"] = c.ID.String()
		chatIDPtr = &c.ID
	}
	meta["sender_type"] = string(senderType)
	meta["sender_name"] = senderName
	if connID != uuid.Nil {
		meta["connection_id"] = connID.String()
	}
	if req.ReplyToUID != nil && *req.ReplyToUID != "" {
		meta["reply_to_uid"] = *req.ReplyToUID
	}

	createReq := &domain.CreateMessageRequest{
		To:               to,
		Channel:          channel,
		From:             senderIdentity,
		Body:             body,
		Media:            req.Media,
		Metadata:         meta,
		FallbackChannels: req.FallbackChannels,
		FallbackBehavior: req.FallbackBehavior,
		TemplateName:     req.TemplateName,
		Language:         req.Language,
		Components:       req.Components,
		Interactive:      req.Interactive,
		ChannelOverrides: req.ChannelOverrides,
		Type:             req.Type,
		Product:          req.Product,
		ConversationalIngestFields: domain.ConversationalIngestFields{
			ChatID:     chatIDPtr,
			ReplyToUID: req.ReplyToUID,
			SenderType: string(senderType),
			SenderName: senderName,
		},
	}

	qMsg, ingestErr := e.ingestor.Ingest(ctx, wsID, traceID, createReq)
	if ingestErr != nil {
		var tmplErr *domain.TemplateRequiredError
		if errors.As(ingestErr, &tmplErr) {
			return nil, fmt.Errorf("%w: %w", ErrServiceWindowExpired, ingestErr)
		}
		return nil, ingestErr
	}
	if qMsg != nil && !qMsg.QueuedAt.IsZero() {
		now = qMsg.QueuedAt
	}

	var persistedMsg *domain.ChatMessage
	if e.chatRepo != nil {
		persistedMsg, _ = e.chatRepo.GetChatMessageByUID(ctx, wsID, traceID)
		if persistedMsg != nil && persistedMsg.ChatID != uuid.Nil {
			if updatedChat, gErr := e.chatRepo.GetChat(ctx, wsID, persistedMsg.ChatID); gErr == nil && updatedChat != nil {
				c = updatedChat
			}
		} else if c != nil {
			if updatedChat, gErr := e.chatRepo.GetChat(ctx, wsID, c.ID); gErr == nil && updatedChat != nil {
				c = updatedChat
			}
		}
	}
	if c == nil {
		c, _ = e.ResolveChat(ctx, wsID, req.Ref)
	}
	if c == nil {
		isFreeformWABA := channel == "whatsapp_cloud" && strings.TrimSpace(req.TemplateName) == "" && !strings.EqualFold(strings.TrimSpace(req.Type), "template")
		if isFreeformWABA {
			tmplErr := &domain.TemplateRequiredError{
				Window: domain.Window(domain.ChatState{Channel: channel}, false, now),
				Source: "ingestion",
			}
			return nil, fmt.Errorf("%w: %w", ErrServiceWindowExpired, tmplErr)
		}
		if req.Ref.ContactID != uuid.Nil {
			c, err = e.EnsureChatForContact(ctx, wsID, req.Ref.ContactID, false)
		} else if strings.TrimSpace(req.Ref.Phone) != "" {
			if resolver, ok := e.contactRepo.(contactResolver); ok {
				if creator, ok := e.chatRepo.(chatFinderCreator); ok {
					phone := strings.TrimSpace(req.Ref.Phone)
					resolvedContact, rErr := resolver.ResolveContact(ctx, wsID, channel, phone, phone, "", phone)
					if rErr == nil && resolvedContact != nil {
						connIDPtr := req.ConnectionID
						if connIDPtr == nil && connID != uuid.Nil {
							connIDPtr = &connID
						}
						c, err = creator.FindOrCreateChat(ctx, wsID, connIDPtr, resolvedContact.ID)
					} else {
						err = rErr
					}
				}
			}
		}
		if err != nil {
			return nil, err
		}
		if c == nil {
			return nil, ErrChatNotFound
		}
	}

	if c.Status == domain.ChatStatusSnoozed || c.Status == domain.ChatStatusClosed {
		c.Status = domain.ChatStatusOpen
		c.SnoozedUntil = nil
	}

	if e.auditRepo != nil {
		eventType := req.AuditEventType
		if eventType == "" {
			eventType = "chat.message.sent"
		}
		auditMap := map[string]interface{}{
			"chat_id":         c.ID.String(),
			"contact_id":      c.ContactID.String(),
			"to":              to,
			"channel":         channel,
			"sender_identity": senderIdentity,
			"body":            displayBody,
			"sender_name":     senderName,
			"sender_type":     string(senderType),
		}
		if connID != uuid.Nil {
			auditMap["connection_id"] = connID.String()
		}
		if req.TemplateName != "" {
			auditMap["template_name"] = req.TemplateName
		}
		if req.Language != "" {
			auditMap["language"] = req.Language
		}
		if req.ReplyToUID != nil && *req.ReplyToUID != "" {
			auditMap["reply_to_uid"] = *req.ReplyToUID
		}
		auditPayload, _ := json.Marshal(auditMap)
		_ = e.auditRepo.InsertAuditLog(ctx, &repository.AuditEntry{
			ID:          uuid.New(),
			WorkspaceID: wsID,
			TraceID:     traceID,
			EventType:   eventType,
			Payload:     auditPayload,
			CreatedAt:   now,
		})
	}

	var msgID uuid.UUID
	if persistedMsg != nil {
		msgID = persistedMsg.ID
	}

	return &SendResult{
		TraceID:      traceID,
		Status:       "queued",
		QueuedAt:     now,
		ChatID:       c.ID,
		ContactID:    c.ContactID,
		ConnectionID: connID,
		Channel:      channel,
		From:         senderIdentity,
		To:           to,
		Body:         displayBody,
		ReplyToUID:   req.ReplyToUID,
		AIDisabled:   !c.EffectiveAI().IsEngaged() || senderType == domain.SenderTypeHumanAgent,
		MessageID:    msgID,
		Message:      persistedMsg,
	}, nil
}
