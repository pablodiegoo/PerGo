package domain

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Direction represents the trajectory of a message in a conversation.
type Direction string

const (
	DirectionInbound      Direction = "inbound"
	DirectionOutbound     Direction = "outbound"
	DirectionInternalNote Direction = "internal_note"
)

// SenderType identifies the origin actor of a message.
type SenderType string

const (
	SenderTypeContact    SenderType = "contact"
	SenderTypeHumanAgent SenderType = "human_agent"
	SenderTypeAIAgent    SenderType = "ai_agent"
	SenderTypeSystem     SenderType = "system"
)

// NormalizeSenderType maps common sender type aliases to canonical SenderType constants.
// Returns an empty SenderType ("") when raw is empty so callers can apply context-specific defaults.
func NormalizeSenderType(raw string) SenderType {
	trimmed := strings.TrimSpace(raw)
	switch strings.ToLower(trimmed) {
	case "human", "human_agent", "operator":
		return SenderTypeHumanAgent
	case "ai", "ai_agent", "bot":
		return SenderTypeAIAgent
	case "system":
		return SenderTypeSystem
	case "contact":
		return SenderTypeContact
	case "":
		return ""
	default:
		return SenderType(trimmed)
	}
}

// ChatStatus represents the lifecycle state of a conversation.
type ChatStatus string

const (
	ChatStatusOpen    ChatStatus = "open"
	ChatStatusPending ChatStatus = "pending"
	ChatStatusClosed  ChatStatus = "closed"
	ChatStatusSnoozed ChatStatus = "snoozed"
)

// EffectiveStatus evaluates the effective ChatStatus at timestamp now without mutating storage.
// When status is snoozed and snoozedUntil is at or before now, it returns ChatStatusOpen.
func EffectiveStatus(status ChatStatus, snoozedUntil *time.Time, now time.Time) ChatStatus {
	if status == "" {
		return ChatStatusOpen
	}
	if status == ChatStatusSnoozed && snoozedUntil != nil && !snoozedUntil.After(now) {
		return ChatStatusOpen
	}
	return status
}

// Reaction represents an emoji reaction attached to a message.
type Reaction struct {
	Emoji     string    `json:"emoji"`
	Sender    string    `json:"sender"`
	CreatedAt time.Time `json:"created_at"`
}

// ReactionUpdatedPayload is the typed payload for reaction events published to NATS and webhooks.
type ReactionUpdatedPayload struct {
	Event       string     `json:"event"`
	WorkspaceID string     `json:"workspace_id"`
	ChatID      string     `json:"chat_id,omitempty"`
	MessageUID  string     `json:"message_uid"`
	Emoji       string     `json:"emoji"`
	Sender      string     `json:"sender"`
	Action      string     `json:"action"` // "add" | "remove"
	Reactions   []Reaction `json:"reactions"`
	Timestamp   string     `json:"timestamp"`
}

// AIMode represents the AI Engagement mode on a Chat.
type AIMode string

const (
	AIModeActive AIMode = "active"
	AIModePaused AIMode = "paused"
	AIModeOff    AIMode = "off"
)

// AIEngagement holds the per-Chat AI automation engagement state: active, paused until T, or off.
type AIEngagement struct {
	Mode        AIMode     `json:"mode"`
	PausedUntil *time.Time `json:"paused_until,omitempty"`
}

// Effective evaluates the effective AI Engagement at timestamp now without mutating storage.
// Expired pauses return active with a nil PausedUntil.
func (e AIEngagement) Effective(now time.Time) AIEngagement {
	switch e.Mode {
	case AIModeOff:
		return AIEngagement{Mode: AIModeOff}
	case AIModePaused:
		if e.PausedUntil != nil && now.Before(*e.PausedUntil) {
			u := e.PausedUntil.UTC()
			return AIEngagement{Mode: AIModePaused, PausedUntil: &u}
		}
		return AIEngagement{Mode: AIModeActive}
	default:
		return AIEngagement{Mode: AIModeActive}
	}
}

// IsEngaged reports whether this AIEngagement is in the active (engaged) mode.
func (e AIEngagement) IsEngaged() bool {
	return e.Mode == "" || e.Mode == AIModeActive
}

