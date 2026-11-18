-- Validate 000379's check on the selections on its own, so adding it never scanned the table under its lock.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.approval_signature VALIDATE CONSTRAINT approval_signature_selection;
