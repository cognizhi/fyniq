-- Validates the constraints 000338 widened NOT VALID: a migration file's kinds and media types, now with the PDFs of
-- the workbook's reports (p1-workbook-migration task 6.1).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.migration_file VALIDATE CONSTRAINT migration_file_kind_known;
ALTER TABLE fyniq.migration_file VALIDATE CONSTRAINT migration_file_media_type;
