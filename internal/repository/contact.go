package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pablojhp.pergo/internal/domain"
)

var ErrContactNotFound = errors.New("contact not found")

type ContactRepository struct {
	pool *pgxpool.Pool
}

func NewContactRepository(pool *pgxpool.Pool) *ContactRepository {
	return &ContactRepository{pool: pool}
}

// GetByID loads a contact and all its associated identities.
func (r *ContactRepository) GetByID(ctx context.Context, workspaceID, contactID uuid.UUID) (*domain.Contact, error) {
	var c domain.Contact
	var attrsRaw []byte
	err := r.pool.QueryRow(ctx, `
		SELECT id, workspace_id, name, email, attributes, tags, closed_at, created_at, updated_at, bot_active, bot_paused_at
		FROM contacts WHERE workspace_id = $1 AND id = $2
	`, workspaceID, contactID).Scan(&c.ID, &c.WorkspaceID, &c.Name, &c.Email, &attrsRaw, &c.Tags, &c.ClosedAt, &c.CreatedAt, &c.UpdatedAt, &c.BotActive, &c.BotPausedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrContactNotFound
		}
		return nil, err
	}

	if len(attrsRaw) > 0 {
		if err := json.Unmarshal(attrsRaw, &c.Attributes); err != nil {
			return nil, fmt.Errorf("unmarshal contact attributes: %w", err)
		}
	}
	if c.Attributes == nil {
		c.Attributes = make(map[string]string)
	}

	rows, err := r.pool.Query(ctx, `
		SELECT id, contact_id, workspace_id, channel, sender_identity, created_at
		FROM contact_identities WHERE workspace_id = $1 AND contact_id = $2
	`, workspaceID, contactID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var ci domain.ContactIdentity
		if err := rows.Scan(&ci.ID, &ci.ContactID, &ci.WorkspaceID, &ci.Channel, &ci.SenderIdentity, &ci.CreatedAt); err != nil {
			return nil, err
		}
		c.Identities = append(c.Identities, ci)
	}

	return &c, nil
}

