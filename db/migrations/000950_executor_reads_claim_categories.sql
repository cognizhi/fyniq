-- The executor reads the organisation's claim categories (PRD-001 Appendix E exp.draft_claim; p1-ai-proposals task 1.2).
--
-- An approved exp.draft_claim proposal drafts the requester's claim through the same function a person's draft uses
-- (journal.CreateClaimIn), and its lines name their category, which the command reads to find the category's ID by name. The executor
-- reads the categories and nothing more (no write): a grant of SELECT on the one table, as for project_billed_item (000606).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

GRANT SELECT ON fyniq.claim_category TO fyniq_executor;

-- +goose Down
REVOKE SELECT ON fyniq.claim_category FROM fyniq_executor;
