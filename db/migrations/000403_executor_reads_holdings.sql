-- The executor reads the register of members (PRD-001 FR-CS-05; p1-company-secretary task 4.2, design D6).
--
-- sec.draft_document fills a members' written resolution with the members holding shares on its day, as the approving
-- person sees them in the equity register, and checks the text it drafts is the text that person approved (AI-P5). The
-- executor's role holds the grants its task types need and no more: this is a read, and it never writes a holding.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

GRANT SELECT ON fyniq.holding_movement TO fyniq_executor;
