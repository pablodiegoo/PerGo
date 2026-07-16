package repository

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrTelegramContactNotFound is returned when a contact mapping cannot be found.
var ErrTelegramContactNotFound = errors.New("telegram contact mapping not found")

// TelegramContact represents a mapped Telegram user.
type TelegramContact struct {
	WorkspaceID uuid.UUID
	ChatID      string
	Username    *string
	PhoneNumber *string
	FirstName   *string
	LastName    *string
}

// TelegramContactRepository provides operations to resolve usernames/phones to Telegram chat IDs.
type TelegramContactRepository struct {
	pool *pgxpool.Pool
}

// NewTelegramContactRepository creates a new TelegramContactRepository.
func NewTelegramContactRepository(pool *pgxpool.Pool) *TelegramContactRepository {
	return &TelegramContactRepository{pool: pool}
}

// Upsert registers or updates a Telegram contact mapping using contacts and contact_identities tables.
func (r *TelegramContactRepository) Upsert(ctx context.Context, workspaceID uuid.UUID, chatID string, username, phoneNumber, firstName, lastName *string) error {
	var normalizedUsername *string
	if username != nil {
		u := strings.TrimPrefix(*username, "@")
		if u != "" {
			normalizedUsername = &u
		}
	}

	// Build contact name
	var nameParts []string
	if firstName != nil && *firstName != "" {
		nameParts = append(nameParts, *firstName)
	}
	if lastName != nil && *lastName != "" {
		nameParts = append(nameParts, *lastName)
	}
	name := strings.TrimSpace(strings.Join(nameParts, " "))
	if name == "" {
		if normalizedUsername != nil && *normalizedUsername != "" {
			name = *normalizedUsername
		} else {
			name = chatID
		}
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Check if identity already exists
	var contactID uuid.UUID
	err = tx.QueryRow(ctx, `
		SELECT contact_id FROM contact_identities 
		WHERE workspace_id = $1 AND channel = 'telegram' AND sender_identity = $2
	`, workspaceID, chatID).Scan(&contactID)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Create a new contact
			contactID = uuid.New()
			_, err = tx.Exec(ctx, `
				INSERT INTO contacts (id, workspace_id, name, created_at, updated_at)
				VALUES ($1, $2, $3, NOW(), NOW())
			`, contactID, workspaceID, name)
			if err != nil {
				return err
			}

			// Link primary telegram identity
			_, err = tx.Exec(ctx, `
				INSERT INTO contact_identities (contact_id, workspace_id, channel, sender_identity, created_at)
				VALUES ($1, $2, 'telegram', $3, NOW())
			`, contactID, workspaceID, chatID)
			if err != nil {
				return err
			}
		} else {
			return err
		}
	} else {
		// Update contact name
		_, err = tx.Exec(ctx, `
			UPDATE contacts 
			SET name = $1, updated_at = NOW() 
			WHERE id = $2 AND workspace_id = $3
		`, name, contactID, workspaceID)
		if err != nil {
			return err
		}
	}

	// Link telegram username if present
	if normalizedUsername != nil && *normalizedUsername != "" {
		cleanUser := strings.ToLower(*normalizedUsername)
		_, err = tx.Exec(ctx, `
			INSERT INTO contact_identities (contact_id, workspace_id, channel, sender_identity, created_at)
			VALUES ($1, $2, 'telegram_username', $3, NOW())
			ON CONFLICT (workspace_id, channel, sender_identity) DO NOTHING
		`, contactID, workspaceID, cleanUser)
		if err != nil {
			return err
		}
	}

	// Link phone number if present
	if phoneNumber != nil && *phoneNumber != "" {
		_, err = tx.Exec(ctx, `
			INSERT INTO contact_identities (contact_id, workspace_id, channel, sender_identity, created_at)
			VALUES ($1, $2, 'phone', $3, NOW())
			ON CONFLICT (workspace_id, channel, sender_identity) DO NOTHING
		`, contactID, workspaceID, *phoneNumber)
		if err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

// Resolve translates a username, phone number, or numeric ID into a valid Telegram chat ID.
func (r *TelegramContactRepository) Resolve(ctx context.Context, workspaceID uuid.UUID, identifier string) (string, error) {
	clean := strings.TrimSpace(identifier)
	if clean == "" {
		return "", ErrTelegramContactNotFound
	}

	// If already a raw numeric chat ID (positive/negative), return as-is
	isNumeric := true
	checkStr := clean
	if strings.HasPrefix(checkStr, "-") {
		checkStr = checkStr[1:]
	}
	if checkStr == "" {
		isNumeric = false
	} else {
		for _, c := range checkStr {
			if c < '0' || c > '9' {
				isNumeric = false
				break
			}
		}
	}
	if isNumeric {
		return clean, nil
	}

	// Look up by username (stripped of leading @) or phone number
	usernameQuery := strings.ToLower(strings.TrimPrefix(clean, "@"))
	var chatID string
	err := r.pool.QueryRow(ctx, `
		SELECT ci_tg.sender_identity
		FROM contact_identities ci_tg
		JOIN contact_identities ci_lookup ON ci_lookup.contact_id = ci_tg.contact_id
		WHERE ci_tg.workspace_id = $1
		  AND ci_tg.channel = 'telegram'
		  AND (
		      (ci_lookup.channel = 'telegram_username' AND LOWER(ci_lookup.sender_identity) = $2)
		      OR (ci_lookup.channel = 'phone' AND ci_lookup.sender_identity = $3)
		  )
		LIMIT 1
	`, workspaceID, usernameQuery, clean).Scan(&chatID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrTelegramContactNotFound
		}
		return "", err
	}

	return chatID, nil
}

// Get retrieves a TelegramContact by its chat ID.
func (r *TelegramContactRepository) Get(ctx context.Context, workspaceID uuid.UUID, chatID string) (*TelegramContact, error) {
	var contactName string
	var username, phone string
	var contactID uuid.UUID

	err := r.pool.QueryRow(ctx, `
		SELECT c.id, c.name
		FROM contacts c
		JOIN contact_identities ci ON ci.contact_id = c.id
		WHERE ci.workspace_id = $1 AND ci.channel = 'telegram' AND ci.sender_identity = $2
	`, workspaceID, chatID).Scan(&contactID, &contactName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTelegramContactNotFound
		}
		return nil, err
	}

	rows, err := r.pool.Query(ctx, `
		SELECT channel, sender_identity 
		FROM contact_identities 
		WHERE workspace_id = $1 AND contact_id = $2
	`, workspaceID, contactID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var chanName, identity string
		if err := rows.Scan(&chanName, &identity); err != nil {
			return nil, err
		}
		if chanName == "telegram_username" {
			username = identity
		} else if chanName == "phone" {
			phone = identity
		}
	}

	var uPtr, pPtr *string
	if username != "" {
		uPtr = &username
	}
	if phone != "" {
		pPtr = &phone
	}

	var firstName, lastName string
	parts := strings.SplitN(contactName, " ", 2)
	if len(parts) > 0 {
		firstName = parts[0]
	}
	if len(parts) > 1 {
		lastName = parts[1]
	}

	var fPtr, lPtr *string
	if firstName != "" {
		fPtr = &firstName
	}
	if lastName != "" {
		lPtr = &lastName
	}

	return &TelegramContact{
		WorkspaceID: workspaceID,
		ChatID:      chatID,
		Username:    uPtr,
		PhoneNumber: pPtr,
		FirstName:   fPtr,
		LastName:    lPtr,
	}, nil
}
