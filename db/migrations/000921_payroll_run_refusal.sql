-- The record of the pay runs Fyniq refused (PRD-001 C.1 #19 "Pay lines that cannot post", FR-PPL-04, NM-INT-03): a run Nemura sent with its proof and Fyniq's checks or the ledger refused waits in Nemura with the code, and
-- until now the refusal was only in the audit trail. Each refusal is kept (the link, the run, its version, the code, when), append-only, with no figure, name or payload; the check lists the runs whose newest
-- refusal no posted version has followed.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.payroll_run_refusal (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  link_id         uuid        NOT NULL,
  run_id          uuid        NOT NULL,
  version         integer     NOT NULL,
  code            text        NOT NULL,
  refused_at      timestamptz NOT NULL,
  CONSTRAINT payroll_run_refusal_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT payroll_run_refusal_link_fkey FOREIGN KEY (organisation_id, link_id) REFERENCES fyniq.people_link (organisation_id, id),
  CONSTRAINT payroll_run_refusal_version CHECK (version >= 1),
  CONSTRAINT payroll_run_refusal_code CHECK (code ~ '^[A-Za-z0-9_.-]{3,80}$')
);
CREATE INDEX payroll_run_refusal_run ON fyniq.payroll_run_refusal (organisation_id, run_id, version);
CREATE TRIGGER payroll_run_refusal_guard BEFORE UPDATE OR DELETE ON fyniq.payroll_run_refusal
  FOR EACH ROW EXECUTE FUNCTION fyniq.notification_append_only();
CREATE TRIGGER payroll_run_refusal_no_truncate BEFORE TRUNCATE ON fyniq.payroll_run_refusal
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.payroll_run_refusal
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('payroll_run_refusal', 'id');

ALTER TABLE fyniq.payroll_run_refusal ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.payroll_run_refusal FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.payroll_run_refusal
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

REVOKE ALL ON fyniq.payroll_run_refusal FROM PUBLIC;
GRANT SELECT ON fyniq.payroll_run_refusal TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (link_id, run_id, version, code, refused_at) ON fyniq.payroll_run_refusal TO fyniq_api;
