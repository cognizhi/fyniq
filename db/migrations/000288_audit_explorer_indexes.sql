-- The audit trail explorer's indexes (PRD-001 FR-CTL-03, AUD-07; p1-audit-explorer slice 6, design D6).
--
-- The explorer lists an organisation's events newest first, filtered by the actor, the record or the action. The indexes
-- are built concurrently, outside a transaction (goose's NO TRANSACTION), so the trail is never locked against appends.
-- squawk assumes every migration runs in a transaction (.squawk.toml), so the rule against concurrent builds in one is
-- waived here, for this file only. Other filters (a field, an approval, the AI originator, the days) narrow these.

-- +goose NO TRANSACTION
-- +goose Up
SET lock_timeout = '5s';
SET statement_timeout = '15min';
-- squawk-ignore ban-concurrent-index-creation-in-transaction
CREATE INDEX CONCURRENTLY IF NOT EXISTS audit_event_actor_idx ON fyniq.audit_event (organisation_id, ((body::jsonb -> 'actor' ->> 'id')), seq);
-- squawk-ignore ban-concurrent-index-creation-in-transaction
CREATE INDEX CONCURRENTLY IF NOT EXISTS audit_event_record_idx ON fyniq.audit_event (organisation_id, object_type, object_id, seq);
-- squawk-ignore ban-concurrent-index-creation-in-transaction
CREATE INDEX CONCURRENTLY IF NOT EXISTS audit_event_action_idx ON fyniq.audit_event (organisation_id, action, seq);
RESET statement_timeout;
RESET lock_timeout;
