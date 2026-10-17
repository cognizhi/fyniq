-- An idempotency key's two times come from the same clock, the API's (FR-INT-08, DEV-05).
--
-- 000002 let the database stamp created_at with its own now() while the API set expires_at from its
-- clock, and idempotency_key_expiry compares the two. In local mode the API's clock can be a business
-- date far from the database's (DEV-05), and the API tests freeze it at 15 Oct 2026: from 09:00 UTC
-- on 16 Oct every request with an Idempotency-Key failed the check. The API now sets created_at with
-- expires_at, as it already does for user_session, so the check holds by construction.
--
-- The check is unchanged. fyniq_api gains INSERT on one column of its own response cache, which
-- carries no audit event (AUD-08); it already set expires_at, the time that decides anything. The
-- default goes, so an insert that forgets created_at fails loudly instead of mixing the clocks again.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.idempotency_key ALTER COLUMN created_at DROP DEFAULT;
GRANT INSERT (created_at) ON fyniq.idempotency_key TO fyniq_api;
