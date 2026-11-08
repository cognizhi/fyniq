-- The ACRA steps of an equity transaction (PRD-001 FR-EQ-17, FR-EQ-09; rev 0.206; p1-equity-register slice 2b, design D4).
--
-- An approved equity transaction records its steps as they happen, each an equity_step row of its transaction, as a
-- founder's conversion records its own (000217):
--   - a cash issue: the day the shareholders resolved, the day their approval was lodged with ACRA, the date by which
--     the next AGM must be held, and the day the return of allotment was lodged;
--   - a transfer: the day the instrument of transfer was signed, and the day the notice of transfer was lodged;
--   - a conversion between classes: the day the shareholders resolved, and the day it was lodged.
-- The effective date is the transaction's movements' (000252). A later row of a kind supersedes the earlier one, which
-- stays. The database is the backstop: a step is of a kind its transaction takes, names an approved transaction that is
-- not effective yet, and is never changed, deleted or truncated (FY290).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.equity_step (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  transaction_id  uuid        NOT NULL,
  kind            text        NOT NULL,
  on_date         date        NOT NULL,
  reference       text,       -- ACRA's transaction number, or what the step rests on
  recorded_by     uuid        NOT NULL,
  recorded_at     timestamptz NOT NULL,
  CONSTRAINT equity_step_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT equity_step_transaction_fkey FOREIGN KEY (organisation_id, transaction_id) REFERENCES fyniq.document (organisation_id, id),
  CONSTRAINT equity_step_recorded_by_fkey FOREIGN KEY (recorded_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT equity_step_kind_known CHECK (kind IN ('resolved', 'approval_lodged', 'agm_due', 'allotment_lodged', 'instrument_signed',
                                                    'transfer_lodged', 'conversion_lodged')),
  CONSTRAINT equity_step_reference_length CHECK (reference IS NULL OR length(btrim(reference)) BETWEEN 1 AND 200)
);
CREATE INDEX equity_step_transaction ON fyniq.equity_step (organisation_id, transaction_id);

-- +goose StatementBegin
-- A step is of a kind its transaction takes, names an approved equity transaction that is not effective yet, and is
-- never changed, deleted or truncated (FY290).
CREATE FUNCTION fyniq.equity_step_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  k text;
BEGIN
  IF TG_OP = 'TRUNCATE' THEN
    RAISE EXCEPTION 'The steps of equity transactions are never deleted' USING ERRCODE = 'FY290';
  END IF;
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'Step % of an equity transaction is never changed or deleted', OLD.id USING ERRCODE = 'FY290',
      HINT = 'Record the step again: the later one supersedes it, and both stay (FR-EQ-09).';
  END IF;
  SELECT v.payload->>'kind' INTO k
    FROM fyniq.document d
    JOIN fyniq.document_version v ON v.organisation_id = d.organisation_id AND v.document_id = d.id AND v.version = d.current_version
   WHERE d.organisation_id = NEW.organisation_id AND d.id = NEW.transaction_id AND d.type = 'equity_transaction' AND d.status = 'posted';
  IF k IS NULL OR EXISTS (SELECT 1 FROM fyniq.holding_movement m WHERE m.organisation_id = NEW.organisation_id AND m.transaction_id = NEW.transaction_id) THEN
    RAISE EXCEPTION 'Equity transaction % is not approved and waiting for ACRA', NEW.transaction_id USING ERRCODE = 'FY290',
      HINT = 'Record the ACRA steps of a transaction between its approval and its effective date (FR-EQ-09).';
  END IF;
  IF NOT ((k = 'issue_cash' AND NEW.kind IN ('resolved', 'approval_lodged', 'agm_due', 'allotment_lodged'))
          OR (k = 'transfer' AND NEW.kind IN ('instrument_signed', 'transfer_lodged'))
          OR (k = 'class_conversion' AND NEW.kind IN ('resolved', 'conversion_lodged'))) THEN
    RAISE EXCEPTION 'A % has no step %', replace(k, '_', ' '), replace(NEW.kind, '_', ' ') USING ERRCODE = 'FY290',
      HINT = 'Record the steps its kind takes (FR-EQ-09, FR-EQ-17).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER equity_step_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.equity_step
  FOR EACH ROW EXECUTE FUNCTION fyniq.equity_step_guard();
CREATE TRIGGER equity_step_no_truncate BEFORE TRUNCATE ON fyniq.equity_step
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.equity_step_guard();

ALTER TABLE fyniq.equity_step ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.equity_step FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.equity_step
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

-- Each step is audited on its transaction in the same transaction (AUD-01).
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.equity_step
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('document', 'transaction_id');

REVOKE ALL ON fyniq.equity_step FROM PUBLIC;
GRANT SELECT ON fyniq.equity_step TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (transaction_id, kind, on_date, reference, recorded_by, recorded_at) ON fyniq.equity_step TO fyniq_api;
