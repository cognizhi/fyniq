-- When a captured document last changed status (PRD-001 FR-DOC-14; p1-document-capture): a reading that never ended (a crash mid-call) is found by how long it has been
-- Reading, and ended as Failed on the next ask, so no document is stranded.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.document_capture ADD COLUMN status_at timestamptz;
GRANT INSERT (status_at), UPDATE (status_at) ON fyniq.document_capture TO fyniq_api;
