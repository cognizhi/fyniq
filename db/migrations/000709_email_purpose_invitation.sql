-- An invitation's email (PRD-001 FR-ADM-01, p1-users-roles task 2.1): one more purpose of a message in the email outbox. It
-- names the organisation and says to ask the inviter for the one-time code; the code is never in it. The check is validated by 000710.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.email_message DROP CONSTRAINT email_message_purpose_known;
ALTER TABLE fyniq.email_message ADD CONSTRAINT email_message_purpose_known
  CHECK (purpose IN ('test', 'sign_in_alert', 'notification', 'digest', 'invoice', 'quote', 'reminder', 'statement', 'remittance',
    'secretary_reminder', 'invitation')) NOT VALID;
