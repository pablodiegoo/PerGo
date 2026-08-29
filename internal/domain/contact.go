package domain

import (
	"encoding/csv"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Contact represents a workspace-scoped unified customer profile.
type Contact struct {
	ID          uuid.UUID         `json:"id"`
	WorkspaceID uuid.UUID         `json:"workspace_id"`
	Name        string            `json:"name"`
	Email       *string           `json:"email,omitempty"`
	Attributes  map[string]string `json:"attributes"`
	Tags        []string          `json:"tags"`
	ClosedAt    *time.Time        `json:"closed_at,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	Identities  []ContactIdentity `json:"identities,omitempty"`
	BotActive   bool              `json:"bot_active"`
	BotPausedAt *time.Time        `json:"bot_paused_at,omitempty"`
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

// WriteContactsCSV serializes contacts into CSV format without database dependencies.
// Custom attributes (if present) are dynamically appended as trailing columns sorted alphabetically.
func WriteContactsCSV(w io.Writer, contacts []Contact) error {
	writer := csv.NewWriter(w)

	// Collect unique custom attribute keys across all contacts
	attrKeyMap := make(map[string]struct{})
	for _, c := range contacts {
		for k := range c.Attributes {
			attrKeyMap[k] = struct{}{}
		}
	}

	var attrKeys []string
	for k := range attrKeyMap {
		attrKeys = append(attrKeys, k)
	}
	sort.Strings(attrKeys)

	headers := []string{"id", "name", "email", "channel", "sender_identity", "tags", "created_at"}
	headers = append(headers, attrKeys...)
	if err := writer.Write(headers); err != nil {
		return err
	}

	for _, contact := range contacts {
		var identityStr string
		var channelStr string
		if len(contact.Identities) > 0 {
			channelStr = contact.Identities[0].Channel
			identityStr = contact.Identities[0].SenderIdentity
		}
		email := ""
		if contact.Email != nil {
			email = *contact.Email
		}
		tagStr := strings.Join(contact.Tags, ",")
		var createdAtStr string
		if !contact.CreatedAt.IsZero() {
			createdAtStr = contact.CreatedAt.Format(time.RFC3339)
		}

		row := []string{
			contact.ID.String(),
			contact.Name,
			email,
			channelStr,
			identityStr,
			tagStr,
			createdAtStr,
		}

		for _, k := range attrKeys {
			val := ""
			if contact.Attributes != nil {
				val = contact.Attributes[k]
			}
			row = append(row, val)
		}

		if err := writer.Write(row); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}
