-- The indexes behind the AI activity log (PRD-001 AI-12; p1-ai-providers task 6.1, design D9).
--
--   - ai_call_by_time: the organisation's attempts newest first, as the Owner and the DPO read them;
--   - ai_call_by_actor: one person's attempts newest first, as each member reads their own.
-- Built concurrently, outside a transaction (goose's NO TRANSACTION), so the calls are never locked against recording.
-- squawk assumes every migration runs in a transaction (.squawk.toml), so the rule against concurrent builds in one is
-- waived here, for this file only.

-- +goose NO TRANSACTION
-- +goose Up
SET lock_timeout = '5s';
SET statement_timeout = '30min';
-- squawk-ignore ban-concurrent-index-creation-in-transaction
CREATE INDEX CONCURRENTLY IF NOT EXISTS ai_call_by_time ON fyniq.ai_call (organisation_id, started_at DESC, id DESC);
-- squawk-ignore ban-concurrent-index-creation-in-transaction
CREATE INDEX CONCURRENTLY IF NOT EXISTS ai_call_by_actor ON fyniq.ai_call (organisation_id, actor, started_at DESC, id DESC);
RESET statement_timeout;
RESET lock_timeout;
