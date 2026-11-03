-- The indexes of director accounts (PRD-001 FR-EQ-01, B.7; p1-founders-money task 1.1, design D1 and D4).
--
-- journal_line_party_idx serves each person's balance and statement (balance.People, balance.PersonLedger) and 000199's
-- FY249 guard; contact_member_once links a member to one contact at most. Both are built concurrently, outside a
-- transaction (goose's NO TRANSACTION), as 000183's is: squawk assumes every migration runs in a transaction
-- (.squawk.toml), so the rule against concurrent builds in one is waived here, for this file only.

-- +goose NO TRANSACTION
-- +goose Up
SET lock_timeout = '5s';
SET statement_timeout = '15min';
-- squawk-ignore ban-concurrent-index-creation-in-transaction
CREATE INDEX CONCURRENTLY IF NOT EXISTS journal_line_party_idx
  ON fyniq.journal_line (organisation_id, party_id, posting_date) WHERE party_id IS NOT NULL;
-- squawk-ignore ban-concurrent-index-creation-in-transaction
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS contact_member_once
  ON fyniq.contact (organisation_id, member_user_id) WHERE member_user_id IS NOT NULL;
RESET statement_timeout;
RESET lock_timeout;
