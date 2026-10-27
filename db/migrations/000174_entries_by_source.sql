-- Entries found by their source document (PRD-001 FR-FA-02, ENG-03; p1-fixed-assets task 2.2, design D3).
--
-- An asset's depreciation is one entry a month, each naming the asset as its source: the register reads an asset's
-- entries by its ID, and so do the guards that look for a document's entries. The index is built concurrently, outside
-- a transaction (goose's NO TRANSACTION), so the ledger is never locked against posting. squawk assumes every migration
-- runs in a transaction (.squawk.toml), so the rule against concurrent builds in one is waived here, for this file only.

-- +goose NO TRANSACTION
-- +goose Up
SET lock_timeout = '5s';
SET statement_timeout = '15min';
-- squawk-ignore ban-concurrent-index-creation-in-transaction
CREATE INDEX CONCURRENTLY IF NOT EXISTS journal_entry_source_idx ON fyniq.journal_entry (organisation_id, source_id);
RESET statement_timeout;
RESET lock_timeout;