// ResolveContact maps an incoming channel/sender combination to an existing contact, or inserts a new profile if unmapped.
func (r *ContactRepository) ResolveContact(
	ctx context.Context,
	workspaceID uuid.UUID,
	channel, senderIdentity, name, username, phone string,
) (*domain.Contact, error) {
	// 1. Try to find the identity directly
	var contactID uuid.UUID
	err := r.pool.QueryRow(ctx, `
		SELECT contact_id FROM contact_identities 
		WHERE workspace_id = $1 AND channel = $2 AND sender_identity = $3
	`, workspaceID, channel, senderIdentity).Scan(&contactID)
	
	if err == nil {
		_, err = r.pool.Exec(ctx, `
			UPDATE contacts SET closed_at = NULL, updated_at = NOW() WHERE id = $1 AND closed_at IS NOT NULL
		`, contactID)
		if err != nil {
			return nil, err
		}
		return r.GetByID(ctx, workspaceID, contactID)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	// 2. Not found. We open a transaction to concurrently protect resolving/creating.
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// Check again in TX to avoid duplicate insert race conditions
	err = tx.QueryRow(ctx, `
		SELECT contact_id FROM contact_identities 
		WHERE workspace_id = $1 AND channel = $2 AND sender_identity = $3
	`, workspaceID, channel, senderIdentity).Scan(&contactID)

	if err == nil {
		_, err = tx.Exec(ctx, `
			UPDATE contacts SET closed_at = NULL, updated_at = NOW() WHERE id = $1 AND closed_at IS NOT NULL
		`, contactID)
		if err != nil {
			return nil, err
		}
		_ = tx.Commit(ctx)
		return r.GetByID(ctx, workspaceID, contactID)
	}

	// Try cross-linking check: check if username or phone are already linked to a contact
	if channel == "telegram" && username != "" {
		cleanUser := strings.ToLower(strings.TrimPrefix(username, "@"))
		_ = tx.QueryRow(ctx, `
			SELECT contact_id FROM contact_identities 
			WHERE workspace_id = $1 AND channel = 'telegram_username' AND LOWER(sender_identity) = $2
		`, workspaceID, cleanUser).Scan(&contactID)
	}

	if contactID == uuid.Nil && phone != "" {
		_ = tx.QueryRow(ctx, `
			SELECT contact_id FROM contact_identities 
			WHERE workspace_id = $1 AND (channel = 'phone' OR channel = 'whatsapp' OR channel = 'whatsapp_cloud') AND sender_identity = $2
		`, workspaceID, phone).Scan(&contactID)
	}

	// If still no contact matched, we create a new one
	if contactID == uuid.Nil {
		contactID = uuid.New()
		displayName := name
		if displayName == "" {
			displayName = senderIdentity
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO contacts (id, workspace_id, name, created_at, updated_at)
			VALUES ($1, $2, $3, NOW(), NOW())
		`, contactID, workspaceID, displayName)
		if err != nil {
			return nil, fmt.Errorf("create contact profile: %w", err)
		}
	} else {
		// Reset closed_at if we matched an existing contact via cross-linking
		_, err = tx.Exec(ctx, `
			UPDATE contacts SET closed_at = NULL, updated_at = NOW() WHERE id = $1 AND closed_at IS NOT NULL
		`, contactID)
		if err != nil {
			return nil, err
		}
	}

	// Link primary identity
	_, err = tx.Exec(ctx, `
		INSERT INTO contact_identities (contact_id, workspace_id, channel, sender_identity, created_at)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (workspace_id, channel, sender_identity) DO NOTHING
	`, contactID, workspaceID, channel, senderIdentity)
	if err != nil {
		return nil, fmt.Errorf("link contact identity: %w", err)
	}

	// Link username (for telegram)
	if channel == "telegram" && username != "" {
		cleanUser := strings.ToLower(strings.TrimPrefix(username, "@"))
		_, _ = tx.Exec(ctx, `
			INSERT INTO contact_identities (contact_id, workspace_id, channel, sender_identity, created_at)
			VALUES ($1, $2, 'telegram_username', $3, NOW())
			ON CONFLICT (workspace_id, channel, sender_identity) DO NOTHING
		`, contactID, workspaceID, cleanUser)
	}

	// Link phone
	if phone != "" {
		_, _ = tx.Exec(ctx, `
			INSERT INTO contact_identities (contact_id, workspace_id, channel, sender_identity, created_at)
			VALUES ($1, $2, 'phone', $3, NOW())
			ON CONFLICT (workspace_id, channel, sender_identity) DO NOTHING
		`, contactID, workspaceID, phone)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return r.GetByID(ctx, workspaceID, contactID)
}

func marshalAttributes(attributes map[string]string) ([]byte, error) {
	if attributes == nil {
		attributes = make(map[string]string)
	}
	return json.Marshal(attributes)
}

// CreateContact creates a new contact profile with optional email and custom attributes.
func (r *ContactRepository) CreateContact(
	ctx context.Context,
	workspaceID uuid.UUID,
	name string,
	email *string,
	attributes map[string]string,
) (*domain.Contact, error) {
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("contact name cannot be empty")
	}
	attrsJSON, err := marshalAttributes(attributes)
	if err != nil {
		return nil, fmt.Errorf("marshal contact attributes: %w", err)
	}

	contactID := uuid.New()
	_, err = r.pool.Exec(ctx, `
		INSERT INTO contacts (id, workspace_id, name, email, attributes, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
	`, contactID, workspaceID, name, email, attrsJSON)
	if err != nil {
		return nil, fmt.Errorf("create contact: %w", err)
	}

	return r.GetByID(ctx, workspaceID, contactID)
}

// UpdateAttributes replaces or updates the custom attributes JSON for a contact.
func (r *ContactRepository) UpdateAttributes(
	ctx context.Context,
	workspaceID, contactID uuid.UUID,
	attributes map[string]string,
) error {
	attrsJSON, err := marshalAttributes(attributes)
	if err != nil {
		return fmt.Errorf("marshal contact attributes: %w", err)
	}

	res, err := r.pool.Exec(ctx, `
		UPDATE contacts
		SET attributes = $3, updated_at = NOW()
		WHERE workspace_id = $1 AND id = $2
	`, workspaceID, contactID, attrsJSON)
	if err != nil {
		return fmt.Errorf("update contact attributes: %w", err)
	}
	if res.RowsAffected() == 0 {
		return ErrContactNotFound
	}
	return nil
}

// UpdateContact updates a contact's profile data (name, email, attributes).
func (r *ContactRepository) UpdateContact(ctx context.Context, workspaceID uuid.UUID, contact *domain.Contact) error {
	if contact == nil {
		return errors.New("nil contact")
	}
	attrsJSON, err := marshalAttributes(contact.Attributes)
	if err != nil {
		return fmt.Errorf("marshal contact attributes: %w", err)
	}

	res, err := r.pool.Exec(ctx, `
		UPDATE contacts
		SET name = $3, email = $4, attributes = $5, updated_at = NOW()
		WHERE workspace_id = $1 AND id = $2
	`, workspaceID, contact.ID, contact.Name, contact.Email, attrsJSON)
	if err != nil {
		return fmt.Errorf("update contact: %w", err)
	}
	if res.RowsAffected() == 0 {
		return ErrContactNotFound
	}
	return nil
}

// DeleteContact deletes a contact by ID within a workspace.
func (r *ContactRepository) DeleteContact(ctx context.Context, workspaceID, contactID uuid.UUID) error {
	res, err := r.pool.Exec(ctx, `
		DELETE FROM contacts WHERE workspace_id = $1 AND id = $2
	`, workspaceID, contactID)
	if err != nil {
		return fmt.Errorf("delete contact: %w", err)
	}
	if res.RowsAffected() == 0 {
		return ErrContactNotFound
	}
	return nil
}

// MergeContacts unifies the identities of a secondary contact into the primary contact, deleting the secondary.
func (r *ContactRepository) MergeContacts(ctx context.Context, workspaceID uuid.UUID, primaryID, secondaryID uuid.UUID) error {
	if primaryID == secondaryID {
		return errors.New("cannot merge contact with itself")
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Validate workspaces
	var primWS, secWS uuid.UUID
	err = tx.QueryRow(ctx, "SELECT workspace_id FROM contacts WHERE id = $1", primaryID).Scan(&primWS)
	if err != nil {
		return fmt.Errorf("primary contact not found: %w", err)
	}
	err = tx.QueryRow(ctx, "SELECT workspace_id FROM contacts WHERE id = $1", secondaryID).Scan(&secWS)
	if err != nil {
		return fmt.Errorf("secondary contact not found: %w", err)
	}

	if primWS != workspaceID || secWS != workspaceID {
		return errors.New("contacts must belong to the active workspace")
	}

	// 1. Delete secondary identities that duplicate primary ones to prevent UNIQUE constraint violations
	_, err = tx.Exec(ctx, `
		DELETE FROM contact_identities ci_sec
		WHERE ci_sec.contact_id = $2
		  AND ci_sec.workspace_id = $3
		  AND EXISTS (
		      SELECT 1 FROM contact_identities ci_prim
		      WHERE ci_prim.contact_id = $1
		        AND ci_prim.workspace_id = $3
		        AND ci_prim.channel = ci_sec.channel
		        AND ci_prim.sender_identity = ci_sec.sender_identity
		  )
	`, primaryID, secondaryID, workspaceID)
	if err != nil {
		return fmt.Errorf("delete duplicate identities: %w", err)
	}

	// 2. Update remaining secondary identities to point to primary contact
	_, err = tx.Exec(ctx, `
		UPDATE contact_identities 
		SET contact_id = $1 
		WHERE contact_id = $2 AND workspace_id = $3
	`, primaryID, secondaryID, workspaceID)
	if err != nil {
		return fmt.Errorf("rebind identities: %w", err)
	}

	// 3. Merge custom attributes: secondary's attributes are merged into primary's attributes
	// In jsonb concatenation (left || right), right-hand keys take precedence, so primary keys are preserved.
	_, err = tx.Exec(ctx, `
		UPDATE contacts
		SET attributes = COALESCE((SELECT attributes FROM contacts WHERE id = $2), '{}'::jsonb) || contacts.attributes,
		    updated_at = NOW()
		WHERE id = $1 AND workspace_id = $3
	`, primaryID, secondaryID, workspaceID)
	if err != nil {
		return fmt.Errorf("merge contact attributes: %w", err)
	}

	// 4. Delete secondary contact profile
	_, err = tx.Exec(ctx, "DELETE FROM contacts WHERE id = $1 AND workspace_id = $2", secondaryID, workspaceID)
	if err != nil {
		return fmt.Errorf("delete secondary contact profile: %w", err)
	}

	return tx.Commit(ctx)
}

// SearchContacts performs a type-ahead search matching contacts by name or linked identities in a workspace.
func (r *ContactRepository) SearchContacts(ctx context.Context, workspaceID uuid.UUID, query string, excludeID uuid.UUID, limit int) ([]domain.Contact, error) {
	q := "%" + strings.ToLower(query) + "%"
	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT c.id, c.name, c.email, c.attributes, c.tags, c.closed_at, c.created_at, c.updated_at, c.bot_active, c.bot_paused_at
		FROM contacts c
		LEFT JOIN contact_identities ci ON ci.contact_id = c.id
		WHERE c.workspace_id = $1
		  AND c.id <> $2
		  AND (LOWER(c.name) LIKE $3 OR LOWER(c.email) LIKE $3 OR LOWER(ci.sender_identity) LIKE $3)
		LIMIT $4
	`, workspaceID, excludeID, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []domain.Contact
	for rows.Next() {
		var c domain.Contact
		var attrsRaw []byte
		if err := rows.Scan(&c.ID, &c.Name, &c.Email, &attrsRaw, &c.Tags, &c.ClosedAt, &c.CreatedAt, &c.UpdatedAt, &c.BotActive, &c.BotPausedAt); err != nil {
			return nil, err
		}
		if len(attrsRaw) > 0 {
			if err := json.Unmarshal(attrsRaw, &c.Attributes); err != nil {
				return nil, fmt.Errorf("unmarshal contact attributes: %w", err)
			}
		}
		if c.Attributes == nil {
			c.Attributes = make(map[string]string)
		}
		list = append(list, c)
	}
	return list, nil
}

// ResolveTelegramChatID translates a Telegram username/phone into a numeric chat ID.
func (r *ContactRepository) ResolveTelegramChatID(ctx context.Context, workspaceID uuid.UUID, identifier string) (string, error) {
	clean := strings.TrimSpace(identifier)
	if clean == "" {
		return "", errors.New("empty identifier")
	}

	// If numeric chat ID, return immediately
	isNumeric := true
	checkStr := strings.TrimPrefix(clean, "-")
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
			return "", errors.New("telegram contact identity mapping not found")
		}
		return "", err
	}

	return chatID, nil
}

// HasUnread checks if a contact has any unread sessions in their workspace.
func (r *ContactRepository) HasUnread(ctx context.Context, workspaceID, contactID uuid.UUID) (bool, error) {
	var hasUnread bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 
			FROM contact_identities ci
			JOIN recipient_sessions rs ON rs.workspace_id = ci.workspace_id
				AND rs.recipient_phone = ci.sender_identity
				AND rs.channel = ci.channel
			WHERE ci.workspace_id = $1
			  AND ci.contact_id = $2
			  AND (rs.last_read_at IS NULL OR rs.last_inbound_at > rs.last_read_at)
		)
	`, workspaceID, contactID).Scan(&hasUnread)
	if err != nil {
		return false, fmt.Errorf("check contact unread: %w", err)
	}
	return hasUnread, nil
}

// AddTags appends tags to the contact's tag list while preserving uniqueness.
func (r *ContactRepository) AddTags(ctx context.Context, workspaceID, contactID uuid.UUID, tags []string) error {
	if len(tags) == 0 {
		return nil
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE contacts 
		SET tags = ARRAY(
			SELECT DISTINCT val 
			FROM unnest(array_cat(tags, $3)) val 
			WHERE val IS NOT NULL
		), 
		updated_at = NOW() 
		WHERE workspace_id = $1 AND id = $2
	`, workspaceID, contactID, tags)
	return err
}

