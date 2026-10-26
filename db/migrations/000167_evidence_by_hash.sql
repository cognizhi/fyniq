-- Evidence found by its file's hash (PRD-001 FR-AP-15; p1-expense-claims task 5.1, design D6).
--
-- A receipt already claimed or captured by anyone is a duplicate (UC-EXP-01): a claim looks up each receipt's SHA-256,
-- as received and as stored, across the organisation's evidence. The indexes are built concurrently, outside a
-- transaction (goose's NO TRANSACTION), so the table is never locked against writes. squawk assumes every migration
-- runs in a transaction (.squawk.toml), so the rule against concurrent builds in one is waived here, for this file only.

-- +goose NO TRANSACTION
-- +goose Up
SET lock_timeout = '5s';
SET statement_timeout = '15min';
-- squawk-ignore ban-concurrent-index-creation-in-transaction
CREATE INDEX CONCURRENTLY IF NOT EXISTS evidence_original_sha256 ON fyniq.evidence (organisation_id, original_sha256);
-- squawk-ignore ban-concurrent-index-creation-in-transaction
CREATE INDEX CONCURRENTLY IF NOT EXISTS evidence_sha256 ON fyniq.evidence (organisation_id, sha256);
RESET statement_timeout;
RESET lock_timeout;
