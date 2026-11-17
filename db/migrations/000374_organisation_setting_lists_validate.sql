-- Validate 000373's check on the settings' values on its own, so adding it never scanned the table under its lock.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.organisation_setting VALIDATE CONSTRAINT organisation_setting_value_length;
