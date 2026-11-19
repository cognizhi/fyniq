-- Validates 000561's check (every existing row holds the default).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.ai_tool_call VALIDATE CONSTRAINT ai_tool_call_kb_pages;
