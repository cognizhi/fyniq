-- Payslips (PRD-002 NM-PAY-04; MOM's itemised pay slip, sg-people@2026.7 `payslip_items`; p1-nemura-payroll slice 4).
--
-- A payslip is made once for each person of an approved run version: the twelve items MOM requires as data, the PDF drawn from them, and the SHA-256 of the exact bytes. It is evidence: written
-- once, never changed or deleted. A correction is the payslip of the corrected version. The person reads their own; the audit event names the payslip, never an amount.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE nemura.payslip (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  version_id      uuid        NOT NULL,
  person_id       uuid        NOT NULL,
  month           date        NOT NULL,
  pay_date        date        NOT NULL,
  reference       text        NOT NULL,
  sha256          bytea       NOT NULL,   -- of the exact PDF bytes
  blob_key        text        NOT NULL,
  data            jsonb       NOT NULL,   -- the twelve items as the person reads them
  published_by    uuid        NOT NULL,
  published_at    timestamptz NOT NULL,
  CONSTRAINT payslip_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT payslip_version_fkey FOREIGN KEY (organisation_id, version_id) REFERENCES nemura.pay_run_version (organisation_id, id),
  CONSTRAINT payslip_person_fkey FOREIGN KEY (organisation_id, person_id) REFERENCES nemura.person (organisation_id, id),
  CONSTRAINT payslip_by_fkey FOREIGN KEY (organisation_id, published_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT payslip_once UNIQUE (organisation_id, version_id, person_id),
  CONSTRAINT payslip_reference_once UNIQUE (organisation_id, reference),
  CONSTRAINT payslip_month CHECK (extract(day FROM month) = 1),
  CONSTRAINT payslip_sha CHECK (octet_length(sha256) = 32)
);
CREATE INDEX payslip_person_idx ON nemura.payslip (organisation_id, person_id, month);
CREATE TRIGGER payslip_append_only BEFORE UPDATE OR DELETE ON nemura.payslip FOR EACH ROW EXECUTE FUNCTION nemura.append_only_guard('A payslip');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON nemura.payslip DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('payslip', 'id');

ALTER TABLE nemura.payslip ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.payslip FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.payslip USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());

GRANT SELECT ON nemura.payslip TO nemura_api, nemura_worker, nemura_readonly;
GRANT INSERT (id, version_id, person_id, month, pay_date, reference, sha256, blob_key, data, published_by, published_at) ON nemura.payslip TO nemura_api;
