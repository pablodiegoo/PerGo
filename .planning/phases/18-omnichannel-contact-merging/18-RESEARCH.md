# Phase 18 Research: Omnichannel Contact Merging

This document maps out the system architecture, database changes, code adjustments, and UI designs required to implement Phase 18: Omnichannel Contact Merging in PerGo.

---

## 1. Database Schema & Migrations

We will introduce a new migration `024_omnichannel_contact_merging.sql`. It defines two tables: `contacts` and `contact_identities`, migrates all legacy `telegram_contacts` records, backfills existing contacts from historical `audit_logs` records to ensure backward compatibility, and then drops the siloed `telegram_contacts` table.

### DDL and Migration Script: `024_omnichannel_contact_merging.sql`

```sql
-- +goose Up
-- +goose StatementBegin

-- 1. Create contacts table
CREATE TABLE contacts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    email VARCHAR(255),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_contacts_workspace ON contacts(workspace_id);

-- 2. Create contact_identities table
CREATE TABLE contact_identities (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    contact_id UUID NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    channel VARCHAR(50) NOT NULL,
    sender_identity VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(workspace_id, channel, sender_identity)
);

CREATE INDEX idx_contact_identities_lookup ON contact_identities(workspace_id, channel, sender_identity);
CREATE INDEX idx_contact_identities_contact ON contact_identities(contact_id);

-- 3. Migrate telegram_contacts data into contacts & contact_identities
DO $$
DECLARE
    r RECORD;
    new_contact_id UUID;
    contact_name TEXT;
BEGIN
    FOR r IN SELECT * FROM telegram_contacts LOOP
        new_contact_id := gen_random_uuid();
        contact_name := TRIM(COALESCE(NULLIF(r.first_name || ' ' || COALESCE(r.last_name, ''), ' '), r.username, r.chat_id));
        
        -- Create contact
        INSERT INTO contacts (id, workspace_id, name, created_at, updated_at)
        VALUES (new_contact_id, r.workspace_id, contact_name, r.updated_at, r.updated_at);
        
        -- Link primary numeric chat_id identity
        INSERT INTO contact_identities (contact_id, workspace_id, channel, sender_identity, created_at)
        VALUES (new_contact_id, r.workspace_id, 'telegram', r.chat_id, r.updated_at);
        
        -- Link username identity if present (stripped of leading '@' for lookup normalization)
        IF r.username IS NOT NULL AND r.username <> '' THEN
            INSERT INTO contact_identities (contact_id, workspace_id, channel, sender_identity, created_at)
            VALUES (new_contact_id, r.workspace_id, 'telegram_username', LOWER(TRIM(LEADING '@' FROM r.username)), r.updated_at)
            ON CONFLICT (workspace_id, channel, sender_identity) DO NOTHING;
        END IF;
        
        -- Link phone number identity if present (enables cross-channel mapping with WhatsApp)
        IF r.phone_number IS NOT NULL AND r.phone_number <> '' THEN
            INSERT INTO contact_identities (contact_id, workspace_id, channel, sender_identity, created_at)
            VALUES (new_contact_id, r.workspace_id, 'phone', r.phone_number, r.updated_at)
            ON CONFLICT (workspace_id, channel, sender_identity) DO NOTHING;
        END IF;
    END LOOP;
END $$;

-- 4. Backfill contacts & identities from historical audit_logs (WhatsApp / WhatsApp Cloud / unmapped Telegrams)
DO $$
DECLARE
    r RECORD;
    new_contact_id UUID;
BEGIN
    FOR r IN 
        SELECT DISTINCT workspace_id, payload->>'channel' as chan, payload->>'from' as ident
        FROM audit_logs
        WHERE event_type = 'inbound_message'
          AND payload->>'from' IS NOT NULL 
          AND payload->>'from' <> ''
          AND payload->>'channel' IS NOT NULL 
          AND payload->>'channel' <> ''
    LOOP
        -- Check if identity already exists
        IF NOT EXISTS (
            SELECT 1 FROM contact_identities 
            WHERE workspace_id = r.workspace_id AND channel = r.chan AND sender_identity = r.ident
        ) THEN
            new_contact_id := gen_random_uuid();
            
            INSERT INTO contacts (id, workspace_id, name)
            VALUES (new_contact_id, r.workspace_id, r.ident);
            
            INSERT INTO contact_identities (contact_id, workspace_id, channel, sender_identity)
            VALUES (new_contact_id, r.workspace_id, r.chan, r.ident);
        END IF;
    END LOOP;
END $$;

-- 5. Drop legacy telegram_contacts table
DROP TABLE IF EXISTS telegram_contacts;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- 1. Recreate telegram_contacts table
CREATE TABLE telegram_contacts (
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    chat_id TEXT NOT NULL,
    username TEXT,
    phone_number TEXT,
    first_name TEXT,
    last_name TEXT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, chat_id)
);

CREATE UNIQUE INDEX idx_telegram_contacts_username ON telegram_contacts(workspace_id, username) WHERE username IS NOT NULL;
CREATE UNIQUE INDEX idx_telegram_contacts_phone ON telegram_contacts(workspace_id, phone_number) WHERE phone_number IS NOT NULL;

-- 2. Migrate data back
INSERT INTO telegram_contacts (workspace_id, chat_id, username, phone_number, first_name, last_name, updated_at)
SELECT 
    c.workspace_id,
    ci.sender_identity,
    (SELECT sender_identity FROM contact_identities WHERE contact_id = c.id AND channel = 'telegram_username' LIMIT 1),
    (SELECT sender_identity FROM contact_identities WHERE contact_id = c.id AND channel = 'phone' LIMIT 1),
    c.name,
    '',
    c.updated_at
FROM contacts c
JOIN contact_identities ci ON ci.contact_id = c.id
WHERE ci.channel = 'telegram';

-- 3. Drop new tables
DROP TABLE IF EXISTS contact_identities CASCADE;
DROP TABLE IF EXISTS contacts CASCADE;

-- +goose StatementEnd
```

