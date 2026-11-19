-- The standalone hand-off files of an approved pay run (PRD-002 NM-PAY-05; p1-nemura-payroll slice 6).
--
-- When Nemura is not linked to Fyniq it produces a journal file and a bank payment file for an approved version. The file itself is not kept (the payment file holds bank account numbers): what is kept is its
-- manifest, written once, with the SHA-256 of the exact bytes handed out, so that what left Nemura can be checked afterwards. A file is never proof of payment; no audit event holds an amount or an account.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE nemura.pay_export (
  organisation_id uuid          NOT NULL DEFAULT nemura.current_org(),
  id              uuid          NOT NULL DEFAULT uuidv7(),
  run_id          uuid          NOT NULL,
  version_id      uuid          NOT NULL,
  kind            text          NOT NULL,
  sha256          bytea         NOT NULL,
  line_count      integer       NOT NULL,
  skipped_count   integer       NOT NULL,
  total           numeric(20,4) NOT NULL,   -- the payment file's total, or the journal's debit total
  created_by      uuid          NOT NULL,
  created_at      timestamptz   NOT NULL,
  CONSTRAINT pay_export_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT pay_export_run_fkey FOREIGN KEY (organisation_id, run_id) REFERENCES nemura.pay_run (organisation_id, id),
  CONSTRAINT pay_export_version_fkey FOREIGN KEY (organisation_id, version_id) REFERENCES nemura.pay_run_version (organisation_id, id),
  CONSTRAINT pay_export_by_fkey FOREIGN KEY (organisation_id, created_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT pay_export_kind CHECK (kind IN ('journal', 'payments')),
  CONSTRAINT pay_export_sha CHECK (octet_length(sha256) = 32),
  CONSTRAINT pay_export_counts CHECK (line_count >= 0 AND skipped_count >= 0)
);
CREATE INDEX pay_export_run_idx ON nemura.pay_export (organisation_id, run_id, created_at);
CREATE TRIGGER pay_export_append_only BEFORE UPDATE OR DELETE ON nemura.pay_export FOR EACH ROW EXECUTE FUNCTION nemura.append_only_guard('An export manifest');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON nemura.pay_export DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('pay_export', 'id');

ALTER TABLE nemura.pay_export ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.pay_export FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.pay_export USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());

GRANT SELECT ON nemura.pay_export TO nemura_api, nemura_worker, nemura_readonly;
GRANT INSERT (id, run_id, version_id, kind, sha256, line_count, skipped_count, total, created_by, created_at) ON nemura.pay_export TO nemura_api;
