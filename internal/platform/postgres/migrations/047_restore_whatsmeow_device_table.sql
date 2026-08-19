-- +goose Up
-- +goose StatementBegin
-- whatsmeow requires whatsmeow_device to be a real table because its internal store
-- executes `INSERT INTO whatsmeow_device (...) VALUES (...) ON CONFLICT (jid) DO UPDATE ...`.
-- In PostgreSQL, `ON CONFLICT (col)` cannot target a VIEW, causing SQLSTATE 42P10.
-- We restore whatsmeow_device as a real table and create all required whatsmeow tables.

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.views WHERE table_name = 'whatsmeow_device') THEN
        EXECUTE 'DROP VIEW whatsmeow_device CASCADE';
    END IF;
END;
$$;

DROP FUNCTION IF EXISTS encrypt_whatsmeow_device() CASCADE;
DROP FUNCTION IF EXISTS delete_whatsmeow_device() CASCADE;

CREATE TABLE IF NOT EXISTS whatsmeow_device (
	jid TEXT PRIMARY KEY,
	lid TEXT,
	facebook_uuid uuid,
	registration_id BIGINT NOT NULL CHECK ( registration_id >= 0 AND registration_id < 4294967296 ),
	noise_key    bytea NOT NULL CHECK ( length(noise_key) = 32 ),
	identity_key bytea NOT NULL CHECK ( length(identity_key) = 32 ),
	signed_pre_key     bytea   NOT NULL CHECK ( length(signed_pre_key) = 32 ),
	signed_pre_key_id  INTEGER NOT NULL CHECK ( signed_pre_key_id >= 0 AND signed_pre_key_id < 16777216 ),
	signed_pre_key_sig bytea   NOT NULL CHECK ( length(signed_pre_key_sig) = 64 ),
	adv_key             bytea NOT NULL,
	adv_details         bytea NOT NULL,
	adv_account_sig     bytea NOT NULL CHECK ( length(adv_account_sig) = 64 ),
	adv_account_sig_key bytea NOT NULL CHECK ( length(adv_account_sig_key) = 32 ),
	adv_device_sig      bytea NOT NULL CHECK ( length(adv_device_sig) = 64 ),
	platform      TEXT NOT NULL DEFAULT '',
	business_name TEXT NOT NULL DEFAULT '',
	push_name     TEXT NOT NULL DEFAULT '',
	lid_migration_ts BIGINT NOT NULL DEFAULT 0
);

DO $$
BEGIN
    IF EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'whatsmeow_device_raw') THEN
        INSERT INTO whatsmeow_device (
            jid, lid, facebook_uuid, registration_id,
            noise_key, identity_key, signed_pre_key, signed_pre_key_id, signed_pre_key_sig,
            adv_key, adv_details, adv_account_sig, adv_account_sig_key, adv_device_sig,
            platform, business_name, push_name, lid_migration_ts
        )
        SELECT
            jid, lid, facebook_uuid, registration_id,
            CASE WHEN noise_key IS NOT NULL THEN pgp_sym_decrypt_bytea(noise_key, 'pergo_whatsmeow_secret') ELSE NULL END,
            CASE WHEN identity_key IS NOT NULL THEN pgp_sym_decrypt_bytea(identity_key, 'pergo_whatsmeow_secret') ELSE NULL END,
            CASE WHEN signed_pre_key IS NOT NULL THEN pgp_sym_decrypt_bytea(signed_pre_key, 'pergo_whatsmeow_secret') ELSE NULL END,
            signed_pre_key_id,
            CASE WHEN signed_pre_key_sig IS NOT NULL THEN pgp_sym_decrypt_bytea(signed_pre_key_sig, 'pergo_whatsmeow_secret') ELSE NULL END,
            CASE WHEN adv_key IS NOT NULL THEN pgp_sym_decrypt_bytea(adv_key, 'pergo_whatsmeow_secret') ELSE NULL END,
            CASE WHEN adv_details IS NOT NULL THEN pgp_sym_decrypt_bytea(adv_details, 'pergo_whatsmeow_secret') ELSE NULL END,
            CASE WHEN adv_account_sig IS NOT NULL THEN pgp_sym_decrypt_bytea(adv_account_sig, 'pergo_whatsmeow_secret') ELSE NULL END,
            CASE WHEN adv_account_sig_key IS NOT NULL THEN pgp_sym_decrypt_bytea(adv_account_sig_key, 'pergo_whatsmeow_secret') ELSE NULL END,
            CASE WHEN adv_device_sig IS NOT NULL THEN pgp_sym_decrypt_bytea(adv_device_sig, 'pergo_whatsmeow_secret') ELSE NULL END,
            COALESCE(platform, ''), COALESCE(business_name, ''), COALESCE(push_name, ''), COALESCE(lid_migration_ts, 0)
        FROM whatsmeow_device_raw
        ON CONFLICT (jid) DO NOTHING;

        DROP TABLE whatsmeow_device_raw CASCADE;
    END IF;
END;
$$;

