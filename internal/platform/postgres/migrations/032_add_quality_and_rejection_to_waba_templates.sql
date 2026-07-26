-- +goose Up
ALTER TABLE waba_templates
    ADD COLUMN rejection_reason TEXT,
    ADD COLUMN quality_score VARCHAR(50);

-- +goose Down
ALTER TABLE waba_templates
    DROP COLUMN IF EXISTS rejection_reason,
    DROP COLUMN IF EXISTS quality_score;
