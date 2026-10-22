-- A member invitation's two times come from the same clock, the API's (FR-ADM-01, DEV-05), as an idempotency key's do
-- since 000016.
--
-- 000029 let the database stamp created_at with its own now() while the API set expires_at from its clock, and
-- member_invitation_expiry_after_creation compares the two. In local mode the API's clock can be a business date far
-- from the database's (DEV-05), and the API tests freeze it at 15 Oct 2026 09:00 UTC: from 09:00 UTC on 22 Oct, seven
-- days later, every invitation failed the check, and every test that adds a member with it. The API now sets created_at
-- with expires_at, so the check holds by construction; accept_member_invitation already compares expires_at with the
-- API's time (p_now).
--
-- The check is unchanged. fyniq_api gains INSERT on one column of the invitation it already writes; it already set
-- expires_at, the time that decides anything. The default goes, so an insert that forgets created_at fails loudly
-- instead of mixing the clocks again.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.member_invitation ALTER COLUMN created_at DROP DEFAULT;
GRANT INSERT (created_at) ON fyniq.member_invitation TO fyniq_api;
