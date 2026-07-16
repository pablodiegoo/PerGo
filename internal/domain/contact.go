package domain

import (
	"time"

	"github.com/google/uuid"
)

// Contact represents a workspace-scoped unified customer profile.
type Contact struct {
	ID          uuid.UUID         `json:"id"`
	WorkspaceID uuid.UUID         `json:"workspace_id"`
	Name        string            `json:"name"`
	Email       *string           `json:"email,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	Identities  []ContactIdentity `json:"identities,omitempty"`
}

// ContactIdentity represents a channel-specific identity linked to a Contact.
type ContactIdentity struct {
	ID             uuid.UUID `json:"id"`
	ContactID      uuid.UUID `json:"contact_id"`
	WorkspaceID    uuid.UUID `json:"workspace_id"`
	Channel        string    `json:"channel"`
	SenderIdentity string    `json:"sender_identity"`
	CreatedAt      time.Time `json:"created_at"`
}
