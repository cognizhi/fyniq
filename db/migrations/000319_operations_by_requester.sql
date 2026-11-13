-- Operations found by who asked for them (PRD-001 FR-INT-11; p1-operations-dr task 11.1, design D10).
--
-- The operations tray lists the caller's own operations, newest first, a page at a time (listMyOperations). The index is
-- built concurrently, outside a transaction (goose's NO TRANSACTION), so asking for long work is never locked out while it
-- builds. squawk assumes every migration runs in a transaction (.squawk.toml), so the rule against concurrent builds in
-- one is waived here, for this file only.

-- +goose NO TRANSACTION
-- +goose Up
SET lock_timeout = '5s';
SET statement_timeout = '15min';
-- squawk-ignore ban-concurrent-index-creation-in-transaction
CREATE INDEX CONCURRENTLY IF NOT EXISTS operation_requested ON fyniq.operation (organisation_id, requested_by, requested_at DESC, id DESC);
RESET statement_timeout;
RESET lock_timeout;
