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

// Upsert registers or updates a Telegram contact mapping.
func (r *TelegramContactRepository) Upsert(ctx context.Context, workspaceID uuid.UUID, chatID string, username, phoneNumber, firstName, lastName *string) error {
	var normalizedUsername *string
	if username != nil {
		u := strings.TrimPrefix(*username, "@")
		if u != "" {
			normalizedUsername = &u
		}
	}

	_, err := r.pool.Exec(ctx, `
		INSERT INTO telegram_contacts (workspace_id, chat_id, username, phone_number, first_name, last_name, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())
		ON CONFLICT (workspace_id, chat_id)
		DO UPDATE SET 
			username = COALESCE(EXCLUDED.username, telegram_contacts.username),
			phone_number = COALESCE(EXCLUDED.phone_number, telegram_contacts.phone_number),
			first_name = COALESCE(EXCLUDED.first_name, telegram_contacts.first_name),
			last_name = COALESCE(EXCLUDED.last_name, telegram_contacts.last_name),
			updated_at = NOW()
	`, workspaceID, chatID, normalizedUsername, phoneNumber, firstName, lastName)
	return err
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

	// Look up by username (stripped of leading @)
	usernameQuery := strings.TrimPrefix(clean, "@")
	var chatID string
	err := r.pool.QueryRow(ctx, `
		SELECT chat_id 
		FROM telegram_contacts 
		WHERE workspace_id = $1 AND (LOWER(username) = LOWER($2) OR phone_number = $3)
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
	var c TelegramContact
	c.WorkspaceID = workspaceID
	c.ChatID = chatID

	err := r.pool.QueryRow(ctx, `
		SELECT username, phone_number, first_name, last_name 
		FROM telegram_contacts 
		WHERE workspace_id = $1 AND chat_id = $2
	`, workspaceID, chatID).Scan(&c.Username, &c.PhoneNumber, &c.FirstName, &c.LastName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTelegramContactNotFound
		}
		return nil, err
	}

	return &c, nil
}

