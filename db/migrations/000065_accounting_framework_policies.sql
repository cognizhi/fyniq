-- The accounting framework and policies (PRD-001 FR-ORG-10, open question 31; the founders' decision of rev 0.62;
-- p1-general-ledger-close task 6.2).
--
-- The framework (FRS, SFRS(I) or SFRS for Small Entities) and its edition, and each accounting policy's words, are
-- recorded as versions, each effective from the start of a financial year until a later one replaces it. A version
-- never changes: a change is a new version. The accountant confirms a framework version (the company's eligibility for
-- it) and approves a policy version, once, and never the person who wrote that version (the founders' decision of
-- 21 Oct 2026): the database refuses both. Who may confirm and approve (the Accountant role, Appendix D's "Accounting
-- framework and policies" row) is the API's check; the database keeps the writer out and the words fixed.
--
-- Until the framework and every policy in force for a year are confirmed and approved, that year's statements are
-- titled "management statements" (FR-ORG-10, G4-017); the reports read that from here.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.accounting_framework (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  version         integer     NOT NULL,
  from_on         date        NOT NULL,
  framework       text        NOT NULL,
  edition         text        NOT NULL,
  written_by      text        NOT NULL,
  written_at      timestamptz NOT NULL DEFAULT now(),
  confirmed_by    text,
  confirmed_at    timestamptz,
  CONSTRAINT accounting_framework_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT accounting_framework_version_key UNIQUE (organisation_id, version),
  CONSTRAINT accounting_framework_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT accounting_framework_version_positive CHECK (version > 0),
  CONSTRAINT accounting_framework_known CHECK (framework IN ('frs', 'sfrs_i', 'sfrs_se')),
  CONSTRAINT accounting_framework_edition_length CHECK (length(btrim(edition)) BETWEEN 1 AND 120),
  CONSTRAINT accounting_framework_written_by_format CHECK (written_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$'),
  CONSTRAINT accounting_framework_confirmed_together CHECK ((confirmed_by IS NULL) = (confirmed_at IS NULL)),
  CONSTRAINT accounting_framework_confirmed_by_format CHECK (confirmed_by IS NULL OR confirmed_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);

CREATE TABLE fyniq.accounting_policy (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  area            text        NOT NULL,
  version         integer     NOT NULL,
  from_on         date        NOT NULL,
  wording         text        NOT NULL,
  written_by      text        NOT NULL,
  written_at      timestamptz NOT NULL DEFAULT now(),
  approved_by     text,
  approved_at     timestamptz,
  CONSTRAINT accounting_policy_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT accounting_policy_version_key UNIQUE (organisation_id, area, version),
  CONSTRAINT accounting_policy_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT accounting_policy_version_positive CHECK (version > 0),
  CONSTRAINT accounting_policy_area_known CHECK (area IN ('revenue', 'government_grants', 'leases', 'development_costs',
    'impairment', 'safes_and_notes', 'conversion_measurement', 'capitalisation_threshold', 'untaken_leave')),
  CONSTRAINT accounting_policy_wording_length CHECK (length(btrim(wording)) BETWEEN 1 AND 4000),
  CONSTRAINT accounting_policy_written_by_format CHECK (written_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$'),
  CONSTRAINT accounting_policy_approved_together CHECK ((approved_by IS NULL) = (approved_at IS NULL)),
  CONSTRAINT accounting_policy_approved_by_format CHECK (approved_by IS NULL OR approved_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);

ALTER TABLE fyniq.accounting_framework ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.accounting_framework FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.accounting_framework
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.accounting_policy ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.accounting_policy FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.accounting_policy
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.accounting_framework
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('accounting_framework', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.accounting_policy
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('accounting_policy', 'id');

-- A version's content never changes and a version never goes (FY160); its confirmation or approval is set once, and
-- never by whoever wrote it (FY161).
-- +goose StatementBegin
CREATE FUNCTION fyniq.accounting_version_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  old_signed text;
  new_signed text;
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'Version % of the accounting % never goes', OLD.version, TG_ARGV[0]
      USING ERRCODE = 'FY160', HINT = 'Record a new version instead (FR-ORG-10).';
  END IF;
  IF TG_TABLE_NAME = 'accounting_framework' THEN
    old_signed := OLD.confirmed_by;
    new_signed := NEW.confirmed_by;
    IF (NEW.organisation_id, NEW.id, NEW.version, NEW.from_on, NEW.framework, NEW.edition, NEW.written_by, NEW.written_at)
       IS DISTINCT FROM (OLD.organisation_id, OLD.id, OLD.version, OLD.from_on, OLD.framework, OLD.edition, OLD.written_by, OLD.written_at) THEN
      RAISE EXCEPTION 'Version % of the accounting framework never changes', OLD.version
        USING ERRCODE = 'FY160', HINT = 'Record a new version instead (FR-ORG-10).';
    END IF;
  ELSE
    old_signed := OLD.approved_by;
    new_signed := NEW.approved_by;
    IF (NEW.organisation_id, NEW.id, NEW.area, NEW.version, NEW.from_on, NEW.wording, NEW.written_by, NEW.written_at)
       IS DISTINCT FROM (OLD.organisation_id, OLD.id, OLD.area, OLD.version, OLD.from_on, OLD.wording, OLD.written_by, OLD.written_at) THEN
      RAISE EXCEPTION 'Version % of the % policy never changes', OLD.version, OLD.area
        USING ERRCODE = 'FY160', HINT = 'Record a new version instead (FR-ORG-10).';
    END IF;
  END IF;
  IF old_signed IS NOT NULL OR new_signed IS NULL THEN
    RAISE EXCEPTION 'Version % of the accounting % is %', OLD.version, TG_ARGV[0],
      CASE WHEN old_signed IS NOT NULL THEN 'already ' || TG_ARGV[1] ELSE 'not ' || TG_ARGV[1] || ' by anyone' END
      USING ERRCODE = 'FY160', HINT = 'A confirmation or approval is recorded once and never withdrawn; record a new version instead (FR-ORG-10).';
  END IF;
  IF new_signed = OLD.written_by THEN
    RAISE EXCEPTION 'The writer of version % of the accounting % cannot have it %', OLD.version, TG_ARGV[0], TG_ARGV[1]
      USING ERRCODE = 'FY161', HINT = 'Another member who holds the Accountant role does it (FR-ORG-10; the founders'' decision of rev 0.62).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER accounting_framework_guard BEFORE UPDATE OR DELETE ON fyniq.accounting_framework
  FOR EACH ROW EXECUTE FUNCTION fyniq.accounting_version_guard('framework', 'confirmed');
CREATE TRIGGER accounting_policy_guard BEFORE UPDATE OR DELETE ON fyniq.accounting_policy
  FOR EACH ROW EXECUTE FUNCTION fyniq.accounting_version_guard('policy', 'approved');

GRANT SELECT ON fyniq.accounting_framework, fyniq.accounting_policy TO fyniq_api, fyniq_readonly;
GRANT INSERT (version, from_on, framework, edition, written_by), UPDATE (confirmed_by, confirmed_at)
  ON fyniq.accounting_framework TO fyniq_api;
GRANT INSERT (area, version, from_on, wording, written_by), UPDATE (approved_by, approved_at)
  ON fyniq.accounting_policy TO fyniq_api;