// Chat represents a conversational thread between a workspace connection and a contact.
type Chat struct {
	ID                     uuid.UUID              `json:"id"`
	WorkspaceID            uuid.UUID              `json:"workspace_id"`
	ConnectionID           *uuid.UUID             `json:"connection_id,omitempty"`
	ContactID              uuid.UUID              `json:"contact_id"`
	Status                 ChatStatus             `json:"status"` // "open", "closed", "snoozed"
	SnoozedUntil           *time.Time             `json:"snoozed_until,omitempty"`
	AssignedUserID         *uuid.UUID             `json:"assigned_user_id,omitempty"`
	AssignedEmail          *string                `json:"assigned_email,omitempty"`
	Tags                   []string               `json:"tags"`
	UnreadCount            int                    `json:"unread_count"`
	AI                     AIEngagement           `json:"ai"`
	AIDisabled             bool                   `json:"ai_disabled"`
	ServiceWindowExpiresAt *time.Time             `json:"service_window_expires_at,omitempty"`
	ServiceWindowIsOpen    bool                   `json:"service_window_is_open"`
	LastMessageAt          time.Time              `json:"last_message_at"`
	Metadata               map[string]interface{} `json:"metadata"`
	CreatedAt              time.Time              `json:"created_at"`
	UpdatedAt              time.Time              `json:"updated_at"`
}

// EffectiveStatus returns the effective ChatStatus for this Chat at the given time (defaults to time.Now().UTC()).
func (c *Chat) EffectiveStatus(now ...time.Time) ChatStatus {
	if c == nil {
		return ChatStatusOpen
	}
	t := time.Now().UTC()
	if len(now) > 0 && !now[0].IsZero() {
		t = now[0].UTC()
	}
	return EffectiveStatus(c.Status, c.SnoozedUntil, t)
}

// EffectiveAI returns the effective AIEngagement for this Chat at the given time (defaults to time.Now().UTC()).
func (c *Chat) EffectiveAI(now ...time.Time) AIEngagement {
	if c == nil {
		return AIEngagement{Mode: AIModeActive}
	}
	t := time.Now().UTC()
	if len(now) > 0 && !now[0].IsZero() {
		t = now[0].UTC()
	}
	if c.AI.Mode == "" && c.AIDisabled {
		return AIEngagement{Mode: AIModeOff}
	}
	return c.AI.Effective(t)
}

// IsServiceWindowOpen reports whether the customer service window is active.
func (c *Chat) IsServiceWindowOpen(now ...time.Time) bool {
	if c == nil || c.ServiceWindowExpiresAt == nil {
		return false
	}
	t := time.Now().UTC()
	if len(now) > 0 && !now[0].IsZero() {
		t = now[0].UTC()
	}
	return t.Before(*c.ServiceWindowExpiresAt)
}

// ChatMessage represents a single message, dispatch, or internal note within a Chat.
type ChatMessage struct {
	ID          uuid.UUID              `json:"id"`
	ChatID      uuid.UUID              `json:"chat_id"`
	WorkspaceID uuid.UUID              `json:"workspace_id"`
	UID         string                 `json:"uid"`
	Direction   Direction              `json:"direction"`
	SenderType  SenderType             `json:"sender_type"`
	SenderName  string                 `json:"sender_name,omitempty"`
	SenderID    string                 `json:"sender_id,omitempty"`
	Body        string                 `json:"body"`
	MediaURL    *string                `json:"media_url,omitempty"`
	MediaType   *string                `json:"media_type,omitempty"`
	IsPrivate   bool                   `json:"is_private"`
	Reactions   []Reaction             `json:"reactions"`
	ReplyToUID  *string                `json:"reply_to_uid,omitempty"`
	Metadata    map[string]interface{} `json:"metadata"`
	CreatedAt   time.Time              `json:"created_at"`
}

// ChatFilter encapsulates querying criteria for listing workspace chats.
type ChatFilter struct {
	Status        string     `json:"status,omitempty"`
	Channel       string     `json:"channel,omitempty"`
	Unread        *bool      `json:"unread,omitempty"`
	Phone         string     `json:"phone,omitempty"`
	ContactID     *uuid.UUID `json:"contact_id,omitempty"`
	ConnectionID  *uuid.UUID `json:"connection_id,omitempty"`
	AssignedEmail *string    `json:"assigned_email,omitempty"`
	Unassigned    *bool      `json:"unassigned,omitempty"`
	Tag           string     `json:"tag,omitempty"`
}

