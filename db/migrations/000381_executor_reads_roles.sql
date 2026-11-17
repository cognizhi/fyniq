-- The executor reads who may still do a proposal (PRD-001 AI-11; p1-ai-proposals-core task 5.1, design D6).
--
-- Before it runs an approved proposal, the executor checks that the person who approved it still holds the right its
-- command needs: their membership and its live roles. It reads them and nothing else of the members; under FORCE RLS,
-- in the proposal's organisation alone.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

GRANT SELECT ON fyniq.user_membership, fyniq.role_assignment TO fyniq_executor;
