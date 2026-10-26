-- Company-card spend (PRD-001 FR-AP-18; p1-expense-claims task 7.1, design D9).
--
-- A card money account names its cardholder, a member, whose My claims shows the card's lines to complete with a
-- receipt and a category; only a card has one. The foreign key and the check are added NOT VALID and validated by
-- 000171 (squawk: constraint-missing-not-valid). Whether the cardholder is an active member is the application's to
-- check when it is set, as it is for a claimant.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.money_account ADD COLUMN cardholder_id uuid;
ALTER TABLE fyniq.money_account ADD CONSTRAINT money_account_cardholder_fkey FOREIGN KEY (cardholder_id)
  REFERENCES fyniq.app_user (user_id) NOT VALID;
ALTER TABLE fyniq.money_account ADD CONSTRAINT money_account_cardholder_card CHECK (cardholder_id IS NULL OR kind = 'card') NOT VALID;
-- Whoever sets up or changes a money account names a card's holder.
GRANT INSERT (cardholder_id), UPDATE (cardholder_id) ON fyniq.money_account TO fyniq_api;
