-- One live asset per source line (PRD-001 FR-FA-01; rev 0.151; p1-fixed-assets task 7.3, design D9).
--
-- A voided asset releases its line (000177), so the line can be registered again: the unique index on the line counts
-- only the sources not released. It is built concurrently, outside a transaction (goose's NO TRANSACTION), so
-- registering is never locked out; 000179 then drops fixed_asset_source_line_once, which it replaces. squawk assumes
-- every migration runs in a transaction (.squawk.toml), so the rule against concurrent builds in one is waived here,
-- for this file only.

-- +goose NO TRANSACTION
-- +goose Up
SET lock_timeout = '5s';
SET statement_timeout = '15min';
-- squawk-ignore ban-concurrent-index-creation-in-transaction
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS fixed_asset_source_line_live ON fyniq.fixed_asset_source (organisation_id, source_id, line)
  WHERE NOT released;
RESET statement_timeout;
RESET lock_timeout;
