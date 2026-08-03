package domain

import (
	"time"

	"github.com/google/uuid"
)

// Tag represents a workspace-scoped label attached to contacts for segment filtering.
type Tag struct {
	ID          uuid.UUID `json:"id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
	Name        string    `json:"name"`
	Color       string    `json:"color"`
	CreatedAt   time.Time `json:"created_at"`
}

// ContactTag represents a dynamic link between a Contact and a Tag.
type ContactTag struct {
	ContactID uuid.UUID `json:"contact_id"`
	TagID     uuid.UUID `json:"tag_id"`
	CreatedAt time.Time `json:"created_at"`
}