// ChatSynopsis represents a 3-bullet synopsis of customer issues, promises made, and current status.
type ChatSynopsis struct {
	CustomerIssues string `json:"customer_issues"`
	PromisesMade   string `json:"promises_made"`
	CurrentStatus  string `json:"current_status"`
}

// SummarizeMessages generates a structured 3-bullet synopsis: Customer Issues, Promises Made, and Current Status.
func SummarizeMessages(messages []ChatMessage, chat *Chat) ChatSynopsis {
	var inboundBodies []string
	var agentCommitments []string
	var allAgentBodies []string

	commitmentKeywords := []string{
		"proposta", "enviar", "envio", "retorno", "amanhã", "amanha", "prazo",
		"aguarde", "preço", "preco", "valor", "orçamento", "orcamento", "reunião",
		"reuniao", "link", "desconto", "confirm", "send", "promise", "proposal",
	}

	for _, msg := range messages {
		body := strings.TrimSpace(msg.Body)
		if body == "" {
			continue
		}

		if msg.Direction == DirectionInbound || msg.SenderType == SenderTypeContact {
			inboundBodies = append(inboundBodies, body)
		} else {
			allAgentBodies = append(allAgentBodies, body)
			lowerBody := strings.ToLower(body)
			for _, kw := range commitmentKeywords {
				if strings.Contains(lowerBody, kw) {
					agentCommitments = append(agentCommitments, body)
					break
				}
			}
		}
	}

	// 1. Customer Issues
	customerIssues := "No recent customer inquiry or pending issues detected."
	if len(inboundBodies) > 0 {
		if len(inboundBodies) == 1 {
			b := inboundBodies[0]
			if len(b) > 160 {
				b = b[:157] + "..."
			}
			customerIssues = fmt.Sprintf("Customer requested: %q", b)
		} else {
			lastInbound := inboundBodies[len(inboundBodies)-1]
			firstInbound := inboundBodies[0]
			if len(firstInbound) > 80 {
				firstInbound = firstInbound[:77] + "..."
			}
			if len(lastInbound) > 80 {
				lastInbound = lastInbound[:77] + "..."
			}
			if firstInbound == lastInbound {
				customerIssues = fmt.Sprintf("Customer inquiry: %q", lastInbound)
			} else {
				customerIssues = fmt.Sprintf("Initial inquiry: %q | Latest inquiry: %q", firstInbound, lastInbound)
			}
		}
	}

	// 2. Promises Made
	promisesMade := "No pending commitments or deadlines recorded by the team."
	if len(agentCommitments) > 0 {
		lastCommitment := agentCommitments[len(agentCommitments)-1]
		if len(lastCommitment) > 160 {
			lastCommitment = lastCommitment[:157] + "..."
		}
		promisesMade = fmt.Sprintf("Team commitment: %q", lastCommitment)
	} else if len(allAgentBodies) > 0 {
		lastAgentMsg := allAgentBodies[len(allAgentBodies)-1]
		if len(lastAgentMsg) > 160 {
			lastAgentMsg = lastAgentMsg[:157] + "..."
		}
		promisesMade = fmt.Sprintf("In progress; latest agent reply: %q", lastAgentMsg)
	}

	// 3. Current Status
	currentStatus := "Open chat with no recent messages."
	if chat != nil && chat.EffectiveStatus() == ChatStatusClosed {
		currentStatus = "Chat resolved and closed."
	} else if len(messages) > 0 {
		lastMsg := messages[len(messages)-1]
		if lastMsg.Direction == DirectionInbound || lastMsg.SenderType == SenderTypeContact {
			currentStatus = "Awaiting team response (latest message received from customer)."
		} else {
			currentStatus = "Awaiting customer response (team replied recently)."
		}
	}

	if chat != nil {
		if chat.IsServiceWindowOpen() {
			currentStatus += " [24h WABA window active]"
		} else if chat.ServiceWindowExpiresAt != nil {
			currentStatus += " [24h WABA window expired]"
		}
	}

	return ChatSynopsis{
		CustomerIssues: customerIssues,
		PromisesMade:   promisesMade,
		CurrentStatus:  currentStatus,
	}
}

