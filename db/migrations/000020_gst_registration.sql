-- The organisation's GST registration (PRD-001 FR-ORG-03; p1-organisation-chart task 2.1): the first
-- setting family with effective dates (data/data-model "Effective-dated reference data", "Valid time and
-- system time for master data").
--
-- Each row is one registration: registered from a date, deregistered from a later date (NULL while it
-- lasts), with its GST registration number, its return frequency and how its quarters align, and its
-- accounting basis. Valid time is [registered_from, deregistered_from); an exclusion constraint on that
-- range refuses two registrations that overlap, as period_no_overlap does for periods (000003). System
-- time is recorded_at, with the version counting each change, and every version is an audit event
-- (require_audit), so the history of what was known when is the audit trail's.
--
-- The accounting basis is invoice basis only: cash basis under IRAS's Cash Accounting Scheme is not
-- offered until its rules are pinned in the sg-gst pack (FR-ORG-03, ENG-45). A later migration widens the
-- check then.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.gst_registration (
  organisation_id     uuid        NOT NULL DEFAULT fyniq.current_org(),
  id                  uuid        NOT NULL DEFAULT uuidv7(),
  registered_from     date        NOT NULL,
  deregistered_from   date,
  gst_number          text        NOT NULL,
  return_frequency    text        NOT NULL DEFAULT 'quarterly',
  -- Quarters start in this month and every third after it: 1 (Jan, Apr, Jul, Oct), 2 or 3.
  quarter_start_month smallint    NOT NULL DEFAULT 1,
  accounting_basis    text        NOT NULL DEFAULT 'invoice',
  version             integer     NOT NULL DEFAULT 1,
  recorded_at         timestamptz NOT NULL,
  CONSTRAINT gst_registration_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT gst_registration_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT gst_registration_dates CHECK (deregistered_from > registered_from),
  CONSTRAINT gst_registration_number_length CHECK (length(btrim(gst_number)) BETWEEN 1 AND 20),
  CONSTRAINT gst_registration_frequency_known CHECK (return_frequency IN ('quarterly', 'monthly')),
  CONSTRAINT gst_registration_quarter_start CHECK (quarter_start_month BETWEEN 1 AND 3),
  CONSTRAINT gst_registration_basis_known CHECK (accounting_basis IN ('invoice')),
  CONSTRAINT gst_registration_version_positive CHECK (version > 0),
  CONSTRAINT gst_registration_no_overlap EXCLUDE USING gist
    (organisation_id WITH =, daterange(registered_from, deregistered_from, '[)') WITH &&)
);

ALTER TABLE fyniq.gst_registration ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.gst_registration FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.gst_registration
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.gst_registration
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('gst_registration', 'id');

GRANT SELECT ON fyniq.gst_registration TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
GRANT INSERT (registered_from, deregistered_from, gst_number, return_frequency, quarter_start_month, accounting_basis, recorded_at),
      UPDATE (registered_from, deregistered_from, gst_number, return_frequency, quarter_start_month, accounting_basis, version, recorded_at)
  ON fyniq.gst_registration TO fyniq_api;
