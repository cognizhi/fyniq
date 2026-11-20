-- Payroll that is not run in Nemura (PRD-001 FR-PAY-15, FR-PPL-05, FR-PPL-09; PRD-002 NM-EXT-01 to NM-EXT-03; p1-payroll-link tasks 7.1, 7.2, design D12). Two things live here:
--
--  * payroll_arrangement: how a month's statutory outputs are met, recorded as evidence (the payroll go-live gate, FR-PAY-15): itemised payslips, KETs, CPF, SDL and SHG submission and payment,
--    AIS and IR21 where they apply. Each row is an item, a method (a provider, a manual process, Nemura, or not applicable with a reason), the evidence file that shows it, and the month it holds from;
--    the latest row of an item in force for a wage month is the arrangement. A pay run that pays a person cannot be approved for payment while an item has none (G4-009).
--  * payroll_import: a provider's payroll journal, or a file import, as a draft journal under Journals (FR-PPL-05): one per source system, pay period and version, once, with the file's SHA-256 and the
--    totals it states by account and department, so a later version posts only the difference. The external system's approval cannot be verified, so the journal is only ever a draft that a
--    Controller or Accountant approves (NM-EXT-03).
-- Both are append-only.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.payroll_arrangement (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  item            text        NOT NULL,
  method          text        NOT NULL,
  provider        text,
  note            text        NOT NULL DEFAULT '',
  evidence_id     uuid,
  effective_from  date        NOT NULL,
  recorded_by     text        NOT NULL,
  recorded_at     timestamptz NOT NULL,
  CONSTRAINT payroll_arrangement_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT payroll_arrangement_evidence_fkey FOREIGN KEY (organisation_id, evidence_id) REFERENCES fyniq.evidence (organisation_id, id),
  CONSTRAINT payroll_arrangement_item_known CHECK (item IN ('payslips', 'kets', 'cpf_sdl_shg', 'ais', 'ir21')),
  CONSTRAINT payroll_arrangement_method_known CHECK (method IN ('nemura', 'provider', 'manual', 'not_applicable')),
  -- Evidence shows every arrangement that Fyniq cannot see for itself: a provider's, a manual process and a "not applicable" with its reason (FR-DOC-05). Nemura's own is the link's.
  CONSTRAINT payroll_arrangement_evidence CHECK (method = 'nemura' OR evidence_id IS NOT NULL),
  CONSTRAINT payroll_arrangement_provider CHECK ((method = 'provider') = (provider IS NOT NULL AND length(btrim(provider)) BETWEEN 1 AND 120)),
  CONSTRAINT payroll_arrangement_nemura_item CHECK (method <> 'nemura' OR item IN ('payslips', 'kets')),
  CONSTRAINT payroll_arrangement_not_applicable CHECK (method <> 'not_applicable' OR (item IN ('ais', 'ir21') AND length(btrim(note)) BETWEEN 1 AND 500)),
  CONSTRAINT payroll_arrangement_note_length CHECK (length(note) <= 500),
  CONSTRAINT payroll_arrangement_month CHECK (effective_from = date_trunc('month', effective_from)::date),
  CONSTRAINT payroll_arrangement_by_form CHECK (recorded_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);
CREATE INDEX payroll_arrangement_item ON fyniq.payroll_arrangement (organisation_id, item, effective_from DESC, recorded_at DESC);

CREATE TABLE fyniq.payroll_import (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  source_system   text        NOT NULL,
  period          date        NOT NULL,
  version         integer     NOT NULL,
  file_sha256     text        NOT NULL,
  journal_id      uuid        NOT NULL,
  line_count      integer     NOT NULL,
  totals          jsonb       NOT NULL,    -- the period's totals by account|values after this version, signed base amounts: what the next version's difference is worked out from
  created_by      text        NOT NULL,
  created_at      timestamptz NOT NULL,
  CONSTRAINT payroll_import_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT payroll_import_version_key UNIQUE (organisation_id, source_system, period, version),
  CONSTRAINT payroll_import_journal_fkey FOREIGN KEY (organisation_id, journal_id) REFERENCES fyniq.document (organisation_id, id),
  CONSTRAINT payroll_import_system CHECK (source_system ~ '^[a-z][a-z0-9_-]{0,39}$'),
  CONSTRAINT payroll_import_period CHECK (period = date_trunc('month', period)::date),
  CONSTRAINT payroll_import_version_positive CHECK (version >= 1 AND line_count >= 0),
  CONSTRAINT payroll_import_digest CHECK (file_sha256 ~ '^[0-9a-f]{64}$'),
  CONSTRAINT payroll_import_by_form CHECK (created_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);

CREATE TRIGGER payroll_arrangement_guard BEFORE UPDATE OR DELETE ON fyniq.payroll_arrangement FOR EACH ROW EXECUTE FUNCTION fyniq.notification_append_only();
CREATE TRIGGER payroll_import_guard BEFORE UPDATE OR DELETE ON fyniq.payroll_import FOR EACH ROW EXECUTE FUNCTION fyniq.notification_append_only();
CREATE TRIGGER payroll_arrangement_no_truncate BEFORE TRUNCATE ON fyniq.payroll_arrangement FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();
CREATE TRIGGER payroll_import_no_truncate BEFORE TRUNCATE ON fyniq.payroll_import FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.payroll_arrangement
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('payroll_arrangement', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.payroll_import
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('payroll_import', 'id');

ALTER TABLE fyniq.payroll_arrangement ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.payroll_arrangement FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.payroll_arrangement USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.payroll_import ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.payroll_import FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.payroll_import USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

REVOKE ALL ON fyniq.payroll_arrangement, fyniq.payroll_import FROM PUBLIC;
GRANT SELECT ON fyniq.payroll_arrangement, fyniq.payroll_import TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (item, method, provider, note, evidence_id, effective_from, recorded_by, recorded_at) ON fyniq.payroll_arrangement TO fyniq_api;
GRANT INSERT (source_system, period, version, file_sha256, journal_id, line_count, totals, created_by, created_at) ON fyniq.payroll_import TO fyniq_api;