CREATE TABLE IF NOT EXISTS whatsmeow_identity_keys (
	our_jid  TEXT,
	their_id TEXT,
	identity bytea NOT NULL CHECK ( length(identity) = 32 ),
	PRIMARY KEY (our_jid, their_id),
	FOREIGN KEY (our_jid) REFERENCES whatsmeow_device(jid) ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE TABLE IF NOT EXISTS whatsmeow_pre_keys (
	jid      TEXT,
	key_id   INTEGER          CHECK ( key_id >= 0 AND key_id < 16777216 ),
	key      bytea   NOT NULL CHECK ( length(key) = 32 ),
	uploaded BOOLEAN NOT NULL,
	PRIMARY KEY (jid, key_id),
	FOREIGN KEY (jid) REFERENCES whatsmeow_device(jid) ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE TABLE IF NOT EXISTS whatsmeow_sessions (
	our_jid  TEXT,
	their_id TEXT,
	session  bytea,
	PRIMARY KEY (our_jid, their_id),
	FOREIGN KEY (our_jid) REFERENCES whatsmeow_device(jid) ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE TABLE IF NOT EXISTS whatsmeow_sender_keys (
	our_jid    TEXT,
	chat_id    TEXT,
	sender_id  TEXT,
	sender_key bytea NOT NULL,
	PRIMARY KEY (our_jid, chat_id, sender_id),
	FOREIGN KEY (our_jid) REFERENCES whatsmeow_device(jid) ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE TABLE IF NOT EXISTS whatsmeow_app_state_sync_keys (
	jid         TEXT,
	key_id      bytea,
	key_data    bytea  NOT NULL,
	timestamp   BIGINT NOT NULL,
	fingerprint bytea  NOT NULL,
	PRIMARY KEY (jid, key_id),
	FOREIGN KEY (jid) REFERENCES whatsmeow_device(jid) ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE TABLE IF NOT EXISTS whatsmeow_app_state_version (
	jid     TEXT,
	name    TEXT,
	version BIGINT NOT NULL,
	hash    bytea  NOT NULL CHECK ( length(hash) = 128 ),
	PRIMARY KEY (jid, name),
	FOREIGN KEY (jid) REFERENCES whatsmeow_device(jid) ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE TABLE IF NOT EXISTS whatsmeow_app_state_mutation_macs (
	jid       TEXT,
	name      TEXT,
	version   BIGINT,
	index_mac bytea          CHECK ( length(index_mac) = 32 ),
	value_mac bytea NOT NULL CHECK ( length(value_mac) = 32 ),
	PRIMARY KEY (jid, name, version, index_mac),
	FOREIGN KEY (jid, name) REFERENCES whatsmeow_app_state_version(jid, name) ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE TABLE IF NOT EXISTS whatsmeow_contacts (
	our_jid        TEXT,
	their_jid      TEXT,
	first_name     TEXT,
	full_name      TEXT,
	push_name      TEXT,
	business_name  TEXT,
	redacted_phone TEXT,
	PRIMARY KEY (our_jid, their_jid),
	FOREIGN KEY (our_jid) REFERENCES whatsmeow_device(jid) ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE TABLE IF NOT EXISTS whatsmeow_chat_settings (
	our_jid       TEXT,
	chat_jid      TEXT,
	muted_until   BIGINT  NOT NULL DEFAULT 0,
	pinned        BOOLEAN NOT NULL DEFAULT false,
	archived      BOOLEAN NOT NULL DEFAULT false,
	PRIMARY KEY (our_jid, chat_jid),
	FOREIGN KEY (our_jid) REFERENCES whatsmeow_device(jid) ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE TABLE IF NOT EXISTS whatsmeow_message_secrets (
	our_jid    TEXT,
	chat_jid   TEXT,
	sender_jid TEXT,
	message_id TEXT,
	key        bytea NOT NULL,
	PRIMARY KEY (our_jid, chat_jid, sender_jid, message_id),
	FOREIGN KEY (our_jid) REFERENCES whatsmeow_device(jid) ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE TABLE IF NOT EXISTS whatsmeow_privacy_tokens (
	our_jid          TEXT,
	their_jid        TEXT,
	token            bytea  NOT NULL,
	timestamp        BIGINT NOT NULL,
	sender_timestamp BIGINT,
	PRIMARY KEY (our_jid, their_jid)
);

CREATE INDEX IF NOT EXISTS idx_whatsmeow_privacy_tokens_our_jid_timestamp ON whatsmeow_privacy_tokens (our_jid, timestamp);

CREATE TABLE IF NOT EXISTS whatsmeow_nct_salt (
	our_jid TEXT PRIMARY KEY,
	salt    bytea NOT NULL,
	FOREIGN KEY (our_jid) REFERENCES whatsmeow_device(jid) ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE TABLE IF NOT EXISTS whatsmeow_lid_map (
	lid TEXT PRIMARY KEY,
	pn  TEXT UNIQUE NOT NULL
);

CREATE TABLE IF NOT EXISTS whatsmeow_event_buffer (
	our_jid          TEXT   NOT NULL,
	ciphertext_hash  bytea  NOT NULL CHECK ( length(ciphertext_hash) = 32 ),
	plaintext        bytea,
	server_timestamp BIGINT NOT NULL,
	insert_timestamp BIGINT NOT NULL,
	PRIMARY KEY (our_jid, ciphertext_hash),
	FOREIGN KEY (our_jid) REFERENCES whatsmeow_device(jid) ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE TABLE IF NOT EXISTS whatsmeow_retry_buffer (
	our_jid    TEXT   NOT NULL,
	chat_jid   TEXT   NOT NULL,
	message_id TEXT   NOT NULL,
	format     TEXT   NOT NULL,
	plaintext  bytea  NOT NULL,
	timestamp  BIGINT NOT NULL,
	PRIMARY KEY (our_jid, chat_jid, message_id),
	FOREIGN KEY (our_jid) REFERENCES whatsmeow_device(jid) ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE INDEX IF NOT EXISTS whatsmeow_retry_buffer_timestamp_idx ON whatsmeow_retry_buffer (our_jid, timestamp);

CREATE TABLE IF NOT EXISTS whatsmeow_version (
	version INTEGER,
	compat  INTEGER
);

INSERT INTO whatsmeow_version (version, compat)
SELECT 14, 8
WHERE NOT EXISTS (SELECT 1 FROM whatsmeow_version);

UPDATE whatsmeow_version SET version = 14, compat = 8 WHERE version < 14;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- +goose StatementEnd
