-- Validates the constraints that 000047 added NOT VALID, in a transaction of its own (squawk:
-- constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.webauthn_challenge VALIDATE CONSTRAINT webauthn_challenge_purpose_known;
ALTER TABLE fyniq.webauthn_challenge VALIDATE CONSTRAINT webauthn_challenge_shape;
ALTER TABLE fyniq.approval_signature VALIDATE CONSTRAINT approval_signature_passkey_shape;
