-- Validates the check 000709 added NOT VALID, in a transaction of its own.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.email_message VALIDATE CONSTRAINT email_message_purpose_known;
