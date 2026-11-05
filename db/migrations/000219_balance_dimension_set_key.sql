-- The balances cache's key by dimension set (PRD-001 FR-DIM-02, §11.4; p1-organisation-chart task 3.3, slice 1): account
-- × period × dimension set × currency, NULL being the set of no dimension, so the lines with none keep one row as before.
-- Built concurrently, outside a transaction (goose's NO TRANSACTION), as 000200's are: squawk assumes every migration
-- runs in a transaction (.squawk.toml), so the rule against concurrent builds in one is waived here, for this file only.
-- 000220 moves the posting trigger onto it and drops the old key.

-- +goose NO TRANSACTION
-- +goose Up
SET lock_timeout = '5s';
SET statement_timeout = '15min';
-- squawk-ignore ban-concurrent-index-creation-in-transaction
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS balance_set_key
  ON fyniq.balance (organisation_id, account_id, period_id, dimension_set_id, currency) NULLS NOT DISTINCT;
RESET statement_timeout;
RESET lock_timeout;
