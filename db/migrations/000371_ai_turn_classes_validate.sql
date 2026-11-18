-- Validate 000370's check on the turns' classes on its own, so adding it never scanned the table under its lock.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.ai_turn VALIDATE CONSTRAINT ai_turn_classes;
