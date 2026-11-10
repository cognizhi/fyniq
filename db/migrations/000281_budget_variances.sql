-- Budget variances in the close (PRD-001 FR-GL-14; p1-planning task 5.3, design D6): the close compares each income or
-- expense account's total for the month with the month's budget for it, at the same thresholds as the month-on-month
-- comparison, once the month's financial year has a recorded budget. An explanation now says which comparison it
-- explains: kind `month` (the previous month, as every explanation so far) or `budget`, when previous_amount holds the
-- month's budget for the account in the base currency, debit positive, as the ledger signs it. The two kinds are kept
-- apart: each account has a latest explanation of each kind for the period, and each goes stale when either of its two
-- figures changes. The guard is unchanged: either kind is of an income or expense account, in an open period, and never
-- changes or goes (FY162, FY163).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.period_variance_explanation ADD COLUMN kind text NOT NULL DEFAULT 'month';
ALTER TABLE fyniq.period_variance_explanation
  ADD CONSTRAINT period_variance_explanation_kind_known CHECK (kind IN ('month', 'budget')) NOT VALID;

GRANT INSERT (kind) ON fyniq.period_variance_explanation TO fyniq_api;
