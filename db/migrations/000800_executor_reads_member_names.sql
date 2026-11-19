-- The executor reads the organisation's members' display names (PRD-001 Appendix E ar.draft_invoice; p1-projects, the AI's draft).
--
-- An approved ar.draft_invoice proposal drafts the same invoice a person's Draft an invoice makes (projectbilling.InvoiceDraft), and a
-- line for time names the person who did it. The executor reads names through the function the API uses, which returns this organisation's
-- members' user IDs and display names and nothing else (no email, no sign-in, no role): the grant is one function's EXECUTE, as for
-- member_emails() (000388). The executor's other reads (projects, time, billing rates, bills and claims) were granted with each table.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

GRANT EXECUTE ON FUNCTION fyniq.member_names() TO fyniq_executor;

-- +goose Down
REVOKE EXECUTE ON FUNCTION fyniq.member_names() FROM fyniq_executor;
