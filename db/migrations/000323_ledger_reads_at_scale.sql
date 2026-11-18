-- The indexes the ledger's and the search's reads keep their time on at 10 M lines (PRD-001 NFR-PERF-01, NFR-PERF-02, NFR-PERF-04, INV-11;
-- p1-operations-dr task 13.13):
--
--   - journal_entry_period_revision: a period's entries in revision order, so a month's cash flow, which the checks lay
--     for every open month after a change, reads that month alone instead of walking the whole ledger;
--   - journal_line_account_amount: an account's lines of one amount by date, so the bank import's reconciliation finds a
--     bank line's money among a busy account's lines of that amount alone (at 10 M lines a busy account holds about 600
--     lines a day, and the 15 days either side of a bank line were read for each);
--   - document_type: a type's documents, so a search for text in a type with few documents reads those few, not every
--     document's version newest first looking for one of them (NFR-PERF-01).
-- Built concurrently, outside a transaction (goose's NO TRANSACTION), so entries and lines are never locked against posting.
-- squawk assumes every migration runs in a transaction (.squawk.toml), so the rule against concurrent builds in one is
-- waived here, for this file only.

-- +goose NO TRANSACTION
-- +goose Up
SET lock_timeout = '5s';
SET statement_timeout = '30min';
-- squawk-ignore ban-concurrent-index-creation-in-transaction
CREATE INDEX CONCURRENTLY IF NOT EXISTS journal_entry_period_revision ON fyniq.journal_entry (organisation_id, period_id, ledger_revision);
-- squawk-ignore ban-concurrent-index-creation-in-transaction
CREATE INDEX CONCURRENTLY IF NOT EXISTS journal_line_account_amount ON fyniq.journal_line (organisation_id, account_id, amount, posting_date);
-- squawk-ignore ban-concurrent-index-creation-in-transaction
CREATE INDEX CONCURRENTLY IF NOT EXISTS document_type ON fyniq.document (organisation_id, type);
RESET statement_timeout;
RESET lock_timeout;
