-- Exports that run in the background (PRD-001 FR-RPT-09, FR-RPT-10; p1-reports tasks 3.3 and 3.4, design D5).
--
-- A report export is asked for by a person and prepared by the worker: the accountant exports (the general ledger, the
-- trial balance, the journals, the fixed asset schedule, and the receivables and payables in detail), as CSV or XLSX.
-- The file is kept in the blob store under exports/<organisation>/<export>/ and its row says what it is, its SHA-256
-- and the ledger revision its figures are read at; it can be downloaded by the person who asked for it until it
-- expires, 24 hours after it is ready (a setting), and is then deleted. The database is the backstop:
--   - the API asks: it writes the report, format, parameters and who asked, and nothing else (FY243 refuses an export
--     written ready or failed);
--   - the worker prepares: it moves a pending export to ready, with its file, hash, revision and expiry, or to failed
--     with why; an expiry job moves a ready one to expired. Nothing else changes, and nothing moves back (FY243);
--   - every change is audited on the export in the same transaction (AUD-01).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.report_export (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  report          text        NOT NULL,
  format          text        NOT NULL,
  parameters      jsonb       NOT NULL,
  requested_by    text        NOT NULL,
  requested_at    timestamptz NOT NULL,
  status          text        NOT NULL DEFAULT 'pending',
  ledger_revision bigint,
  file_name       text,
  media_type      text,
  size_bytes      bigint,
  file_sha256     bytea,
  rows_count      bigint,
  ready_at        timestamptz,
  expires_at      timestamptz,
  failure         text,
  CONSTRAINT report_export_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT report_export_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT report_export_report_known CHECK (report IN ('general_ledger', 'trial_balance', 'journals', 'fixed_assets', 'receivables',
                                                          'payables')),
  CONSTRAINT report_export_format_known CHECK (format IN ('csv', 'xlsx')),
  CONSTRAINT report_export_status_known CHECK (status IN ('pending', 'ready', 'failed', 'expired')),
  CONSTRAINT report_export_parameters_object CHECK (jsonb_typeof(parameters) = 'object'),
  CONSTRAINT report_export_ready_whole CHECK (status NOT IN ('ready', 'expired') OR (ledger_revision IS NOT NULL AND file_name IS NOT NULL
    AND media_type IS NOT NULL AND size_bytes IS NOT NULL AND file_sha256 IS NOT NULL AND rows_count IS NOT NULL AND ready_at IS NOT NULL
    AND expires_at > ready_at)),
  CONSTRAINT report_export_failed_says_why CHECK (status <> 'failed' OR length(btrim(failure)) BETWEEN 1 AND 500),
  CONSTRAINT report_export_file_name_length CHECK (length(file_name) BETWEEN 1 AND 200)
);

CREATE INDEX report_export_requested ON fyniq.report_export (organisation_id, requested_by, requested_at DESC);
CREATE INDEX report_export_expiring ON fyniq.report_export (organisation_id, status, expires_at);

-- +goose StatementBegin
-- An export is written pending and then moves once: to ready or failed, and a ready one to expired. What was asked for
-- never changes, nor what a ready export holds (FY243).
CREATE FUNCTION fyniq.report_export_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF NEW.status <> 'pending' OR NEW.ledger_revision IS NOT NULL OR NEW.file_name IS NOT NULL OR NEW.ready_at IS NOT NULL
       OR NEW.expires_at IS NOT NULL OR NEW.failure IS NOT NULL THEN
      RAISE EXCEPTION 'An export is asked for pending, with nothing prepared'
        USING ERRCODE = 'FY243', HINT = 'Ask for the export; the worker prepares it (FR-RPT-09).';
    END IF;
    RETURN NEW;
  END IF;
  IF TG_OP <> 'UPDATE' THEN
    RAISE EXCEPTION 'Export % is never deleted or truncated', OLD.id
      USING ERRCODE = 'FY243', HINT = 'An export expires; its row stays as the record of what was exported (FR-RPT-09).';
  END IF;
  IF NEW.report IS DISTINCT FROM OLD.report OR NEW.format IS DISTINCT FROM OLD.format OR NEW.parameters IS DISTINCT FROM OLD.parameters
     OR NEW.requested_by IS DISTINCT FROM OLD.requested_by OR NEW.requested_at IS DISTINCT FROM OLD.requested_at
     OR NOT ((OLD.status = 'pending' AND NEW.status IN ('ready', 'failed')) OR (OLD.status = 'ready' AND NEW.status = 'expired'))
     OR (OLD.status = 'ready' AND (NEW.ledger_revision, NEW.file_name, NEW.media_type, NEW.size_bytes, NEW.file_sha256, NEW.rows_count,
         NEW.ready_at) IS DISTINCT FROM (OLD.ledger_revision, OLD.file_name, OLD.media_type, OLD.size_bytes, OLD.file_sha256,
         OLD.rows_count, OLD.ready_at)) THEN
    RAISE EXCEPTION 'Export % cannot move from % to % or change what it holds', OLD.id, OLD.status, NEW.status
      USING ERRCODE = 'FY243', HINT = 'A pending export becomes ready or failed, and a ready one expires (FR-RPT-09).';
  END IF;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER report_export_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.report_export
  FOR EACH ROW EXECUTE FUNCTION fyniq.report_export_guard();
CREATE TRIGGER report_export_no_truncate BEFORE TRUNCATE ON fyniq.report_export
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.report_export_guard();

ALTER TABLE fyniq.report_export ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.report_export FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.report_export
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

-- Asked for, prepared and expired: each audited on the export in the same transaction (AUD-01).
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.report_export
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('report_export', 'id');

REVOKE ALL ON fyniq.report_export FROM PUBLIC;
GRANT SELECT ON fyniq.report_export TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (report, format, parameters, requested_by, requested_at) ON fyniq.report_export TO fyniq_api;
GRANT UPDATE (status, ledger_revision, file_name, media_type, size_bytes, file_sha256, rows_count, ready_at, expires_at, failure)
  ON fyniq.report_export TO fyniq_worker;