// OutboundChatMessageParams holds the inputs needed to construct a persisted outbound ChatMessage.
type OutboundChatMessageParams struct {
	ID             uuid.UUID
	ChatID         uuid.UUID
	WorkspaceID    uuid.UUID
	UID            string
	SenderType     SenderType
	SenderName     string
	SenderIdentity string
	To             string
	Channel        string
	Body           string
	TemplateName   string
	Language       string
	Type           string
	Media          *Media
	Product        *ProductPayload
	ReplyToUID     *string
	Metadata       map[string]string
	CreatedAt      time.Time
}

// BuildOutboundChatMessage constructs a canonical outbound ChatMessage with populated metadata and template display body.
func BuildOutboundChatMessage(p OutboundChatMessageParams) *ChatMessage {
	id := p.ID
	if id == uuid.Nil {
		id = uuid.New()
	}

	bodyText := p.Body
	if bodyText == "" && p.TemplateName != "" {
		if p.Language != "" {
			bodyText = fmt.Sprintf("[Template: %s (%s)]", p.TemplateName, p.Language)
		} else {
			bodyText = fmt.Sprintf("[Template: %s]", p.TemplateName)
		}
	}

	var mediaURL, mediaType *string
	if p.Media != nil {
		if p.Media.MediaURL != "" {
			u := p.Media.MediaURL
			mediaURL = &u
		}
		if p.Media.MediaType != "" {
			t := p.Media.MediaType
			mediaType = &t
		}
	}

	msgMeta := make(map[string]interface{})
	for k, v := range p.Metadata {
		msgMeta[k] = v
	}
	if p.Channel != "" {
		if _, ok := msgMeta["channel"]; !ok {
			msgMeta["channel"] = p.Channel
		}
	}
	if p.SenderIdentity != "" {
		if _, ok := msgMeta["sender_identity"]; !ok {
			msgMeta["sender_identity"] = p.SenderIdentity
		}
	}
	if p.To != "" {
		if _, ok := msgMeta["to"]; !ok {
			msgMeta["to"] = p.To
		}
	}
	if p.TemplateName != "" {
		msgMeta["template_name"] = p.TemplateName
		if p.Language != "" || msgMeta["language"] == nil {
			msgMeta["language"] = p.Language
		}
		if _, ok := msgMeta["type"]; !ok {
			msgMeta["type"] = "template"
		}
	} else if p.Type != "" {
		if _, ok := msgMeta["type"]; !ok {
			msgMeta["type"] = p.Type
		}
	}
	if p.Product != nil {
		if prodBytes, err := json.Marshal(p.Product); err == nil {
			msgMeta["product_json"] = string(prodBytes)
		}
	}

	return &ChatMessage{
		ID:          id,
		ChatID:      p.ChatID,
		WorkspaceID: p.WorkspaceID,
		UID:         p.UID,
		Direction:   DirectionOutbound,
		SenderType:  p.SenderType,
		SenderName:  p.SenderName,
		SenderID:    p.SenderIdentity,
		Body:        bodyText,
		MediaURL:    mediaURL,
		MediaType:   mediaType,
		IsPrivate:   false,
		Reactions:   []Reaction{},
		ReplyToUID:  p.ReplyToUID,
		Metadata:    msgMeta,
		CreatedAt:   p.CreatedAt,
	}
}

// ChatState is the pure value snapshot of a Chat's activity-driven state.
type ChatState struct {
	ID                     uuid.UUID
	WorkspaceID            uuid.UUID
	ConnectionID           *uuid.UUID
	ContactID              uuid.UUID
	Status                 ChatStatus
	SnoozedUntil           *time.Time
	AI                     AIEngagement
	ServiceWindowExpiresAt *time.Time
	UnreadCount            int
	LastMessageAt          time.Time
	LastInboundAt          *time.Time
}

// Activity is a domain event applied to a ChatState via Apply.
type Activity interface {
	isActivity()
}

// InboundTurn represents an inbound customer message turn on a Chat.
type InboundTurn struct {
	ID         uuid.UUID
	At         time.Time
	UID        string
	SenderName string
	SenderID   string
	ReplyToUID *string
	Body       string
	MediaURL   *string
	MediaType  *string
	Channel    string
	Metadata   map[string]interface{}
}

