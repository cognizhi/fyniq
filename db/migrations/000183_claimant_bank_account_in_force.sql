-- One bank account in force per person and currency (PRD-001 FR-AP-22; p1-payments-out task 1.1, design D9).
--
-- 000180's payee_bank_account_in_force keeps one account in force per vendor and currency; a person's account has no
-- vendor, so its own partial index keeps one per person and currency. The index is built concurrently, outside a
-- transaction (goose's NO TRANSACTION), as 000174's is: squawk assumes every migration runs in a transaction
-- (.squawk.toml), so the rule against concurrent builds in one is waived here, for this file only.

-- +goose NO TRANSACTION
-- +goose Up
SET lock_timeout = '5s';
SET statement_timeout = '15min';
-- squawk-ignore ban-concurrent-index-creation-in-transaction
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS payee_bank_account_member_in_force
  ON fyniq.payee_bank_account (organisation_id, member_user_id, currency) WHERE state = 'in_force' AND member_user_id IS NOT NULL;
RESET statement_timeout;
RESET lock_timeout;
