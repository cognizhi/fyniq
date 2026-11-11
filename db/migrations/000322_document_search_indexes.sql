-- The indexes behind lists, the global search and the balance sheet (PRD-001 NFR-PERF-01, NFR-PERF-02, FR-UX-02;
-- p1-operations-dr tasks 13.9, 13.10 and 13.12).
--
--   - document_version_date: a type's documents newest first, read from the index until a page is full;
--   - document_version_search and document_number_search: trigram indexes, so a word of a search finds the versions
--     whose text, and the documents whose number, hold it, without reading the rest;
--   - document_version_customer and _supplier: a contact's documents, for the search by party's name and a contact's page;
--   - journal_entry_source_type: the entries of a source type, such as the opening balances the balance sheet reads apart
--     from the balances (NFR-PERF-02, task 13.12).
-- Built concurrently, outside a transaction (goose's NO TRANSACTION), so documents and entries are never locked against
-- writing.
-- squawk assumes every migration runs in a transaction (.squawk.toml), so the rule against concurrent builds in one is
-- waived here, for this file only.

-- +goose NO TRANSACTION
-- +goose Up
SET lock_timeout = '5s';
SET statement_timeout = '30min';
-- squawk-ignore ban-concurrent-index-creation-in-transaction
CREATE INDEX CONCURRENTLY IF NOT EXISTS document_version_date ON fyniq.document_version (organisation_id, doc_date DESC, document_id DESC);
-- squawk-ignore ban-concurrent-index-creation-in-transaction
CREATE INDEX CONCURRENTLY IF NOT EXISTS document_version_search ON fyniq.document_version USING gin (organisation_id, search_text fyniq_ext.gin_trgm_ops);
-- squawk-ignore ban-concurrent-index-creation-in-transaction
CREATE INDEX CONCURRENTLY IF NOT EXISTS document_number_search ON fyniq.document_number USING gin (organisation_id, lower(number) fyniq_ext.gin_trgm_ops);
-- squawk-ignore ban-concurrent-index-creation-in-transaction
CREATE INDEX CONCURRENTLY IF NOT EXISTS document_version_customer ON fyniq.document_version (organisation_id, (payload->>'customer_id'));
-- squawk-ignore ban-concurrent-index-creation-in-transaction
CREATE INDEX CONCURRENTLY IF NOT EXISTS document_version_supplier ON fyniq.document_version (organisation_id, (payload->>'supplier_id'));
-- squawk-ignore ban-concurrent-index-creation-in-transaction
CREATE INDEX CONCURRENTLY IF NOT EXISTS journal_entry_source_type ON fyniq.journal_entry (organisation_id, source_type, ledger_revision);
RESET statement_timeout;
RESET lock_timeout;
