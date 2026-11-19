-- The knowledge-base page versions an answer used (PRD-001 AI-12, AI-51; p1-knowledge-base task 2.5, design D4).
--
-- A kb.read of a page records which page and which version of it the turn read: for a platform page the release that shipped
-- it and the page file's digest, for an organisation page its number. The activity log shows them, so "why did the assistant
-- say that" is answered with the page's exact text, which the platform keeps per release (git) or per version (the database).
-- The list is a JSON array of {key, version}; most calls carry none. The new check is NOT VALID here and validated by the next
-- migration.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.ai_tool_call ADD COLUMN kb_pages jsonb NOT NULL DEFAULT '[]';
ALTER TABLE fyniq.ai_tool_call ADD CONSTRAINT ai_tool_call_kb_pages
  CHECK (jsonb_typeof(kb_pages) = 'array' AND jsonb_array_length(kb_pages) <= 20 AND pg_column_size(kb_pages) <= 4096) NOT VALID;
GRANT INSERT (kb_pages) ON fyniq.ai_tool_call TO fyniq_api;