---

## 2. Go Domain Models & Repository

### Domain Structs: `internal/domain/contact.go`

```go
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
```

### Contact Repository Interface & Implementation: `internal/repository/contact.go`

The repository handles contact resolution, merging, searching, and identity-to-contact routing mappings. It uses transaction isolation levels and parameterization.

```go
package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

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
	err := r.pool.QueryRow(ctx, `
		SELECT id, workspace_id, name, email, created_at, updated_at
		FROM contacts WHERE workspace_id = $1 AND id = $2
	`, workspaceID, contactID).Scan(&c.ID, &c.WorkspaceID, &c.Name, &c.Email, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrContactNotFound
		}
		return nil, err
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

	// 3. Delete secondary contact profile
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
		SELECT DISTINCT c.id, c.name, c.email, c.created_at, c.updated_at
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
		if err := rows.Scan(&c.ID, &c.Name, &c.Email, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
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
```

---

## 3. Message Resolution Hooks

Every inbound event or operator outbound action must map the sender/receiver identifier to a contact profile.

### Inbound Resolution: `internal/inbound/processor.go`

We add the `contactRepo` dependency to `InboundProcessor`. Inside `Process(ctx, ev)`, we call `ResolveContact` before any other pipeline step:

```go
// Add ContactRepository to InboundProcessor struct
type InboundProcessor struct {
	// ... existing repos
	contactRepo          *repository.ContactRepository
}

func (p *InboundProcessor) Process(ctx context.Context, ev *InboundEvent) error {
	if ev.WorkspaceID == uuid.Nil {
		return fmt.Errorf("inbound: workspace ID is required")
	}

	// 1. Resolve or Create Contact and identity mapping
	var contactID uuid.UUID
	if p.contactRepo != nil {
		var username, phone string
		// Extract metadata if telegram or other channels provide it
		if ev.Channel == "telegram" {
			username = ev.Metadata["username"]
			phone = ev.Metadata["phone_number"]
		}
		contact, err := p.contactRepo.ResolveContact(ctx, ev.WorkspaceID, ev.Channel, ev.From, ev.SenderName, username, phone)
		if err != nil {
			slog.Error("inbound: failed to resolve contact profile", "error", err, "from", ev.From)
		} else {
			contactID = contact.ID
		}
	}
	_ = contactID

	// ... continue with session tracking, deduplication, S3 upload, and NATS publishing ...
}
```

### Outbound Resolution

1. **Public API Ingestion (`internal/outbound/processor.go`)**:
   Add a contact resolution step inside `Ingest(ctx, workspaceID, traceID, req)` to automatically create a contact profile when sending an outbound message to a target number/username:
   ```go
   if p.contactRepo != nil {
       _, _ = p.contactRepo.ResolveContact(ctx, workspaceID, req.Channel, req.To, req.To, "", "")
   }
   ```

2. **Dispatcher Worker Resolution (`internal/platform/queue/orchestrator.go`)**:
   Substitute `TelegramContactRepository` with `ContactRepository`:
   ```go
   to := qMsg.To
   if channelName == "telegram" && o.contactRepo != nil {
       if resolvedChatID, err := o.contactRepo.ResolveTelegramChatID(ctx, qMsg.WorkspaceID, qMsg.To); err == nil && resolvedChatID != "" {
           to = resolvedChatID
           qMsg.To = resolvedChatID
       }
   }
   ```

---

## 4. Consolidated Chat Queries & Router Integration

Currently, conversations are fetched and grouped by `(from, channel, to)`. In Phase 18, we restructure this to group by the underlying `contact_id`.

