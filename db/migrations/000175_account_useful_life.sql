-- The default useful life of an asset account (PRD-001 FR-COA-02, FR-FA-01; rev 0.151; p1-fixed-assets task 7.2, design
-- D8).
--
-- account.useful_life_months is the life in whole months that the fixed asset register's form offers for an asset on
-- the account: null for none, else 1 to 600. The chart's rules keep it to the register's report groups (COA-11); the
-- database keeps its range. It is a default only: a registered asset keeps its own life whatever the account says
-- later. The check is added NOT VALID and validated by 000176 (squawk: constraint-missing-not-valid). Accounts loaded
-- before this migration have none until the chart says otherwise: an account changes only with its audit event.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.account ADD COLUMN useful_life_months smallint;
ALTER TABLE fyniq.account ADD CONSTRAINT account_useful_life_months CHECK (useful_life_months BETWEEN 1 AND 600) NOT VALID;
GRANT INSERT (useful_life_months), UPDATE (useful_life_months) ON fyniq.account TO fyniq_api;
