-- The index of 2700's lines by instrument (PRD-001 FR-EQ-18; p1-equity-register slice 4a-ii).
--
-- journal_line_instrument_idx serves each instrument's balance (balance.Instrument). It is built concurrently, outside a
-- transaction (goose's NO TRANSACTION), as 000200's is: squawk assumes every migration runs in a transaction
-- (.squawk.toml), so the rule against concurrent builds in one is waived here, for this file only.

-- +goose NO TRANSACTION
-- +goose Up
SET lock_timeout = '5s';
SET statement_timeout = '15min';
-- squawk-ignore ban-concurrent-index-creation-in-transaction
CREATE INDEX CONCURRENTLY IF NOT EXISTS journal_line_instrument_idx
  ON fyniq.journal_line (organisation_id, instrument_id, posting_date) WHERE instrument_id IS NOT NULL;
RESET statement_timeout;
RESET lock_timeout;
