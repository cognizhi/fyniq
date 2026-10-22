-- Bills (PRD-001 FR-AP-02; p1-purchases task 1.2).
--
-- A supplier's bill is a document of its own: versioned like a journal, posted by the rule bill@1 through the posting
-- service, numbered from its own sequence (FR-ORG-04, BILL- by default, gap-free: numbered when it posts). Its entry
-- holds no new kind of line: the database's checks on entries and lines apply to it as to every posting. The checks
-- are widened NOT VALID and validated by 000076 (squawk: constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.document DROP CONSTRAINT document_type_known;
ALTER TABLE fyniq.document ADD CONSTRAINT document_type_known CHECK (type IN ('journal', 'opening_balance', 'fx_revaluation', 'bill')) NOT VALID;
ALTER TABLE fyniq.document_sequence DROP CONSTRAINT document_sequence_type_known;
ALTER TABLE fyniq.document_sequence ADD CONSTRAINT document_sequence_type_known CHECK (document_type IN ('journal', 'bill')) NOT VALID;