func (InboundTurn) isActivity() {}

// InboundActivity is an alias for InboundTurn.
type InboundActivity = InboundTurn

// OutboundActivity represents an outbound message turn on a Chat.
type OutboundActivity struct {
	ID           uuid.UUID
	At           time.Time
	UID          string
	SenderType   SenderType
	SenderName   string
	SenderID     string
	ReplyToUID   *string
	Body         string
	Media        *Media
	Product      *ProductPayload
	TemplateName string
	Language     string
	MessageType  string
	Channel      string
	To           string
	Metadata     map[string]string
}

func (OutboundActivity) isActivity() {}

// AIChangeActivity represents an explicit AI Engagement state transition on a Chat.
type AIChangeActivity struct {
	At          time.Time
	Mode        AIMode
	PausedUntil *time.Time
	TraceID     string
}

func (AIChangeActivity) isActivity() {}

// IntentKind identifies a post-commit side-effect produced by Apply.
type IntentKind string

const (
	IntentCancelDebounce IntentKind = "cancel_debounce"
	IntentPublishHandoff IntentKind = "publish_handoff"
)

// Intent describes a post-commit effect to execute after persisting a ChatState transition.
type Intent struct {
	Kind        IntentKind
	WorkspaceID uuid.UUID
	ChatID      uuid.UUID
	ContactID   uuid.UUID
	SenderName  string
	TraceID     string
	At          time.Time
}