### REST / Repository Thread Updates: `internal/repository/audit.go`

```go
// ConversationSummary represents a contact-level conversation card.
type ConversationSummary struct {
	ContactID         uuid.UUID `json:"contact_id"`
	ContactName       string    `json:"contact_name"`
	LastMessageBody   string    `json:"last_message_body"`
	LastMessageTime   time.Time `json:"last_message_time"`
	TotalMessageCount int64     `json:"total_message_count"`
	Channel           string    `json:"channel"`            // channel of last message
	RecipientIdentity string    `json:"recipient_identity"` // recipient identity of last message
}

// ListConversations lists unified conversations grouped by contact_id.
func (r *AuditRepository) ListConversations(ctx context.Context, workspaceID uuid.UUID, channelFilter string) ([]ConversationSummary, error) {
	query := `
		WITH MsgWithContact AS (
			SELECT 
				al.id,
				al.created_at,
				al.payload->>'body' AS body,
				al.payload->>'channel' AS channel,
				al.payload->>'to' AS recipient_identity,
				ci.contact_id,
				c.name AS contact_name
			FROM audit_logs al
			LEFT JOIN contact_identities ci ON ci.workspace_id = al.workspace_id 
				AND ci.channel = al.payload->>'channel' 
				AND ci.sender_identity = al.payload->>'from'
			LEFT JOIN contacts c ON c.id = ci.contact_id
			WHERE al.workspace_id = $1 
			  AND al.event_type = 'inbound_message'
		),
		RankedConversations AS (
			SELECT 
				contact_id,
				contact_name,
				channel,
				recipient_identity,
				body,
				created_at,
				ROW_NUMBER() OVER(PARTITION BY contact_id ORDER BY created_at DESC) as rn,
				COUNT(*) OVER(PARTITION BY contact_id) as total_count
			FROM MsgWithContact
		)
		SELECT 
			COALESCE(contact_id, '00000000-0000-0000-0000-000000000000'::uuid), 
			COALESCE(contact_name, ''), 
			channel, 
			COALESCE(recipient_identity, ''), 
			COALESCE(body, ''), 
			created_at, 
			total_count
		FROM RankedConversations
		WHERE rn = 1
		  AND ($2 = '' OR channel = $2)
		ORDER BY created_at DESC
	`

	rows, err := r.pool.Query(ctx, query, workspaceID, channelFilter)
	if err != nil {
		return nil, fmt.Errorf("query conversations list: %w", err)
	}
	defer rows.Close()

	var summaries []ConversationSummary
	for rows.Next() {
		var s ConversationSummary
		if err := rows.Scan(&s.ContactID, &s.ContactName, &s.Channel, &s.RecipientIdentity, &s.LastMessageBody, &s.LastMessageTime, &s.TotalMessageCount); err != nil {
			return nil, fmt.Errorf("scan conversation summary: %w", err)
		}
		summaries = append(summaries, s)
	}
	return summaries, nil
}

// ListThread performs a UNION between inbound and outbound messages matching ANY identity owned by the Contact.
func (r *AuditRepository) ListThreadByContact(ctx context.Context, workspaceID uuid.UUID, contactID uuid.UUID, afterID *uuid.UUID) ([]ThreadMessage, error) {
	query := `
		SELECT al.id, al.trace_id, 'inbound' AS direction, COALESCE(al.payload->>'body', '') AS body, al.created_at
		FROM audit_logs al
		JOIN contact_identities ci ON ci.workspace_id = al.workspace_id 
			AND ci.channel = al.payload->>'channel' 
			AND ci.sender_identity = al.payload->>'from'
		WHERE al.workspace_id = $1
		  AND ci.contact_id = $2
		  AND al.event_type = 'inbound_message'
		  AND ($3::uuid IS NULL OR al.id > $3::uuid)

		UNION ALL

		SELECT al.id, al.trace_id, 'outbound' AS direction, COALESCE(al.payload->'request'->>'body', '') AS body, al.created_at
		FROM audit_logs al
		JOIN contact_identities ci ON ci.workspace_id = al.workspace_id 
			AND ci.channel = al.payload->'request'->>'channel' 
			AND ci.sender_identity = al.payload->'request'->>'to'
		WHERE al.workspace_id = $1
		  AND ci.contact_id = $2
		  AND al.event_type = 'outbound_message'
		  AND ($3::uuid IS NULL OR al.id > $3::uuid)

		ORDER BY created_at ASC
	`

	rows, err := r.pool.Query(ctx, query, workspaceID, contactID, afterID)
	if err != nil {
		return nil, fmt.Errorf("query thread messages: %w", err)
	}
	defer rows.Close()

	var messages []ThreadMessage
	for rows.Next() {
		var m ThreadMessage
		if err := rows.Scan(&m.ID, &m.TraceID, &m.Direction, &m.Body, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan thread message: %w", err)
		}
		messages = append(messages, m)
	}
	return messages, nil
}
```

