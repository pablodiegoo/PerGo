-- +goose Up
-- +goose StatementBegin
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- If whatsmeow_device already exists (e.g. created by whatsmeow directly), rename it
DO $$
BEGIN
    IF EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'whatsmeow_device') THEN
        ALTER TABLE whatsmeow_device RENAME TO whatsmeow_device_old;
    END IF;
END;
$$;

-- Create the raw table with NO CHECK constraints on key sizes
CREATE TABLE whatsmeow_device_raw (
	jid TEXT PRIMARY KEY,
	lid TEXT,
	facebook_uuid uuid,
	registration_id BIGINT,
	noise_key    bytea,
	identity_key bytea,
	signed_pre_key     bytea,
	signed_pre_key_id  INTEGER,
	signed_pre_key_sig bytea,
	adv_key             bytea,
	adv_details         bytea,
	adv_account_sig     bytea,
	adv_account_sig_key bytea,
	adv_device_sig      bytea,
	platform      TEXT DEFAULT '',
	business_name TEXT DEFAULT '',
	push_name     TEXT DEFAULT '',
	lid_migration_ts BIGINT DEFAULT 0
);

-- If whatsmeow_device_old exists, encrypt and copy data
DO $$
BEGIN
    IF EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'whatsmeow_device_old') THEN
        INSERT INTO whatsmeow_device_raw (
            jid, lid, facebook_uuid, registration_id,
            noise_key, identity_key, signed_pre_key, signed_pre_key_id, signed_pre_key_sig,
            adv_key, adv_details, adv_account_sig, adv_account_sig_key, adv_device_sig,
            platform, business_name, push_name, lid_migration_ts
        )
        SELECT
            jid, lid, facebook_uuid, registration_id,
            pgp_sym_encrypt(noise_key, 'omnigo_whatsmeow_secret'),
            pgp_sym_encrypt(identity_key, 'omnigo_whatsmeow_secret'),
            pgp_sym_encrypt(signed_pre_key, 'omnigo_whatsmeow_secret'),
            signed_pre_key_id,
            pgp_sym_encrypt(signed_pre_key_sig, 'omnigo_whatsmeow_secret'),
            pgp_sym_encrypt(adv_key, 'omnigo_whatsmeow_secret'),
            pgp_sym_encrypt(adv_details, 'omnigo_whatsmeow_secret'),
            pgp_sym_encrypt(adv_account_sig, 'omnigo_whatsmeow_secret'),
            pgp_sym_encrypt(adv_account_sig_key, 'omnigo_whatsmeow_secret'),
            pgp_sym_encrypt(adv_device_sig, 'omnigo_whatsmeow_secret'),
            platform, business_name, push_name, lid_migration_ts
        FROM whatsmeow_device_old;

        DROP TABLE whatsmeow_device_old CASCADE;
    END IF;
END;
$$;

-- Create the view exposing decrypted keys
CREATE OR REPLACE VIEW whatsmeow_device AS
SELECT 
    jid,
    lid,
    facebook_uuid,
    registration_id,
    pgp_sym_decrypt(noise_key, 'omnigo_whatsmeow_secret') AS noise_key,
    pgp_sym_decrypt(identity_key, 'omnigo_whatsmeow_secret') AS identity_key,
    pgp_sym_decrypt(signed_pre_key, 'omnigo_whatsmeow_secret') AS signed_pre_key,
    signed_pre_key_id,
    pgp_sym_decrypt(signed_pre_key_sig, 'omnigo_whatsmeow_secret') AS signed_pre_key_sig,
    pgp_sym_decrypt(adv_key, 'omnigo_whatsmeow_secret') AS adv_key,
    pgp_sym_decrypt(adv_details, 'omnigo_whatsmeow_secret') AS adv_details,
    pgp_sym_decrypt(adv_account_sig, 'omnigo_whatsmeow_secret') AS adv_account_sig,
    pgp_sym_decrypt(adv_account_sig_key, 'omnigo_whatsmeow_secret') AS adv_account_sig_key,
    pgp_sym_decrypt(adv_device_sig, 'omnigo_whatsmeow_secret') AS adv_device_sig,
    platform,
    business_name,
    push_name,
    lid_migration_ts
FROM whatsmeow_device_raw;

