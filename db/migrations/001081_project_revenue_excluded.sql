-- Costs that do not show progress (PRD-001 FR-PRJ-11, rev 0.580; p2-customer-portal-subscriptions task 4.1 leftovers).
--
-- The input method measures progress by the costs that show it, and leaves out the rest (SFRS(I) 15 B19: waste, rework, a cost not in proportion to the work). The accountant
-- enters the running total of what to leave out of the project's posted costs, with the reason, on the election, which changes by version and is audited.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.project_revenue_election
  ADD COLUMN excluded_cost   numeric(20,4) NOT NULL DEFAULT 0,
  ADD COLUMN excluded_reason text;
ALTER TABLE fyniq.project_revenue_election
  ADD CONSTRAINT project_revenue_election_excluded_form CHECK (excluded_cost <> 'NaN' AND excluded_cost >= 0
    AND ((excluded_cost = 0) = (excluded_reason IS NULL)) AND (excluded_reason IS NULL OR length(excluded_reason) BETWEEN 1 AND 500)) NOT VALID;

GRANT INSERT (excluded_cost, excluded_reason) ON fyniq.project_revenue_election TO fyniq_api;
GRANT UPDATE (excluded_cost, excluded_reason) ON fyniq.project_revenue_election TO fyniq_api;
