-- 2700's lines keyed by instrument (PRD-001 FR-EQ-18, ENG-04; p1-equity-register slice 4a-ii, task 5.1, design D6).
--
-- 2700 Convertible notes / SAFE takes the role convertible-instruments. Each of its lines names its instrument,
-- journal_line.instrument_id, so each SAFE's or note's balance reads from the ledger as a director's account reads its
-- person's. The database is the backstop (FY292): a line on convertible instruments names a recorded instrument, held
-- for share so that it is not changed meanwhile, and no other line names one; it names no person (FY250). The widened
-- role check and the new foreign key are added NOT VALID and validated by 000258; 000259 builds the index concurrently.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.account DROP CONSTRAINT account_role_known;
ALTER TABLE fyniq.account ADD CONSTRAINT account_role_known CHECK (role IN ('accounts-receivable', 'accounts-payable', 'gst-control',
  'fx-rounding', 'unrealised-fx', 'deferred-revenue', 'prepayments', 'salaries-payable', 'cpf-sdl-payable', 'employer-cpf', 'sdl',
  'depreciation', 'accumulated-depreciation', 'suspense', 'retained-earnings', 'expense-claims-payable', 'income-tax-payable',
  'income-tax-expense', 'director-loan', 'deferred-remuneration', 'share-capital', 'capital-contribution',
  'withholding-tax-payable', 'customer-deposits', 'share-application-monies', 'convertible-instruments')) NOT VALID;

ALTER TABLE fyniq.journal_line ADD COLUMN instrument_id uuid;
ALTER TABLE fyniq.journal_line ADD CONSTRAINT journal_line_instrument_fkey FOREIGN KEY (organisation_id, instrument_id)
  REFERENCES fyniq.document (organisation_id, id) NOT VALID;
GRANT INSERT (instrument_id) ON fyniq.journal_line TO fyniq_api, fyniq_worker, fyniq_executor;

-- +goose StatementBegin
-- (000252, 000257) A journal line keeps every rule 000199 and 000252 gave it; and a line on convertible instruments names
-- its instrument, a recorded SAFE or convertible note held for share, while no other line names one (FR-EQ-18; FY292).
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
  -- (000054, 000252) INV-13: share capital changes only through the register of share issues, the opening balances or a
  -- conversion between classes in the equity register, their reversals included; every other source is refused.
  IF a.role = 'share-capital' AND e.source_type NOT IN ('share_issue', 'opening_balance', 'equity_transaction') THEN
    RAISE EXCEPTION 'Account % is share capital, so a % cannot post to it', a.code, replace(e.source_type, '_', ' ')
      USING ERRCODE = 'FY007',
            HINT = 'Record the share issue in the register of share issues, or the balance in the opening balances (INV-13).';
  END IF;
  -- (000199) B.7, FR-EQ-01: a line on a director's account names its person, a director or shareholder, held for share
  -- so that no one unmarks them meanwhile (FY249); (000252) a line on share application monies names its subscriber, a
  -- shareholder or investor, held the same way; no other line names one.
  IF a.role IN ('director-loan', 'deferred-remuneration') THEN
    IF NEW.party_id IS NULL THEN
      RAISE EXCEPTION 'Account % is a director''s account, so each line on it names whose it is', a.code
        USING ERRCODE = 'FY250', HINT = 'Choose the director or shareholder the amount belongs to (FR-EQ-01, B.7).';
    END IF;
    PERFORM 1 FROM fyniq.contact c
      WHERE c.organisation_id = org AND c.id = NEW.party_id AND (c.is_director OR c.is_shareholder)
      FOR SHARE;
    IF NOT FOUND THEN
      RAISE EXCEPTION 'Contact % is not a director or shareholder of this organisation, so it has no director''s account',
        NEW.party_id
        USING ERRCODE = 'FY250', HINT = 'Mark the contact director or shareholder, or choose another (FR-EQ-01).';
    END IF;
  ELSIF a.role = 'share-application-monies' THEN
    IF NEW.party_id IS NULL THEN
      RAISE EXCEPTION 'Account % holds share application monies, so each line on it names its subscriber', a.code
        USING ERRCODE = 'FY250', HINT = 'Choose the shareholder or investor who paid for shares not yet allotted (B.7).';
    END IF;
    PERFORM 1 FROM fyniq.contact c
      WHERE c.organisation_id = org AND c.id = NEW.party_id AND (c.is_shareholder OR c.is_investor)
      FOR SHARE;
    IF NOT FOUND THEN
      RAISE EXCEPTION 'Contact % is not a shareholder or investor of this organisation, so it holds no share application monies',
        NEW.party_id
        USING ERRCODE = 'FY250', HINT = 'Mark the contact a shareholder or investor, or choose another (B.7).';
    END IF;
  ELSIF NEW.party_id IS NOT NULL THEN
    RAISE EXCEPTION 'Account % is not a director''s account, so its line names no person', a.code
      USING ERRCODE = 'FY250', HINT = 'Leave the person out, or post to the director''s account or share application monies (B.7).';
  END IF;
  -- (000257) FR-EQ-18: 2700 keys its lines by instrument, so each SAFE's or note's balance reads from the ledger (ENG-04).
  IF a.role = 'convertible-instruments' THEN
    IF NEW.instrument_id IS NULL THEN
      RAISE EXCEPTION 'Account % holds SAFEs and convertible notes, so each line on it names its instrument', a.code
        USING ERRCODE = 'FY292', HINT = 'Choose the recorded SAFE or note the money is for (FR-EQ-18).';
    END IF;
    PERFORM 1 FROM fyniq.document d
      WHERE d.organisation_id = org AND d.id = NEW.instrument_id AND d.type = 'instrument' AND d.status = 'posted'
      FOR SHARE;
    IF NOT FOUND THEN
      RAISE EXCEPTION 'Document % is not a recorded SAFE or convertible note of this organisation', NEW.instrument_id
        USING ERRCODE = 'FY292', HINT = 'Record the instrument first: it is recorded on another person''s approval (FR-EQ-18).';
    END IF;
  ELSIF NEW.instrument_id IS NOT NULL THEN
    RAISE EXCEPTION 'Account % does not hold SAFEs or convertible notes, so its line names no instrument', a.code
      USING ERRCODE = 'FY292', HINT = 'Leave the instrument out, or post to the convertible instruments account (FR-EQ-18).';
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