-- Create the INSTEAD OF trigger functions
CREATE OR REPLACE FUNCTION encrypt_whatsmeow_device()
RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO whatsmeow_device_raw (
        jid, lid, facebook_uuid, registration_id,
        noise_key, identity_key, signed_pre_key, signed_pre_key_id, signed_pre_key_sig,
        adv_key, adv_details, adv_account_sig, adv_account_sig_key, adv_device_sig,
        platform, business_name, push_name, lid_migration_ts
    )
    VALUES (
        NEW.jid, NEW.lid, NEW.facebook_uuid, NEW.registration_id,
        pgp_sym_encrypt(NEW.noise_key, 'omnigo_whatsmeow_secret'),
        pgp_sym_encrypt(NEW.identity_key, 'omnigo_whatsmeow_secret'),
        pgp_sym_encrypt(NEW.signed_pre_key, 'omnigo_whatsmeow_secret'),
        NEW.signed_pre_key_id,
        pgp_sym_encrypt(NEW.signed_pre_key_sig, 'omnigo_whatsmeow_secret'),
        pgp_sym_encrypt(NEW.adv_key, 'omnigo_whatsmeow_secret'),
        pgp_sym_encrypt(NEW.adv_details, 'omnigo_whatsmeow_secret'),
        pgp_sym_encrypt(NEW.adv_account_sig, 'omnigo_whatsmeow_secret'),
        pgp_sym_encrypt(NEW.adv_account_sig_key, 'omnigo_whatsmeow_secret'),
        pgp_sym_encrypt(NEW.adv_device_sig, 'omnigo_whatsmeow_secret'),
        NEW.platform, NEW.business_name, NEW.push_name, NEW.lid_migration_ts
    )
    ON CONFLICT (jid) DO UPDATE SET
        lid = EXCLUDED.lid,
        facebook_uuid = EXCLUDED.facebook_uuid,
        registration_id = EXCLUDED.registration_id,
        noise_key = EXCLUDED.noise_key,
        identity_key = EXCLUDED.identity_key,
        signed_pre_key = EXCLUDED.signed_pre_key,
        signed_pre_key_id = EXCLUDED.signed_pre_key_id,
        signed_pre_key_sig = EXCLUDED.signed_pre_key_sig,
        adv_key = EXCLUDED.adv_key,
        adv_details = EXCLUDED.adv_details,
        adv_account_sig = EXCLUDED.adv_account_sig,
        adv_account_sig_key = EXCLUDED.adv_account_sig_key,
        adv_device_sig = EXCLUDED.adv_device_sig,
        platform = EXCLUDED.platform,
        business_name = EXCLUDED.business_name,
        push_name = EXCLUDED.push_name,
        lid_migration_ts = EXCLUDED.lid_migration_ts;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION delete_whatsmeow_device()
RETURNS TRIGGER AS $$
BEGIN
    DELETE FROM whatsmeow_device_raw WHERE jid = OLD.jid;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

-- Bind triggers to view
CREATE TRIGGER whatsmeow_device_insert_update_trigger
INSTEAD OF INSERT OR UPDATE ON whatsmeow_device
FOR EACH ROW EXECUTE FUNCTION encrypt_whatsmeow_device();

CREATE TRIGGER whatsmeow_device_delete_trigger
INSTEAD OF DELETE ON whatsmeow_device
FOR EACH ROW EXECUTE FUNCTION delete_whatsmeow_device();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP VIEW IF EXISTS whatsmeow_device CASCADE;
DROP FUNCTION IF EXISTS encrypt_whatsmeow_device();
DROP FUNCTION IF EXISTS delete_whatsmeow_device();

-- Recreate standard table if we roll back
CREATE TABLE whatsmeow_device (
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

-- Copy data back decrypted
INSERT INTO whatsmeow_device (
    jid, lid, facebook_uuid, registration_id,
    noise_key, identity_key, signed_pre_key, signed_pre_key_id, signed_pre_key_sig,
    adv_key, adv_details, adv_account_sig, adv_account_sig_key, adv_device_sig,
    platform, business_name, push_name, lid_migration_ts
)
SELECT
    jid, lid, facebook_uuid, registration_id,
    pgp_sym_decrypt(noise_key, 'omnigo_whatsmeow_secret'),
    pgp_sym_decrypt(identity_key, 'omnigo_whatsmeow_secret'),
    pgp_sym_decrypt(signed_pre_key, 'omnigo_whatsmeow_secret'),
    signed_pre_key_id,
    pgp_sym_decrypt(signed_pre_key_sig, 'omnigo_whatsmeow_secret'),
    pgp_sym_decrypt(adv_key, 'omnigo_whatsmeow_secret'),
    pgp_sym_decrypt(adv_details, 'omnigo_whatsmeow_secret'),
    pgp_sym_decrypt(adv_account_sig, 'omnigo_whatsmeow_secret'),
    pgp_sym_decrypt(adv_account_sig_key, 'omnigo_whatsmeow_secret'),
    pgp_sym_decrypt(adv_device_sig, 'omnigo_whatsmeow_secret'),
    platform, business_name, push_name, lid_migration_ts
FROM whatsmeow_device_raw;

DROP TABLE whatsmeow_device_raw CASCADE;
-- +goose StatementEnd
