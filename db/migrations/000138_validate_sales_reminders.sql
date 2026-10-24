-- Validates the key and the check 000137 added for reminders to customers (PRD-001 FR-AR-08).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.contact VALIDATE CONSTRAINT contact_customer_group_fkey;
ALTER TABLE fyniq.email_message VALIDATE CONSTRAINT email_message_purpose_known;
