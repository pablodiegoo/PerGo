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
    -- Check if table exists before migrating to avoid errors if run in clean db environments
    IF EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'telegram_contacts') THEN
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
    END IF;
END $$;

-- 4. Backfill contacts & identities from historical audit_logs (WhatsApp / WhatsApp Cloud / unmapped Telegrams)
DO $$
DECLARE
    r RECORD;
    new_contact_id UUID;
BEGIN
    IF EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'audit_logs') THEN
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
    END IF;
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
