-- A page rendered from the approval policies cites one source per object type, more than the 50 a page of notes needs (PRD-001 AI-50;
-- p1-knowledge-base task 5.1): the limit of a version's sources is 200. Widened NOT VALID, validated by 000565 (squawk:
-- constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.kb_page_version DROP CONSTRAINT kb_page_version_sources;
ALTER TABLE fyniq.kb_page_version ADD CONSTRAINT kb_page_version_sources
  CHECK (jsonb_typeof(sources) = 'array' AND jsonb_array_length(sources) <= 200) NOT VALID;
