-- +goose Up
-- +goose StatementBegin
CREATE EXTENSION IF NOT EXISTS pgcrypto;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'whatsmeow_device_raw') THEN
        IF EXISTS (SELECT 1 FROM pg_tables WHERE tablename = 'whatsmeow_device' AND schemaname = 'public') THEN
            DROP TABLE public.whatsmeow_device CASCADE;
        ELSIF EXISTS (SELECT 1 FROM pg_views WHERE viewname = 'whatsmeow_device' AND schemaname = 'public') THEN
            DROP VIEW public.whatsmeow_device CASCADE;
        END IF;

        EXECUTE 'CREATE VIEW whatsmeow_device AS
        SELECT 
            jid,
            lid,
            facebook_uuid,
            registration_id,
            CASE WHEN noise_key IS NOT NULL THEN pgp_sym_decrypt_bytea(noise_key, ''pergo_whatsmeow_secret'') ELSE NULL END AS noise_key,
            CASE WHEN identity_key IS NOT NULL THEN pgp_sym_decrypt_bytea(identity_key, ''pergo_whatsmeow_secret'') ELSE NULL END AS identity_key,
            CASE WHEN signed_pre_key IS NOT NULL THEN pgp_sym_decrypt_bytea(signed_pre_key, ''pergo_whatsmeow_secret'') ELSE NULL END AS signed_pre_key,
            signed_pre_key_id,
            CASE WHEN signed_pre_key_sig IS NOT NULL THEN pgp_sym_decrypt_bytea(signed_pre_key_sig, ''pergo_whatsmeow_secret'') ELSE NULL END AS signed_pre_key_sig,
            CASE WHEN adv_key IS NOT NULL THEN pgp_sym_decrypt_bytea(adv_key, ''pergo_whatsmeow_secret'') ELSE NULL END AS adv_key,
            CASE WHEN adv_details IS NOT NULL THEN pgp_sym_decrypt_bytea(adv_details, ''pergo_whatsmeow_secret'') ELSE NULL END AS adv_details,
            CASE WHEN adv_account_sig IS NOT NULL THEN pgp_sym_decrypt_bytea(adv_account_sig, ''pergo_whatsmeow_secret'') ELSE NULL END AS adv_account_sig,
            CASE WHEN adv_account_sig_key IS NOT NULL THEN pgp_sym_decrypt_bytea(adv_account_sig_key, ''pergo_whatsmeow_secret'') ELSE NULL END AS adv_account_sig_key,
            CASE WHEN adv_device_sig IS NOT NULL THEN pgp_sym_decrypt_bytea(adv_device_sig, ''pergo_whatsmeow_secret'') ELSE NULL END AS adv_device_sig,
            platform,
            business_name,
            push_name,
            lid_migration_ts
        FROM whatsmeow_device_raw';

        EXECUTE 'CREATE OR REPLACE FUNCTION encrypt_whatsmeow_device()
        RETURNS TRIGGER AS $func$
        BEGIN
            INSERT INTO whatsmeow_device_raw (
                jid, lid, facebook_uuid, registration_id,
                noise_key, identity_key, signed_pre_key, signed_pre_key_id, signed_pre_key_sig,
                adv_key, adv_details, adv_account_sig, adv_account_sig_key, adv_device_sig,
                platform, business_name, push_name, lid_migration_ts
            )
            VALUES (
                NEW.jid, NEW.lid, NEW.facebook_uuid, NEW.registration_id,
                CASE WHEN NEW.noise_key IS NOT NULL THEN pgp_sym_encrypt_bytea(NEW.noise_key, ''pergo_whatsmeow_secret'') ELSE NULL END,
                CASE WHEN NEW.identity_key IS NOT NULL THEN pgp_sym_encrypt_bytea(NEW.identity_key, ''pergo_whatsmeow_secret'') ELSE NULL END,
                CASE WHEN NEW.signed_pre_key IS NOT NULL THEN pgp_sym_encrypt_bytea(NEW.signed_pre_key, ''pergo_whatsmeow_secret'') ELSE NULL END,
                NEW.signed_pre_key_id,
                CASE WHEN NEW.signed_pre_key_sig IS NOT NULL THEN pgp_sym_encrypt_bytea(NEW.signed_pre_key_sig, ''pergo_whatsmeow_secret'') ELSE NULL END,
                CASE WHEN NEW.adv_key IS NOT NULL THEN pgp_sym_encrypt_bytea(NEW.adv_key, ''pergo_whatsmeow_secret'') ELSE NULL END,
                CASE WHEN NEW.adv_details IS NOT NULL THEN pgp_sym_encrypt_bytea(NEW.adv_details, ''pergo_whatsmeow_secret'') ELSE NULL END,
                CASE WHEN NEW.adv_account_sig IS NOT NULL THEN pgp_sym_encrypt_bytea(NEW.adv_account_sig, ''pergo_whatsmeow_secret'') ELSE NULL END,
                CASE WHEN NEW.adv_account_sig_key IS NOT NULL THEN pgp_sym_encrypt_bytea(NEW.adv_account_sig_key, ''pergo_whatsmeow_secret'') ELSE NULL END,
                CASE WHEN NEW.adv_device_sig IS NOT NULL THEN pgp_sym_encrypt_bytea(NEW.adv_device_sig, ''pergo_whatsmeow_secret'') ELSE NULL END,
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
        $func$ LANGUAGE plpgsql';

        EXECUTE 'CREATE OR REPLACE FUNCTION delete_whatsmeow_device()
        RETURNS TRIGGER AS $func$
        BEGIN
            DELETE FROM whatsmeow_device_raw WHERE jid = OLD.jid;
            RETURN OLD;
        END;
        $func$ LANGUAGE plpgsql';

        EXECUTE 'CREATE TRIGGER whatsmeow_device_insert_update_trigger
        INSTEAD OF INSERT OR UPDATE ON whatsmeow_device
        FOR EACH ROW EXECUTE FUNCTION encrypt_whatsmeow_device()';

        EXECUTE 'CREATE TRIGGER whatsmeow_device_delete_trigger
        INSTEAD OF DELETE ON whatsmeow_device
        FOR EACH ROW EXECUTE FUNCTION delete_whatsmeow_device()';
    END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- +goose StatementEnd