// CloseThread sets closed_at to the current timestamp.
func (r *ContactRepository) CloseThread(ctx context.Context, workspaceID, contactID uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE contacts 
		SET closed_at = NOW(), updated_at = NOW() 
		WHERE workspace_id = $1 AND id = $2
	`, workspaceID, contactID)
	return err
}

// UpdateBotState toggles bot activity and sets the paused timestamp for a contact.
func (r *ContactRepository) UpdateBotState(ctx context.Context, workspaceID, contactID uuid.UUID, botActive bool, pausedAt *time.Time) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE contacts 
		SET bot_active = $3, bot_paused_at = $4, updated_at = NOW() 
		WHERE workspace_id = $1 AND id = $2
	`, workspaceID, contactID, botActive, pausedAt)
	return err
}

// FindIdentityForChannel finds an eligible target identity on targetChannel for the given recipient,
// inspecting direct formatting or querying cross-channel contact identities in the workspace.
func (r *ContactRepository) FindIdentityForChannel(ctx context.Context, workspaceID uuid.UUID, recipient string, targetChannel string) (string, bool, error) {
	clean := strings.TrimSpace(recipient)
	if clean == "" {
		return "", false, nil
	}

	// 1. Target: Email or email variants
	if targetChannel == "email" || targetChannel == "email_ses" || targetChannel == "email_smtp" || targetChannel == "email_mautic" {
		if strings.Contains(clean, "@") {
			return clean, true, nil
		}
		var email string
		err := r.pool.QueryRow(ctx, `
			SELECT COALESCE(NULLIF(c.email, ''), ci_email.sender_identity)
			FROM contacts c
			LEFT JOIN contact_identities ci_lookup ON ci_lookup.contact_id = c.id AND ci_lookup.workspace_id = c.workspace_id
			LEFT JOIN contact_identities ci_email ON ci_email.contact_id = c.id AND ci_email.workspace_id = c.workspace_id AND ci_email.channel IN ('email', 'email_ses', 'email_smtp', 'email_mautic')
			WHERE c.workspace_id = $1
			  AND (
			      c.id::text = $2
			      OR c.email = $2
			      OR ci_lookup.sender_identity = $2
			      OR (ci_lookup.channel = 'telegram_username' AND LOWER(ci_lookup.sender_identity) = LOWER($2))
			  )
			  AND ((c.email IS NOT NULL AND c.email <> '') OR ci_email.sender_identity IS NOT NULL)
			LIMIT 1
		`, workspaceID, clean).Scan(&email)
		if err == nil && email != "" {
			return email, true, nil
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return "", false, fmt.Errorf("find identity for channel %s: %w", targetChannel, err)
		}
		return "", false, nil
	}

	// 2. Target: WhatsApp or WhatsApp Cloud
	if targetChannel == "whatsapp" || targetChannel == "whatsapp_cloud" {
		if sanitized, ok := domain.SanitizePhone(clean); ok {
			return sanitized, true, nil
		}
		var phone string
		err := r.pool.QueryRow(ctx, `
			SELECT ci_target.sender_identity
			FROM contact_identities ci_target
			JOIN contacts c ON c.id = ci_target.contact_id AND c.workspace_id = ci_target.workspace_id
			LEFT JOIN contact_identities ci_lookup ON ci_lookup.contact_id = c.id AND ci_lookup.workspace_id = c.workspace_id
			WHERE ci_target.workspace_id = $1
			  AND ci_target.channel IN ('whatsapp', 'whatsapp_cloud', 'phone')
			  AND (
			      c.id::text = $2
			      OR c.email = $2
			      OR ci_lookup.sender_identity = $2
			      OR (ci_lookup.channel = 'telegram_username' AND LOWER(ci_lookup.sender_identity) = LOWER($2))
			  )
			LIMIT 1
		`, workspaceID, clean).Scan(&phone)
		if err == nil && phone != "" {
			return phone, true, nil
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return "", false, fmt.Errorf("find identity for channel %s: %w", targetChannel, err)
		}
		return "", false, nil
	}

	// 3. Target: Telegram
	if targetChannel == "telegram" {
		var tgID string
		err := r.pool.QueryRow(ctx, `
			SELECT ci_target.sender_identity
			FROM contact_identities ci_target
			JOIN contacts c ON c.id = ci_target.contact_id AND c.workspace_id = ci_target.workspace_id
			LEFT JOIN contact_identities ci_lookup ON ci_lookup.contact_id = c.id AND ci_lookup.workspace_id = c.workspace_id
			WHERE ci_target.workspace_id = $1
			  AND ci_target.channel = 'telegram'
			  AND (
			      c.id::text = $2
			      OR c.email = $2
			      OR ci_lookup.sender_identity = $2
			      OR (ci_lookup.channel = 'telegram_username' AND LOWER(ci_lookup.sender_identity) = LOWER($2))
			  )
			LIMIT 1
		`, workspaceID, clean).Scan(&tgID)
		if err == nil && tgID != "" {
			return tgID, true, nil
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return "", false, fmt.Errorf("find identity for channel %s: %w", targetChannel, err)
		}

		var contactExists bool
		_ = r.pool.QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM contacts c
				LEFT JOIN contact_identities ci_lookup ON ci_lookup.contact_id = c.id AND ci_lookup.workspace_id = c.workspace_id
				WHERE c.workspace_id = $1
				  AND (
				      c.id::text = $2
				      OR c.email = $2
				      OR ci_lookup.sender_identity = $2
				  )
			)
		`, workspaceID, clean).Scan(&contactExists)
		if contactExists {
			return "", false, nil
		}

		if strings.HasPrefix(clean, "@") {
			if chatID, err := r.ResolveTelegramChatID(ctx, workspaceID, clean); err == nil && chatID != "" {
				return chatID, true, nil
			}
		}
		return "", false, nil
	}

	// 4. Other channels
	var identity string
	err := r.pool.QueryRow(ctx, `
		SELECT ci_target.sender_identity
		FROM contact_identities ci_target
		JOIN contacts c ON c.id = ci_target.contact_id AND c.workspace_id = ci_target.workspace_id
		LEFT JOIN contact_identities ci_lookup ON ci_lookup.contact_id = c.id AND ci_lookup.workspace_id = c.workspace_id
		WHERE ci_target.workspace_id = $1
		  AND ci_target.channel = $2
		  AND (
		      c.id::text = $3
		      OR c.email = $3
		      OR ci_lookup.sender_identity = $3
		  )
		LIMIT 1
	`, workspaceID, targetChannel, clean).Scan(&identity)
	if err == nil && identity != "" {
		return identity, true, nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", false, fmt.Errorf("find identity for channel %s: %w", targetChannel, err)
	}

	return "", false, nil
}

