package domain

import "github.com/google/uuid"

// SessionKey encapsulates the identifier tuple for a recipient communication session across a workspace and channel.
type SessionKey struct {
	WorkspaceID       uuid.UUID `json:"workspace_id"`
	RecipientPhone    string    `json:"recipient_phone"`
	Channel           string    `json:"channel"`
	RecipientIdentity string    `json:"recipient_identity"`
}

// NewSessionKey creates a new SessionKey value object.
func NewSessionKey(workspaceID uuid.UUID, recipientPhone, channel, recipientIdentity string) SessionKey {
	return SessionKey{
		WorkspaceID:       workspaceID,
		RecipientPhone:    recipientPhone,
		Channel:           channel,
		RecipientIdentity: recipientIdentity,
	}
}
