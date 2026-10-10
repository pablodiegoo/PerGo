package domain

import (
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

// ChatStatus represents the lifecycle state of a conversation.
type ChatStatus string

const (
	ChatStatusOpen   ChatStatus = "open"
	ChatStatusClosed ChatStatus = "closed"
)

// Reaction represents an emoji reaction attached to a message.
type Reaction struct {
	Emoji     string    `json:"emoji"`
	Sender    string    `json:"sender"`
	CreatedAt time.Time `json:"created_at"`
}

// Chat represents a conversational thread between a workspace connection and a contact.
type Chat struct {
	ID                     uuid.UUID              `json:"id"`
	WorkspaceID            uuid.UUID              `json:"workspace_id"`
	ConnectionID           *uuid.UUID             `json:"connection_id,omitempty"`
	ContactID              uuid.UUID              `json:"contact_id"`
	Status                 string                 `json:"status"` // "open", "closed"
	AssignedUserID         *uuid.UUID             `json:"assigned_user_id,omitempty"`
	AssignedEmail          *string                `json:"assigned_email,omitempty"`
	Tags                   []string               `json:"tags"`
	UnreadCount            int                    `json:"unread_count"`
	AIDisabled             bool                   `json:"ai_disabled"`
	ServiceWindowExpiresAt *time.Time             `json:"service_window_expires_at,omitempty"`
	ServiceWindowIsOpen    bool                   `json:"service_window_is_open"`
	LastMessageAt          time.Time              `json:"last_message_at"`
	Metadata               map[string]interface{} `json:"metadata"`
	CreatedAt              time.Time              `json:"created_at"`
	UpdatedAt              time.Time              `json:"updated_at"`
}

// IsServiceWindowOpen reports whether the customer service window is active.
func (c *Chat) IsServiceWindowOpen() bool {
	if c == nil || c.ServiceWindowExpiresAt == nil {
		return false
	}
	return time.Now().UTC().Before(*c.ServiceWindowExpiresAt)
}

// ChatMessage represents a single message, dispatch, or internal note within a Chat.
type ChatMessage struct {
	ID          uuid.UUID              `json:"id"`
	ChatID      uuid.UUID              `json:"chat_id"`
	WorkspaceID uuid.UUID              `json:"workspace_id"`
	UID         string                 `json:"uid"`
	Direction   string                 `json:"direction"`
	SenderType  string                 `json:"sender_type"`
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
	Status    string     `json:"status,omitempty"`
	Unread    *bool      `json:"unread,omitempty"`
	Phone     string     `json:"phone,omitempty"`
	ContactID *uuid.UUID `json:"contact_id,omitempty"`
}