// Apply is the pure state machine transition function for Chat Activity.
// Given a ChatState snapshot and an Activity, it returns the next ChatState,
// an optional ChatMessage to persist, and post-commit Intents.
func Apply(s ChatState, a Activity) (ChatState, *ChatMessage, []Intent) {
	next := s
	if next.AI.Mode == "" {
		next.AI.Mode = AIModeActive
	}

	switch act := a.(type) {
	case InboundTurn:
		at := act.At.UTC()
		if at.IsZero() {
			at = time.Now().UTC()
		}

		if next.Status == ChatStatusSnoozed || next.Status == ChatStatusClosed || next.Status == "" {
			next.Status = ChatStatusOpen
			next.SnoozedUntil = nil
		}
		next.UnreadCount++
		if at.After(next.LastMessageAt) {
			next.LastMessageAt = at
		}
		if next.LastInboundAt == nil || at.After(*next.LastInboundAt) {
			inAt := at
			next.LastInboundAt = &inAt
		}
		duration := 24 * time.Hour
		if act.Metadata != nil && (act.Metadata["entry_point_type"] == "ctwa" || act.Metadata["is_referral"] == "true") {
			duration = 72 * time.Hour
		}
		windowEnd := at.Add(duration)
		if next.ServiceWindowExpiresAt == nil || windowEnd.After(*next.ServiceWindowExpiresAt) {
			next.ServiceWindowExpiresAt = &windowEnd
		}

		msgID := act.ID
		if msgID == uuid.Nil {
			msgID = uuid.New()
		}
		msgMeta := make(map[string]interface{})
		for k, v := range act.Metadata {
			msgMeta[k] = v
		}
		if act.Channel != "" {
			if _, ok := msgMeta["channel"]; !ok {
				msgMeta["channel"] = act.Channel
			}
		}

		msg := &ChatMessage{
			ID:          msgID,
			ChatID:      s.ID,
			WorkspaceID: s.WorkspaceID,
			UID:         act.UID,
			Direction:   DirectionInbound,
			SenderType:  SenderTypeContact,
			SenderName:  act.SenderName,
			SenderID:    act.SenderID,
			Body:        act.Body,
			MediaURL:    act.MediaURL,
			MediaType:   act.MediaType,
			IsPrivate:   false,
			Reactions:   []Reaction{},
			ReplyToUID:  act.ReplyToUID,
			Metadata:    msgMeta,
			CreatedAt:   at,
		}
		return next, msg, nil

	case OutboundActivity:
		at := act.At.UTC()
		if at.IsZero() {
			at = time.Now().UTC()
		}

		if next.Status == ChatStatusSnoozed || next.Status == ChatStatusClosed || next.Status == "" {
			next.Status = ChatStatusOpen
			next.SnoozedUntil = nil
		}
		if at.After(next.LastMessageAt) {
			next.LastMessageAt = at
		}

		senderType := NormalizeSenderType(string(act.SenderType))
		if senderType == "" {
			senderType = SenderTypeAIAgent
		}

		var intents []Intent
		if senderType == SenderTypeHumanAgent {
			eff := s.AI.Effective(at)
			wasEngaged := eff.IsEngaged()

			if eff.Mode == AIModeOff {
				next.AI = AIEngagement{Mode: AIModeOff}
			} else {
				until := at.Add(12 * time.Hour)
				if eff.Mode == AIModePaused && eff.PausedUntil != nil && eff.PausedUntil.After(until) {
					until = *eff.PausedUntil
				}
				next.AI = AIEngagement{Mode: AIModePaused, PausedUntil: &until}
			}

			if wasEngaged && !next.AI.Effective(at).IsEngaged() {
				intents = append(intents,
					Intent{
						Kind:        IntentCancelDebounce,
						WorkspaceID: s.WorkspaceID,
						ChatID:      s.ID,
						ContactID:   s.ContactID,
						SenderName:  act.SenderName,
						TraceID:     act.UID,
						At:          at,
					},
					Intent{
						Kind:        IntentPublishHandoff,
						WorkspaceID: s.WorkspaceID,
						ChatID:      s.ID,
						ContactID:   s.ContactID,
						SenderName:  act.SenderName,
						TraceID:     act.UID,
						At:          at,
					},
				)
			}
		}

		msg := BuildOutboundChatMessage(OutboundChatMessageParams{
			ID:             act.ID,
			ChatID:         s.ID,
			WorkspaceID:    s.WorkspaceID,
			UID:            act.UID,
			SenderType:     senderType,
			SenderName:     act.SenderName,
			SenderIdentity: act.SenderID,
			To:             act.To,
			Channel:        act.Channel,
			Body:           act.Body,
			TemplateName:   act.TemplateName,
			Language:       act.Language,
			Type:           act.MessageType,
			Media:          act.Media,
			Product:        act.Product,
			ReplyToUID:     act.ReplyToUID,
			Metadata:       act.Metadata,
			CreatedAt:      at,
		})
		if msg.Metadata == nil {
			msg.Metadata = make(map[string]interface{})
		}
		msg.Metadata["status"] = "queued"

		return next, msg, intents

	case AIChangeActivity:
		at := act.At.UTC()
		if at.IsZero() {
			at = time.Now().UTC()
		}

		if EffectiveStatus(next.Status, next.SnoozedUntil, at) == ChatStatusOpen {
			next.Status = ChatStatusOpen
			next.SnoozedUntil = nil
		}

		eff := s.AI.Effective(at)
		wasEngaged := eff.IsEngaged()

		switch act.Mode {
		case AIModeOff:
			next.AI = AIEngagement{Mode: AIModeOff}
		case AIModePaused:
			if eff.Mode == AIModeOff {
				next.AI = AIEngagement{Mode: AIModeOff}
			} else if act.PausedUntil != nil {
				u := act.PausedUntil.UTC()
				next.AI = AIEngagement{Mode: AIModePaused, PausedUntil: &u}
			} else {
				u := at.Add(12 * time.Hour)
				next.AI = AIEngagement{Mode: AIModePaused, PausedUntil: &u}
			}
		default:
			next.AI = AIEngagement{Mode: AIModeActive}
		}

		var intents []Intent
		nowEngaged := next.AI.Effective(at).IsEngaged()
		if !nowEngaged {
			intents = append(intents, Intent{
				Kind:        IntentCancelDebounce,
				WorkspaceID: s.WorkspaceID,
				ChatID:      s.ID,
				ContactID:   s.ContactID,
				TraceID:     act.TraceID,
				At:          at,
			})
			if wasEngaged {
				intents = append(intents, Intent{
					Kind:        IntentPublishHandoff,
					WorkspaceID: s.WorkspaceID,
					ChatID:      s.ID,
					ContactID:   s.ContactID,
					TraceID:     act.TraceID,
					At:          at,
				})
			}
		}
		return next, nil, intents

	default:
		return next, nil, nil
	}
}



