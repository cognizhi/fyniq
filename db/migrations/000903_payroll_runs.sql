-- Pay runs from the people platform (PRD-001 FR-PAY-07, FR-PPL-04, FR-PPL-09, FR-PPL-11; p1-payroll-link tasks 4.1, 5.2, 5.3). Two things live
-- here, and no per-person pay does (FR-PAY-13):
--
--  * payroll_mapping: which account each department's salary posts to and which account each kind of deduction is recovered through, by
--    version. A run's approval names the version it was calculated against; a run for another version than the current one is refused
--    (FR-PPL-09). The department is a dimension value of Fyniq's own; a line carries it (FR-EMP-02's minimum, design D8).
--  * payroll_run_receipt: what Fyniq received and posted of a run's version: the envelope's SHA-256 (used once), who prepared and approved it
--    (issuer and subject), the mapping version, the entry it posted and the run's cumulative posted totals by line, from which the next
--    version's difference is worked out. The database refuses a payroll entry that has no receipt from the same transaction: no code path
--    posts pay without having verified it.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- The origin Nemura's web app is served from: a passkey assertion made there names it in its client data, and Fyniq checks it against this (one
-- relying-party ID for both apps, two origins: PRD-002 question 10, p1-payroll-link design D3).
ALTER TABLE fyniq.people_link ADD COLUMN nemura_origin text NOT NULL DEFAULT '';
ALTER TABLE fyniq.people_link ADD CONSTRAINT people_link_origin_shape CHECK (nemura_origin ~ '^(https?://[a-z0-9.-]{1,200}(:[0-9]{1,5})?)?$') NOT VALID;
GRANT INSERT (nemura_origin) ON fyniq.people_link TO fyniq_api;

