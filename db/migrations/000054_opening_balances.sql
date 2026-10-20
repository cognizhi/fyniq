-- Opening balances (PRD-001 FR-GL-06, posting rule "Opening balances", INV-13; rev 0.49; p1-general-ledger-close task 3.1).
--
-- An organisation's opening balances are one document of type opening_balance, posted as entered at the books start
-- and netting to zero. opening_balance names it: its key is the organisation, so a second one is refused whoever
-- writes it (23505), and it never changes or goes (FY054). Its document versions, statuses and actors follow the
-- rules of every document (000005, 000052).
--
-- INV-13 becomes an allow list: a line to a share-capital account posts only from the register of share issues
-- (share_issue, wave 4) or the opening balances. 000014 refused only manual journals; any other source is refused
-- now too, so a new posting source can never reach share capital by accident (FY007).
--
-- The widened type check is added NOT VALID and validated by 000055 (squawk: constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.document DROP CONSTRAINT document_type_known;
ALTER TABLE fyniq.document ADD CONSTRAINT document_type_known CHECK (type IN ('journal', 'opening_balance')) NOT VALID;

CREATE TABLE fyniq.opening_balance (
  organisation_id uuid NOT NULL DEFAULT fyniq.current_org(),
  document_id     uuid NOT NULL,
  CONSTRAINT opening_balance_pkey PRIMARY KEY (organisation_id),
  CONSTRAINT opening_balance_document_fkey FOREIGN KEY (organisation_id, document_id) REFERENCES fyniq.document (organisation_id, id)
);

-- The row names a document of type opening_balance, and never changes or goes.
-- +goose StatementBegin
CREATE FUNCTION fyniq.opening_balance_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'The opening balances document never changes or goes' USING ERRCODE = 'FY054',
      HINT = 'Save a new version of it instead.';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM fyniq.document d
                  WHERE d.organisation_id = NEW.organisation_id AND d.id = NEW.document_id AND d.type = 'opening_balance') THEN
    RAISE EXCEPTION 'Document % is not an opening balances document', NEW.document_id USING ERRCODE = 'FY054';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER opening_balance_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.opening_balance
  FOR EACH ROW EXECUTE FUNCTION fyniq.opening_balance_guard();
CREATE TRIGGER opening_balance_no_truncate BEFORE TRUNCATE ON fyniq.opening_balance
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();

ALTER TABLE fyniq.opening_balance ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.opening_balance FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.opening_balance
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

-- The document's own audit event covers the row (AUD-08).
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.opening_balance
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('document', 'document_id');

GRANT SELECT ON fyniq.opening_balance TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
GRANT INSERT (document_id) ON fyniq.opening_balance TO fyniq_api;

-- journal_line_check as in 000014, with INV-13 as an allow list.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.journal_line_check() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  org uuid := fyniq.require_org();
  e   record;
  a   record;
  o   record;
BEGIN
  SELECT posting_date, created_xact, source_type INTO e FROM fyniq.journal_entry
   WHERE organisation_id = org AND id = NEW.entry_id;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'Journal entry % does not exist in this organisation', NEW.entry_id
      USING ERRCODE = 'FY021';
  END IF;
  IF e.created_xact <> pg_current_xact_id()::text::bigint THEN
    RAISE EXCEPTION 'Journal entry % is posted, so no line can be added to it', NEW.entry_id
      USING ERRCODE = 'FY002',
            HINT = 'Correct posted history with a reversing entry (INV-02, FR-GL-12).';
  END IF;

  SELECT code, is_group, is_active, role INTO a FROM fyniq.account
   WHERE organisation_id = org AND id = NEW.account_id
   FOR SHARE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'Account % does not exist in this organisation', NEW.account_id
      USING ERRCODE = 'FY004';
  END IF;
  IF a.is_group THEN
    RAISE EXCEPTION 'Account % is a group account, so it cannot take postings', a.code
      USING ERRCODE = 'FY004', HINT = 'Post to an account under it (INV-04, FR-COA-05).';
  END IF;
  IF NOT a.is_active THEN
    RAISE EXCEPTION 'Account % is inactive, so it cannot take postings', a.code
      USING ERRCODE = 'FY004', HINT = 'Reactivate it or post to another account (INV-04).';
  END IF;
  -- (000054) INV-13: share capital changes only through the register of share issues or the opening balances, their
  -- reversals included; every other source is refused.
  IF a.role = 'share-capital' AND e.source_type NOT IN ('share_issue', 'opening_balance') THEN
    RAISE EXCEPTION 'Account % is share capital, so a % cannot post to it', a.code, replace(e.source_type, '_', ' ')
      USING ERRCODE = 'FY007',
            HINT = 'Record the share issue in the register of share issues, or the balance in the opening balances (INV-13).';
  END IF;

  SELECT base_currency, base_minor_units INTO o FROM fyniq.organisation WHERE organisation_id = org;
  NEW.organisation_id := org;
  NEW.posting_date := e.posting_date;
  NEW.minor_units := (SELECT c.minor_units FROM fyniq.currency c WHERE c.code = NEW.currency);
  IF NEW.minor_units IS NULL THEN
    RAISE EXCEPTION 'Currency % is not an ISO 4217 code Fyniq knows', NEW.currency
      USING ERRCODE = 'FY005';
  END IF;
  NEW.base_currency := o.base_currency;
  NEW.base_minor_units := o.base_minor_units;
  NEW.recorded_at := now();
  RETURN NEW;
END
$$;
-- +goose StatementEnd
