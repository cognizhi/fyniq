-- The entries of a kind of source recorded since a time, as the nightly review of unusual items reads the last week's
-- journals and payments and the half year before them (PRD-001 FM-6, rev 0.287), without reading the whole ledger.
-- Built concurrently, outside a transaction (goose's NO TRANSACTION), so postings are never locked against it; squawk's
-- rule against a concurrent build in a transaction is waived for this file only, as for 000363.

-- +goose NO TRANSACTION
-- +goose Up
SET lock_timeout = '5s';
SET statement_timeout = '30min';
-- squawk-ignore ban-concurrent-index-creation-in-transaction
CREATE INDEX CONCURRENTLY IF NOT EXISTS journal_entry_recorded ON fyniq.journal_entry (organisation_id, source_type, recorded_at);
RESET statement_timeout;
RESET lock_timeout;
