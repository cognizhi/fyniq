-- Share-capital accounts (PRD-001 INV-13, rev 0.24: from P0; p0-manual-journal-rules 2.5).
--
-- The chart of accounts marks share-capital accounts with a role, not a list of codes: an organisation
-- may number its chart differently (3010 and 3020 in the default chart). The role is set when the
-- account is created and never changes. A manual journal line to a share-capital account is refused by
-- the domain (ENG-24) and, as the backstop, by the posting-source check on every journal line here
-- (golden rule 2, ADR-D1). The register of share issues and opening balances, the sources allowed to
-- post there, arrive in P1. Further roles (FR-COA-03's system accounts) extend the CHECK.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.account ADD COLUMN role text;
ALTER TABLE fyniq.account ADD CONSTRAINT account_role_known CHECK (role IN ('share-capital')) NOT VALID;
-- 000015 validates it, in a transaction of its own (squawk: constraint-missing-not-valid).

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.account_guard() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'UPDATE' AND (NEW.code IS DISTINCT FROM OLD.code
     OR NEW.type IS DISTINCT FROM OLD.type OR NEW.created_at IS DISTINCT FROM OLD.created_at) THEN
    RAISE EXCEPTION 'The code and type of account % cannot change', OLD.code
      USING ERRCODE = 'FY040', HINT = 'Create a new account and deactivate this one (FR-COA-04).';
  END IF;
  -- (000014) The role is fixed when the account is created: a share-capital account stays one (INV-13).
  IF TG_OP = 'UPDATE' AND NEW.role IS DISTINCT FROM OLD.role THEN
    RAISE EXCEPTION 'The role of account % cannot change', OLD.code
      USING ERRCODE = 'FY040', HINT = 'Create a new account with the role and deactivate this one (FR-COA-04, INV-13).';
  END IF;
  IF NEW.parent_id IS NOT NULL THEN
    IF NOT EXISTS (SELECT 1 FROM fyniq.account p
                    WHERE p.organisation_id = NEW.organisation_id AND p.id = NEW.parent_id
                      AND p.is_group) THEN
      RAISE EXCEPTION 'The parent of account % is not a group account', NEW.code
        USING ERRCODE = 'FY041', HINT = 'Choose a group account as the parent (FR-COA-05).';
    END IF;
    IF TG_OP = 'UPDATE' AND EXISTS (
      WITH RECURSIVE up(id) AS (
        SELECT NEW.parent_id
        UNION
        SELECT a.parent_id FROM fyniq.account a JOIN up ON a.id = up.id
         WHERE a.organisation_id = NEW.organisation_id AND a.parent_id IS NOT NULL)
      SELECT 1 FROM up WHERE id = NEW.id) THEN
      RAISE EXCEPTION 'Account % cannot sit under its own descendant', NEW.code USING ERRCODE = 'FY041';
    END IF;
  END IF;
  IF TG_OP = 'UPDATE' AND OLD.is_group AND NOT NEW.is_group AND EXISTS (
       SELECT 1 FROM fyniq.account c WHERE c.organisation_id = NEW.organisation_id AND c.parent_id = NEW.id) THEN
    RAISE EXCEPTION 'Account % has accounts under it, so it must stay a group', NEW.code
      USING ERRCODE = 'FY041';
  END IF;
  IF TG_OP = 'UPDATE' AND NOT OLD.is_group AND NEW.is_group AND EXISTS (
       SELECT 1 FROM fyniq.journal_line l WHERE l.organisation_id = NEW.organisation_id AND l.account_id = NEW.id) THEN
    RAISE EXCEPTION 'Account % has postings, so it cannot become a group', NEW.code
      USING ERRCODE = 'FY041', HINT = 'Postings go only to leaf accounts (FR-COA-05).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

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
  -- (000014) INV-13: share capital changes only through the register of share issues or opening
  -- balances (P1), so no manual journal posts to it, its reversals included.
  IF a.role = 'share-capital' AND e.source_type = 'manual_journal' THEN
    RAISE EXCEPTION 'Account % is share capital, so a manual journal cannot post to it', a.code
      USING ERRCODE = 'FY007',
            HINT = 'Record the share issue in the register of share issues (INV-13).';
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

-- ------------------------------------------------------------------ grants (db/README.md)
-- The API may mark an account when it creates it (audited, as every account change); nothing may
-- change the mark afterwards (account_guard).
GRANT INSERT (role) ON fyniq.account TO fyniq_api;
