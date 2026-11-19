-- The kind of a permanent resident's joint election (PRD-001 FR-PAY-14; p1-nemura-payroll, the oracle review's F/F item).
--
-- A joint application to the CPF Board (Form JAP/94) is for full employer rates with graduated employee rates (F/G) or full rates for both (F/F, which is the full table's, `sc_spr3`). The kind is
-- written with the election and, like the rest of it, never changes: a different kind is a new election from a later day. An election recorded before this column is F/G.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE nemura.pr_election ADD COLUMN kind text NOT NULL DEFAULT 'fg';
ALTER TABLE nemura.pr_election ADD CONSTRAINT pr_election_kind_known CHECK (kind IN ('fg', 'ff')) NOT VALID;
-- (a table of a few rows, every one of which has the default kind)
-- squawk-ignore constraint-missing-not-valid
ALTER TABLE nemura.pr_election VALIDATE CONSTRAINT pr_election_kind_known;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION nemura.pr_election_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR NEW.person_id IS DISTINCT FROM OLD.person_id OR NEW.effective_from IS DISTINCT FROM OLD.effective_from OR NEW.evidence_id IS DISTINCT FROM OLD.evidence_id
     OR NEW.kind IS DISTINCT FROM OLD.kind OR (OLD.effective_to IS NOT NULL AND NEW.effective_to IS DISTINCT FROM OLD.effective_to) THEN
    RAISE EXCEPTION 'An election is history: it can only be ended, once' USING ERRCODE = 'NM171';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

GRANT INSERT (kind) ON nemura.pr_election TO nemura_api;