---

## 5. Dashboard Merging UI Design

The merging action is integrated into the Inbox split-pane view. 

### UI Details

1. **Merge Trigger Button**: Added to the Chat Panel header next to the Contact's name.
2. **HTMX Search Input Overlay**: When clicked, reveals a search panel requesting results dynamically.
3. **Target Selection**: Search targets hit `/admin/contacts/search` with keyup delay (`hx-trigger="keyup changed delay:300ms"`).
4. **Merge Request**: Selecting a contact fires a `POST /admin/contacts/merge` request with parameters `primary_id` and `secondary_id`, prompting standard browser confirmation beforehand.

```html
<!-- Merging UI Dropdown inside Chat Panel Header -->
<div class="relative inline-block text-left" id="merge-dropdown-container">
    <button 
        type="button" 
        class="inline-flex items-center gap-1.5 px-2.5 py-1.5 border border-zinc-200 text-xs font-semibold rounded-md text-zinc-700 bg-white hover:bg-zinc-50"
        onclick="document.getElementById('merge-menu').classList.toggle('hidden')"
    >
        Mesclar Contato
    </button>

    <div 
        id="merge-menu" 
        class="hidden origin-top-right absolute right-0 mt-2 w-72 rounded-md shadow-lg bg-white ring-1 ring-black ring-opacity-5 focus:outline-none z-50 p-3"
    >
        <p class="text-xs font-semibold text-zinc-500 mb-2">Mesclar com outro contato:</p>
        <input 
            type="text" 
            name="q" 
            placeholder="Buscar por nome ou telefone..." 
            class="w-full px-2 py-1 text-xs border border-zinc-300 rounded-md focus:outline-none focus:ring-1 focus:ring-blue-500"
            hx-get={ fmt.Sprintf("/admin/contacts/search?exclude_id=%s", contact.ID) }
            hx-trigger="keyup changed delay:300ms"
            hx-target="#merge-results"
        />
        <div id="merge-results" class="mt-2 max-h-48 overflow-y-auto flex flex-col gap-1">
            <!-- Results populated dynamically by HTMX -->
        </div>
    </div>
</div>
```

### Search Result Component Template (HTML Fragment)

```html
<!-- Search results returned from GET /admin/contacts/search -->
for _, c := range searchResults {
    <div 
        class="p-2 hover:bg-zinc-50 rounded cursor-pointer text-xs flex justify-between items-center"
        hx-post={ fmt.Sprintf("/admin/contacts/merge?primary_id=%s&secondary_id=%s", primaryID, c.ID) }
        hx-confirm={ fmt.Sprintf("Tem certeza que deseja mesclar %s com este contato? Todo o histórico de conversas será unificado.", c.Name) }
        hx-target="body"
    >
        <div>
            <p class="font-medium text-zinc-950">{ c.Name }</p>
            <p class="text-zinc-400 text-[10px]">{ c.Email }</p>
        </div>
        <svg class="h-3 w-3 text-zinc-400" ...arrow icon... />
    </div>
}
```

---

## 6. Modified/Created Files Checklist

Below is the list of files to be created or modified in Phase 18:

| File Action | Path | Purpose |
|-------------|------|---------|
| **Create** | `internal/platform/postgres/migrations/024_omnichannel_contact_merging.sql` | Migration creating `contacts` and `contact_identities`, migrating old data, dropping `telegram_contacts` |
| **Create** | `internal/domain/contact.go` | Domain structures representing `Contact` and `ContactIdentity` |
| **Create** | `internal/repository/contact.go` | Contact database access layer including resolver, search, and merge logic |
| **Modify** | `internal/inbound/processor.go` | Inject `ContactRepository` and run `ResolveContact` on inbound message processing |
| **Modify** | `internal/platform/queue/orchestrator.go` | Substitute `TelegramContactRepository` with `ContactRepository` to translate Telegram identifiers |
| **Modify** | `internal/repository/audit.go` | Update `ListConversations` and `ListThread` to query by unified contact mappings |
| **Modify** | `internal/api/handler/admin/inbox.go` | Wire merging/search actions, update handlers to use Contact entities |
| **Modify** | `cmd/pergo/main.go` | Wire up the new `ContactRepository` and register merge/search endpoints |
| **Modify** | `templates/pages/inbox.templ` | Render updated layout supporting contacts |
| **Modify** | `templates/components/chat_panel.templ` | Add merge button, channel picker dropdown, and unify bubble render targets |
| **Modify** | `templates/components/conv_item.templ` | Update click routes to pass contact IDs instead of raw numbers |