CREATE TABLE fyniq.payroll_mapping (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  version         integer     NOT NULL,
  set_by          text        NOT NULL,
  set_at          timestamptz NOT NULL,
  CONSTRAINT payroll_mapping_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT payroll_mapping_version_key UNIQUE (organisation_id, version),
  CONSTRAINT payroll_mapping_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT payroll_mapping_version_positive CHECK (version >= 1),
  CONSTRAINT payroll_mapping_by_format CHECK (set_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);
CREATE TABLE fyniq.payroll_mapping_entry (
  organisation_id uuid NOT NULL DEFAULT fyniq.current_org(),
  id              uuid NOT NULL DEFAULT uuidv7(),
  mapping_id      uuid NOT NULL,
  key             text NOT NULL,   -- department:<dimension value id> or deduction:<kind>
  account_id      uuid NOT NULL,
  CONSTRAINT payroll_mapping_entry_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT payroll_mapping_entry_key UNIQUE (organisation_id, mapping_id, key),
  CONSTRAINT payroll_mapping_entry_mapping_fkey FOREIGN KEY (organisation_id, mapping_id) REFERENCES fyniq.payroll_mapping (organisation_id, id),
  CONSTRAINT payroll_mapping_entry_account_fkey FOREIGN KEY (organisation_id, account_id) REFERENCES fyniq.account (organisation_id, id),
  CONSTRAINT payroll_mapping_entry_key_shape CHECK (key ~ '^(department:[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}|deduction:[a-z][a-z_]{0,39})$')
);
CREATE TRIGGER payroll_mapping_guard BEFORE UPDATE OR DELETE ON fyniq.payroll_mapping
  FOR EACH ROW EXECUTE FUNCTION fyniq.notification_append_only();
CREATE TRIGGER payroll_mapping_entry_guard BEFORE UPDATE OR DELETE ON fyniq.payroll_mapping_entry
  FOR EACH ROW EXECUTE FUNCTION fyniq.notification_append_only();
CREATE TRIGGER payroll_mapping_no_truncate BEFORE TRUNCATE ON fyniq.payroll_mapping
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();
CREATE TRIGGER payroll_mapping_entry_no_truncate BEFORE TRUNCATE ON fyniq.payroll_mapping_entry
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.payroll_mapping
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('payroll_mapping', 'id');

CREATE TABLE fyniq.payroll_run_receipt (
  organisation_id  uuid        NOT NULL DEFAULT fyniq.current_org(),
  id               uuid        NOT NULL DEFAULT uuidv7(),
  link_id          uuid        NOT NULL,
  epoch            integer     NOT NULL,
  source_system    text        NOT NULL,
  run_id           uuid        NOT NULL,
  version          integer     NOT NULL,
  wage_month       date        NOT NULL,
  envelope_sha256  text        NOT NULL,
  approver_issuer  text        NOT NULL,
  approver_subject text        NOT NULL,
  preparer_issuer  text        NOT NULL,
  preparer_subject text        NOT NULL,
  mapping_version  integer     NOT NULL,
  entry_id         uuid,                   -- the entry it posted; none when the version changed nothing
  totals           jsonb       NOT NULL,   -- the run's posted totals by line after this version: account|values -> signed base amount
  received_at      timestamptz NOT NULL,
  created_xact     bigint      NOT NULL DEFAULT pg_current_xact_id()::text::bigint,
  CONSTRAINT payroll_run_receipt_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT payroll_run_receipt_envelope_key UNIQUE (organisation_id, envelope_sha256),
  CONSTRAINT payroll_run_receipt_run_key UNIQUE (organisation_id, source_system, run_id, version),
  CONSTRAINT payroll_run_receipt_link_fkey FOREIGN KEY (organisation_id, link_id) REFERENCES fyniq.people_link (organisation_id, id),
  CONSTRAINT payroll_run_receipt_entry_fkey FOREIGN KEY (organisation_id, entry_id) REFERENCES fyniq.journal_entry (organisation_id, id),
  CONSTRAINT payroll_run_receipt_digest CHECK (envelope_sha256 ~ '^[0-9a-f]{64}$'),
  CONSTRAINT payroll_run_receipt_version_positive CHECK (version >= 1 AND mapping_version >= 1 AND epoch >= 1),
  CONSTRAINT payroll_run_receipt_distinct_people CHECK ((approver_issuer, approver_subject) <> (preparer_issuer, preparer_subject)),
  CONSTRAINT payroll_run_receipt_wage_month CHECK (wage_month = date_trunc('month', wage_month)::date)
);
CREATE TRIGGER payroll_run_receipt_guard BEFORE UPDATE OR DELETE ON fyniq.payroll_run_receipt
  FOR EACH ROW EXECUTE FUNCTION fyniq.notification_append_only();
CREATE TRIGGER payroll_run_receipt_no_truncate BEFORE TRUNCATE ON fyniq.payroll_run_receipt
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.payroll_run_receipt
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('payroll_run_receipt', 'id');

-- No payroll entry without its receipt: an entry whose source is a pay run must have a receipt of the same run and version, made in the same
-- transaction and naming the entry (FR-PPL-09). A receipt is made after the entry, so the check is deferred to commit.
-- +goose StatementBegin
CREATE FUNCTION fyniq.payroll_entry_needs_receipt() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM fyniq.payroll_run_receipt r
                  WHERE r.organisation_id = NEW.organisation_id AND r.entry_id = NEW.id
                    AND r.run_id::text = NEW.source_id AND r.version = NEW.source_version
                    AND r.created_xact = pg_current_xact_id()::text::bigint) THEN
    RAISE EXCEPTION 'A pay run posts only with the receipt of its verification, made in the same transaction' USING ERRCODE = 'FY901',
      HINT = 'Post a pay run through the link''s payroll operation, which verifies its authority first (FR-PPL-09).';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER payroll_entry_needs_receipt AFTER INSERT ON fyniq.journal_entry
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (NEW.source_type = 'payroll_run')
  EXECUTE FUNCTION fyniq.payroll_entry_needs_receipt();

-- ------------------------------------------------------------------ row-level security and grants
ALTER TABLE fyniq.payroll_mapping ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.payroll_mapping FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.payroll_mapping
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.payroll_mapping_entry ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.payroll_mapping_entry FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.payroll_mapping_entry
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.payroll_run_receipt ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.payroll_run_receipt FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.payroll_run_receipt
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

REVOKE ALL ON fyniq.payroll_mapping, fyniq.payroll_mapping_entry, fyniq.payroll_run_receipt FROM PUBLIC;
GRANT SELECT ON fyniq.payroll_mapping, fyniq.payroll_mapping_entry, fyniq.payroll_run_receipt TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (version, set_by, set_at) ON fyniq.payroll_mapping TO fyniq_api;
GRANT INSERT (mapping_id, key, account_id) ON fyniq.payroll_mapping_entry TO fyniq_api;
GRANT INSERT (link_id, epoch, source_system, run_id, version, wage_month, envelope_sha256, approver_issuer, approver_subject, preparer_issuer,
              preparer_subject, mapping_version, entry_id, totals, received_at) ON fyniq.payroll_run_receipt TO fyniq_api;
