-- The ACRA steps of a conversion to shares (PRD-001 FR-EQ-09; p1-founders-money task 5.4, design D12).
--
-- An approved conversion records its steps as they happen, each a conversion_step row of its settlement: the day the
-- shareholders resolved, the day their approval was lodged with ACRA, the date by which the next AGM must be held (by
-- default worked out from the financial years [verify], recorded when it differs), and the day the return of allotment
-- was lodged; the effective date is the share issue's (000216). A later row of a kind supersedes the earlier one, which
-- stays. The database is the backstop: a step names an approved conversion (a posted director_settlement converting)
-- that is not effective yet, and is never changed, deleted or truncated (FY256).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.conversion_step (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  settlement_id   uuid        NOT NULL,
  kind            text        NOT NULL,
  on_date         date        NOT NULL,
  reference       text,       -- ACRA's transaction number, or what the step rests on
  recorded_by     uuid        NOT NULL,
  recorded_at     timestamptz NOT NULL,
  CONSTRAINT conversion_step_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT conversion_step_settlement_fkey FOREIGN KEY (organisation_id, settlement_id) REFERENCES fyniq.document (organisation_id, id),
  CONSTRAINT conversion_step_recorded_by_fkey FOREIGN KEY (recorded_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT conversion_step_kind_known CHECK (kind IN ('resolved', 'approval_lodged', 'agm_due', 'allotment_lodged')),
  CONSTRAINT conversion_step_reference_length CHECK (reference IS NULL OR length(btrim(reference)) BETWEEN 1 AND 200)
);
CREATE INDEX conversion_step_settlement ON fyniq.conversion_step (organisation_id, settlement_id);

-- +goose StatementBegin
-- A step names an approved conversion that is not effective yet, and is never changed, deleted or truncated (FY256).
CREATE FUNCTION fyniq.conversion_step_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'TRUNCATE' THEN
    RAISE EXCEPTION 'The steps of conversions are never deleted' USING ERRCODE = 'FY256';
  END IF;
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'Step % of a conversion is never changed or deleted', OLD.id USING ERRCODE = 'FY256',
      HINT = 'Record the step again: the later one supersedes it, and both stay (FR-EQ-09).';
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM fyniq.document d
      JOIN fyniq.document_version v ON v.organisation_id = d.organisation_id AND v.document_id = d.id AND v.version = d.current_version
     WHERE d.organisation_id = NEW.organisation_id AND d.id = NEW.settlement_id AND d.type = 'director_settlement' AND d.status = 'posted'
       AND v.payload->>'route' = 'convert'
  ) OR EXISTS (SELECT 1 FROM fyniq.share_issue s WHERE s.organisation_id = NEW.organisation_id AND s.settlement_id = NEW.settlement_id) THEN
    RAISE EXCEPTION 'Settlement % is not an approved conversion waiting for ACRA', NEW.settlement_id USING ERRCODE = 'FY256',
      HINT = 'Record the ACRA steps of a conversion between its approval and its effective date (FR-EQ-09).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER conversion_step_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.conversion_step
  FOR EACH ROW EXECUTE FUNCTION fyniq.conversion_step_guard();
CREATE TRIGGER conversion_step_no_truncate BEFORE TRUNCATE ON fyniq.conversion_step
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.conversion_step_guard();

ALTER TABLE fyniq.conversion_step ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.conversion_step FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.conversion_step
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

-- Each step is audited on its settlement in the same transaction (AUD-01).
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.conversion_step
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('document', 'settlement_id');

REVOKE ALL ON fyniq.conversion_step FROM PUBLIC;
GRANT SELECT ON fyniq.conversion_step TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (settlement_id, kind, on_date, reference, recorded_by, recorded_at) ON fyniq.conversion_step TO fyniq_api;
