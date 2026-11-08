-- The ledger's purge after retention (PRD-001 AUD-10, rev 0.230 and rev 0.238; p1-audit-explorer slice 9, design D7b).
--
-- With retention.purge_after_retention switched on, the purge job carries a financial year's lines forward across the
-- boundary, the first day of the next year (rule purge-carry-forward@1, internal/domain/retention), and deletes them. In
-- one transaction:
--   1. it records the ledger boundary: the guard below checks the switch, that the boundary is the first day of a
--      financial year at least 7 years past, that it moves forward a year at a time, and that every period before it
--      is closed, and stamps the transaction;
--   2. it posts the carry-forward entries (rule purge-carry-forward@1), one per source document, under the document's
--      own source, dated the day before the boundary in the purged year's last, closed period: every report from the
--      boundary on reads them as its opening balance, and each sub-ledger still reconciles with its account by source
--      (INV-07). Only an entry of the transaction that recorded a boundary the next day may (FY305); its lines may name
--      an account, a person or an instrument that changed since, and a document's release or depreciation rules do not
--      apply to it;
--   3. it calls ledger_purge, which checks that the carry-forward replaces the purged lines exactly, keeps a tombstone of
--      each purged entry (its posting identity and source, so a reader asking for it finds its document's carry-forward
--      instead), deletes the purged entries and their lines under its own flag, rebuilds the balances cache and marks
--      the purged periods;
--   4. the job compares the balances cache with the lines (INV-08) and commits.
-- A boundary commits only with its purge done. Outside that transaction nothing changes: INV-02 and INV-03 hold for
-- every role, the owner included, and a purged period never reopens.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.ledger_boundary (
  organisation_id  uuid        NOT NULL DEFAULT fyniq.current_org(),
  id               uuid        NOT NULL DEFAULT uuidv7(),
  boundary         date        NOT NULL,  -- the first day kept whole; every entry before it went, but those kept
  approval         text        NOT NULL,  -- the setting change that switched the purge on
  counts           jsonb       NOT NULL,  -- entries and lines purged and kept, carry-forward entries
  purged_hash      bytea       NOT NULL,  -- SHA-256 of the purged lines in canonical form
  anchor_key       text        NOT NULL,  -- where its signed copy is in the anchors' bucket
  written_at       timestamptz NOT NULL,
  xact             bigint      NOT NULL,  -- set by the guard: the transaction that carried it forward
  carried_revision bigint,                -- set by ledger_purge: the ledger revision after the carry-forward
  CONSTRAINT ledger_boundary_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT ledger_boundary_boundary_key UNIQUE (organisation_id, boundary),
  CONSTRAINT ledger_boundary_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT ledger_boundary_first_day CHECK (EXTRACT(DAY FROM boundary) = 1),
  CONSTRAINT ledger_boundary_hash_sha256 CHECK (length(purged_hash) = 32),
  CONSTRAINT ledger_boundary_counts_object CHECK (jsonb_typeof(counts) = 'object'),
  CONSTRAINT ledger_boundary_text_lengths CHECK (length(approval) BETWEEN 1 AND 200 AND length(anchor_key) BETWEEN 1 AND 300),
  CONSTRAINT ledger_boundary_revision_positive CHECK (carried_revision > 0)
);

-- What went: one row per purged entry, so a reader asking for a posting by its identity, or for a document's postings,
-- learns they were purged and where their balance was carried. Written only by ledger_purge; never changes.
CREATE TABLE fyniq.ledger_purged_entry (
  organisation_id   uuid   NOT NULL DEFAULT fyniq.current_org(),
  id                uuid   NOT NULL,  -- the purged entry's ID
  posting_identity  text   NOT NULL,
  source_type       text   NOT NULL,
  source_id         text   NOT NULL,
  posting_date      date   NOT NULL,
  ledger_revision   bigint NOT NULL,
  reverses_entry_id uuid,
  boundary          date   NOT NULL,  -- the boundary it was carried to
  CONSTRAINT ledger_purged_entry_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT ledger_purged_entry_identity_key UNIQUE (organisation_id, posting_identity),
  CONSTRAINT ledger_purged_entry_boundary_fkey FOREIGN KEY (organisation_id, boundary)
    REFERENCES fyniq.ledger_boundary (organisation_id, boundary)
);
CREATE INDEX ledger_purged_entry_source_idx ON fyniq.ledger_purged_entry (organisation_id, source_id);

ALTER TABLE fyniq.period ADD COLUMN purged_on date;

-- A boundary is recorded only while the purge may run, a year at a time; it then changes once, when its purge is done,
-- in the same transaction, and never again.
-- +goose StatementBegin
CREATE FUNCTION fyniq.ledger_boundary_guard() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  previous date;
  earliest date;
  o        record;
BEGIN
  IF TG_OP = 'UPDATE' THEN
    IF OLD.carried_revision IS NULL AND NEW.carried_revision IS NOT NULL AND OLD.xact = pg_current_xact_id()::text::bigint
       AND (to_jsonb(NEW) - 'carried_revision') = (to_jsonb(OLD) - 'carried_revision') THEN
      RETURN NEW;
    END IF;
    RAISE EXCEPTION 'A ledger boundary never changes' USING ERRCODE = 'FY305';
  END IF;
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A ledger boundary never changes' USING ERRCODE = 'FY305';
  END IF;
  NEW.xact := pg_current_xact_id()::text::bigint;
  NEW.carried_revision := NULL;
  IF NOT fyniq.purge_switched_on(NEW.organisation_id) THEN
    RAISE EXCEPTION 'The purge after retention is switched off' USING ERRCODE = 'FY305',
      HINT = 'It is switched on under dual control (retention.purge_after_retention).';
  END IF;
  SELECT x.fy_start_month, x.books_start, x.first_year_end INTO o FROM fyniq.organisation x WHERE x.organisation_id = NEW.organisation_id;
  IF o.books_start IS NULL OR o.first_year_end IS NULL THEN
    RAISE EXCEPTION 'The organisation''s financial years are not set, so no year can be purged' USING ERRCODE = 'FY305',
      HINT = 'A year''s revenue and expense fold into retained earnings only at the end of a financial year (FR-GL-10).';
  END IF;
  IF NEW.boundary < o.first_year_end + 1 OR EXTRACT(MONTH FROM NEW.boundary) <> o.fy_start_month THEN
    RAISE EXCEPTION 'The ledger boundary % is not the first day of a financial year', NEW.boundary USING ERRCODE = 'FY305';
  END IF;
  IF NEW.boundary + interval '7 years' > current_date THEN
    RAISE EXCEPTION 'The year before % is less than 7 years past', NEW.boundary USING ERRCODE = 'FY305',
      HINT = 'Records are kept at least 7 years after their financial year (§11.9).';
  END IF;
  SELECT max(b.boundary) INTO previous FROM fyniq.ledger_boundary b WHERE b.organisation_id = NEW.organisation_id;
  IF previous >= NEW.boundary THEN
    RAISE EXCEPTION 'A ledger boundary moves forward only' USING ERRCODE = 'FY305';
  END IF;
  SELECT min(e.posting_date) INTO earliest FROM fyniq.journal_entry e
   WHERE e.organisation_id = NEW.organisation_id AND e.posting_date >= coalesce(previous, '-infinity'::date);
  -- The year before the boundary is the first (which may be longer or shorter, and holds what comes before the books
  -- start) or twelve months.
  IF earliest IS NULL OR earliest >= NEW.boundary
     OR (NEW.boundary > o.first_year_end + 1 AND earliest < NEW.boundary - interval '1 year') THEN
    RAISE EXCEPTION 'The ledger boundary % does not end the oldest year still on the books', NEW.boundary USING ERRCODE = 'FY305',
      HINT = 'Years are purged in order, the oldest first (AUD-10).';
  END IF;
  IF EXISTS (SELECT 1 FROM fyniq.period p WHERE p.organisation_id = NEW.organisation_id AND p.starts_on < NEW.boundary
              AND p.status <> 'closed') THEN
    RAISE EXCEPTION 'A period before % is not closed', NEW.boundary USING ERRCODE = 'FY305',
      HINT = 'Close every period of the year first.';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER ledger_boundary_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.ledger_boundary
  FOR EACH ROW EXECUTE FUNCTION fyniq.ledger_boundary_guard();
CREATE TRIGGER ledger_boundary_no_truncate BEFORE TRUNCATE ON fyniq.ledger_boundary
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.journal_immutable();

-- A boundary commits only with its purge done.
-- +goose StatementBegin
CREATE FUNCTION fyniq.ledger_boundary_carried() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM fyniq.ledger_boundary b WHERE b.organisation_id = NEW.organisation_id AND b.id = NEW.id
                  AND b.carried_revision IS NOT NULL) THEN
    RAISE EXCEPTION 'The ledger boundary % was recorded without its purge', NEW.boundary USING ERRCODE = 'FY305',
      HINT = 'Record a boundary only in the purge''s transaction, then call fyniq.ledger_purge (AUD-10).';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER ledger_boundary_carried AFTER INSERT ON fyniq.ledger_boundary
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.ledger_boundary_carried();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.ledger_boundary
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('ledger_boundary', 'id');

-- Whether this transaction recorded a boundary the day after a date and has not yet purged behind it: the moment, and
-- the date, of the carry-forward.
-- +goose StatementBegin
CREATE FUNCTION fyniq.ledger_carrying(p_org uuid, p_date date) RETURNS boolean
LANGUAGE sql STABLE
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT EXISTS (SELECT 1 FROM fyniq.ledger_boundary b
                  WHERE b.organisation_id = p_org AND b.boundary = p_date + 1
                    AND b.xact = pg_current_xact_id()::text::bigint AND b.carried_revision IS NULL)
$$;
-- +goose StatementEnd

-- (000004, 000292) INV-03: the revision and the period of a new entry; only the purge's carry-forward posts into a closed
-- period, dated the day before the boundary it records, and nothing else claims its rule or its source.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.journal_entry_assign() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  org uuid := fyniq.require_org();
  p   record;
BEGIN
  UPDATE fyniq.ledger_revision SET revision = revision + 1
   WHERE organisation_id = org
   RETURNING revision INTO NEW.ledger_revision;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'The organisation of this transaction does not exist' USING ERRCODE = 'FY006';
  END IF;

  SELECT id, status, starts_on, ends_on INTO p FROM fyniq.period
   WHERE organisation_id = org AND daterange(starts_on, ends_on, '[]') @> NEW.posting_date
   FOR SHARE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'No period covers the posting date %', NEW.posting_date
      USING ERRCODE = 'FY030', HINT = 'Create the period first, or post on a date inside one.';
  END IF;
  -- (000292) AUD-10: a carry-forward is posted only by the purge, dated the boundary it records in this transaction, and
  -- only it posts into that closed period.
  IF (NEW.source_type = 'ledger_boundary' OR NEW.rule_version = 'purge-carry-forward@1')
     AND NOT (NEW.rule_version = 'purge-carry-forward@1' AND NEW.reverses_entry_id IS NULL AND fyniq.ledger_carrying(org, NEW.posting_date)) THEN
    RAISE EXCEPTION 'Only the purge after retention posts a carry-forward, dated the day before the boundary it records' USING ERRCODE = 'FY305',
      HINT = 'Carry-forward entries come from the purge job (AUD-10).';
  END IF;
  IF p.status = 'closed' AND NEW.rule_version IS DISTINCT FROM 'purge-carry-forward@1' THEN
    RAISE EXCEPTION 'The period % to % is closed, so nothing can be posted on %',
                    p.starts_on, p.ends_on, NEW.posting_date
      USING ERRCODE = 'FY003',
            HINT = 'Post in an open period, or ask an Owner to reopen this one (INV-03).';
  END IF;

  NEW.organisation_id := org;
  NEW.period_id := p.id;
  NEW.recorded_at := now();
  NEW.created_xact := pg_current_xact_id()::text::bigint;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

-- (000257, 000292) A journal line keeps every rule it had; a carry-forward line may name an account, a person or an
-- instrument that changed since the years it carries.
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
  carry boolean;
BEGIN
  SELECT posting_date, created_xact, source_type, rule_version INTO e FROM fyniq.journal_entry
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
  -- (000292) AUD-10: a carry-forward line, which only the purge posts (journal_entry_assign), carries a balance of years
  -- past: its account may be inactive, its person no longer a director and its instrument converted since.
  carry := e.rule_version = 'purge-carry-forward@1';

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
  IF NOT a.is_active AND NOT carry THEN
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
      WHERE c.organisation_id = org AND c.id = NEW.party_id AND (c.is_director OR c.is_shareholder OR carry)
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
      WHERE c.organisation_id = org AND c.id = NEW.party_id AND (c.is_shareholder OR c.is_investor OR carry)
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
      WHERE d.organisation_id = org AND d.id = NEW.instrument_id AND d.type = 'instrument' AND (d.status = 'posted' OR carry)
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

-- (000122, 000292) A base-only line comes from FX revaluation, a settlement's residual, or the purge's carry-forward.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.journal_line_base_only() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  source text;
  stray  integer;
  role   text;
  e      record;
BEGIN
  SELECT source_type, posting_date, created_xact, rule_version INTO e FROM fyniq.journal_entry
   WHERE organisation_id = NEW.organisation_id AND id = NEW.entry_id;
  source := e.source_type;
  -- (000292) AUD-10: the purge's carry-forward holds a base residual the FX rule cannot express as a base-only line, in
  -- the transaction that recorded its boundary.
  IF e.rule_version = 'purge-carry-forward@1' AND EXISTS (SELECT 1 FROM fyniq.ledger_boundary b WHERE b.organisation_id = NEW.organisation_id
                                              AND b.boundary = e.posting_date + 1 AND b.xact = e.created_xact) THEN
    RETURN NULL;
  END IF;
  IF source IN ('bill_payment', 'credit_application', 'supplier_refund') THEN
    SELECT a.role INTO role FROM fyniq.account a WHERE a.organisation_id = NEW.organisation_id AND a.id = NEW.account_id;
    IF role IS DISTINCT FROM 'accounts-payable' OR abs(NEW.base_amount) > 1 THEN
      RAISE EXCEPTION 'A % changes a base value alone only to clear the residual RND-FX left of a document on accounts payable, at most 1.00 %', source, NEW.base_currency
        USING ERRCODE = 'FY008', HINT = 'Post the line with its amount; a larger difference is a write-off (INV-05, FR-BNK-08).';
    END IF;
    RETURN NULL;
  END IF;
  IF source IN ('customer_receipt', 'write_off', 'customer_credit_application', 'customer_refund') THEN
    SELECT a.role INTO role FROM fyniq.account a WHERE a.organisation_id = NEW.organisation_id AND a.id = NEW.account_id;
    IF role IS DISTINCT FROM 'accounts-receivable' OR abs(NEW.base_amount) > 1 THEN
      RAISE EXCEPTION 'A % changes a base value alone only to clear the residual RND-FX left of an invoice on accounts receivable, at most 1.00 %', source, NEW.base_currency
        USING ERRCODE = 'FY008', HINT = 'Post the line with its amount; a larger difference is a write-off (INV-05, FR-AR-17).';
    END IF;
    RETURN NULL;
  END IF;
  IF source IS DISTINCT FROM 'fx_revaluation' THEN
    RAISE EXCEPTION 'A line with a base amount and no % amount comes only from FX revaluation, not from %', NEW.currency, source
      USING ERRCODE = 'FY008', HINT = 'Post the line with its amount; only the month-end revaluation changes a base value alone (INV-05, FR-GL-04).';
  END IF;
  SELECT count(*) INTO stray
    FROM fyniq.journal_line l
    JOIN fyniq.account a ON a.organisation_id = l.organisation_id AND a.id = l.account_id
   WHERE l.organisation_id = NEW.organisation_id AND l.entry_id = NEW.entry_id
     AND NOT ((l.amount = 0 AND l.base_amount <> 0 AND l.currency <> l.base_currency AND a.revalue)
           OR (l.currency = l.base_currency AND a.role IS NOT DISTINCT FROM 'unrealised-fx'));
  IF stray > 0 THEN
    RAISE EXCEPTION 'A revaluation revalues only accounts marked for it, against the unrealised-FX account: % line(s) of entry % are neither', stray, NEW.entry_id
      USING ERRCODE = 'FY008', HINT = 'Mark the account for revaluation in the chart, or leave its balance out (INV-05, FR-GL-04).';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd

-- (000089, 000111, 000146, 000172, 000292) A release or a month's depreciation names its document's current version; the
-- purge's carry-forward of that document's balance is neither (journal_entry_assign holds it to the purge).
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.journal_entry_bill_release() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF NEW.rule_version = 'purge-carry-forward@1' THEN -- (000292) the purge's carry-forward of the document's balance
    RETURN NEW;
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM fyniq.document d
    WHERE d.organisation_id = NEW.organisation_id AND d.id::text = NEW.source_id AND d.type = 'bill'
      AND d.status = 'posted' AND d.current_version = NEW.source_version
  ) THEN
    RAISE EXCEPTION 'Release % names no posted bill at its current version', NEW.posting_identity
      USING ERRCODE = 'FY172', HINT = 'A release releases the spread of a posted bill''s current version (FR-GL-03).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.journal_entry_invoice_release() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF NEW.rule_version = 'purge-carry-forward@1' THEN -- (000292) the purge's carry-forward of the document's balance
    RETURN NEW;
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM fyniq.document d
    WHERE d.organisation_id = NEW.organisation_id AND d.id::text = NEW.source_id AND d.type = 'invoice'
      AND d.status = 'posted' AND d.current_version = NEW.source_version
  ) THEN
    RAISE EXCEPTION 'Release % names no issued invoice at its current version', NEW.posting_identity
      USING ERRCODE = 'FY172', HINT = 'A release releases the spread of an issued invoice''s current version (FR-GL-03).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.journal_entry_bank_release() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF NEW.rule_version = 'purge-carry-forward@1' THEN -- (000292) the purge's carry-forward of the document's balance
    RETURN NEW;
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM fyniq.document d
    WHERE d.organisation_id = NEW.organisation_id AND d.id::text = NEW.source_id AND d.type = 'bank_transaction'
      AND d.status = 'posted' AND d.current_version = NEW.source_version
  ) THEN
    RAISE EXCEPTION 'Release % names no posted bank transaction at its current version', NEW.posting_identity
      USING ERRCODE = 'FY227', HINT = 'A release releases the spread of a categorised bank line that is still categorised (FR-GL-03, Appendix B.1).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.journal_entry_asset_depreciation() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF NEW.rule_version = 'purge-carry-forward@1' THEN -- (000292) the purge's carry-forward of the document's balance
    RETURN NEW;
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM fyniq.document d
    WHERE d.organisation_id = NEW.organisation_id AND d.id::text = NEW.source_id AND d.type = 'fixed_asset'
      AND d.status = 'posted' AND d.current_version = NEW.source_version
  ) THEN
    RAISE EXCEPTION 'Depreciation % names no registered asset at its current version', NEW.posting_identity
      USING ERRCODE = 'FY236', HINT = 'A month''s depreciation is of a registered asset''s current version (FR-FA-02).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

-- INV-02 holds for every role, the owner included: only ledger_purge, under its own flag and behind a boundary its
-- transaction recorded, deletes an entry or line dated before that boundary and at least 7 years old.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.journal_immutable() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' AND TG_TABLE_NAME IN ('journal_entry', 'journal_line')
     AND current_setting('fyniq.ledger_purging', true) = 'on'
     AND OLD.posting_date < current_date - interval '7 years'
     AND EXISTS (SELECT 1 FROM fyniq.ledger_boundary b
                  WHERE b.organisation_id = OLD.organisation_id AND b.xact = pg_current_xact_id()::text::bigint
                    AND b.carried_revision IS NULL AND OLD.posting_date < b.boundary)
     AND fyniq.purge_switched_on(OLD.organisation_id) THEN
    RETURN OLD;
  END IF;
  RAISE EXCEPTION 'Posted journal entries and lines cannot be changed or deleted (% on %)', TG_OP, TG_TABLE_NAME
    USING ERRCODE = 'FY002',
          HINT = 'Correct posted history with a reversing entry (INV-02, FR-GL-12).';
END
$$;
-- +goose StatementEnd

-- A period's dates never change; a purged period stays closed, and only the purge marks one, once.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.period_guard() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF NEW.starts_on IS DISTINCT FROM OLD.starts_on OR NEW.ends_on IS DISTINCT FROM OLD.ends_on
     OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
    RAISE EXCEPTION 'The dates of the period starting % cannot change', OLD.starts_on
      USING ERRCODE = 'FY031';
  END IF;
  IF OLD.purged_on IS NOT NULL AND (NEW.status IS DISTINCT FROM OLD.status OR NEW.purged_on IS DISTINCT FROM OLD.purged_on) THEN
    RAISE EXCEPTION 'The period starting % was purged on %, so it stays closed', OLD.starts_on, OLD.purged_on
      USING ERRCODE = 'FY305', HINT = 'Its lines were carried forward after retention (AUD-10).';
  END IF;
  IF NEW.purged_on IS DISTINCT FROM OLD.purged_on AND NOT (OLD.status = 'closed' AND NEW.status = 'closed' AND EXISTS (
       SELECT 1 FROM fyniq.ledger_boundary b
        WHERE b.organisation_id = OLD.organisation_id AND b.xact = pg_current_xact_id()::text::bigint
          AND b.carried_revision IS NULL AND OLD.ends_on < b.boundary)) THEN
    RAISE EXCEPTION 'Only the purge after retention marks a period purged' USING ERRCODE = 'FY305';
  END IF;
  IF NEW.status IS DISTINCT FROM OLD.status THEN
    NEW.closed_revision := CASE WHEN NEW.status = 'closed' THEN fyniq.lock_ledger() END;
  ELSE
    NEW.closed_revision := OLD.closed_revision;
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

-- The purge: the transaction's organisation's entries given, all dated before the boundary this transaction recorded,
-- with their lines, once the carry-forward this transaction posted replaces them exactly; then the balances cache
-- rebuilt, the purged periods marked and the boundary done. It refuses an
-- entry a permanent register or the bank's reconciliation names (a share issue, the equity register, the asset
-- register's depreciation and disposals, a bank match or transfer), one with a line on share capital (INV-13), and a
-- reversal split from what it reverses.
-- +goose StatementBegin
CREATE FUNCTION fyniq.ledger_purge(p_boundary date, p_entries uuid[]) RETURNS bigint
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  org uuid := fyniq.require_org();
  b   uuid;
  n   bigint;
  d   record;
BEGIN
  SELECT x.id INTO b FROM fyniq.ledger_boundary x
   WHERE x.organisation_id = org AND x.boundary = p_boundary AND x.xact = pg_current_xact_id()::text::bigint
     AND x.carried_revision IS NULL;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'No ledger boundary at % was recorded in this transaction', p_boundary USING ERRCODE = 'FY305',
      HINT = 'Record the boundary first, in the purge''s own transaction (AUD-10).';
  END IF;
  PERFORM fyniq.lock_ledger();
  IF (SELECT count(*) FROM fyniq.journal_entry e WHERE e.organisation_id = org AND e.id = ANY (p_entries) AND e.posting_date < p_boundary)
     <> cardinality(ARRAY(SELECT DISTINCT unnest(p_entries))) THEN
    RAISE EXCEPTION 'An entry to purge is not this organisation''s, or not dated before %', p_boundary USING ERRCODE = 'FY305';
  END IF;
  IF EXISTS (SELECT 1 FROM fyniq.share_issue s WHERE s.organisation_id = org AND s.entry_id = ANY (p_entries))
     OR EXISTS (SELECT 1 FROM fyniq.bank_match m WHERE m.organisation_id = org AND m.entry_id = ANY (p_entries))
     OR EXISTS (SELECT 1 FROM fyniq.bank_transfer t WHERE t.organisation_id = org AND t.entry_id = ANY (p_entries))
     OR EXISTS (SELECT 1 FROM fyniq.journal_entry e WHERE e.organisation_id = org AND e.id = ANY (p_entries)
                 AND e.source_type IN ('equity_transaction', 'asset_depreciation', 'asset_disposal'))
     OR EXISTS (SELECT 1 FROM fyniq.journal_line l JOIN fyniq.account a ON a.organisation_id = l.organisation_id AND a.id = l.account_id
                 WHERE l.organisation_id = org AND l.entry_id = ANY (p_entries) AND a.role = 'share-capital') THEN
    RAISE EXCEPTION 'An entry to purge is one a permanent register or the bank''s reconciliation needs' USING ERRCODE = 'FY305',
      HINT = 'Share issues, the equity register, share capital, the asset register''s depreciation and disposals, and reconciled bank entries are kept (AUD-10, INV-13).';
  END IF;
  IF EXISTS (SELECT 1 FROM fyniq.journal_entry e WHERE e.organisation_id = org AND e.reverses_entry_id IS NOT NULL
              AND (e.id = ANY (p_entries)) <> (e.reverses_entry_id = ANY (p_entries))) THEN
    RAISE EXCEPTION 'An entry to purge is reversed by, or reverses, an entry kept' USING ERRCODE = 'FY305',
      HINT = 'A reversal and what it reverses go together or stay together (AUD-10).';
  END IF;
  -- The carry-forward this transaction posted, dated the day before the boundary, replaces the purged lines exactly, so
  -- every balance from the boundary on is unchanged: revenue and expense fold into retained earnings by dimension set in
  -- the base currency, retained earnings counts whole, and every other line counts by account, dimension set, currency,
  -- person, instrument and source document.
  WITH re AS (
    SELECT a.id FROM fyniq.account a WHERE a.organisation_id = org AND a.role = 'retained-earnings' ORDER BY a.code LIMIT 1
  ), lines AS (
    SELECT -1 AS side, l.*, e.source_type, e.source_id FROM fyniq.journal_line l
      JOIN fyniq.journal_entry e ON e.organisation_id = l.organisation_id AND e.id = l.entry_id
     WHERE l.organisation_id = org AND l.entry_id = ANY (p_entries)
    UNION ALL
    SELECT 1, l.*, e.source_type, e.source_id FROM fyniq.journal_line l
      JOIN fyniq.journal_entry e ON e.organisation_id = l.organisation_id AND e.id = l.entry_id
     WHERE l.organisation_id = org AND e.rule_version = 'purge-carry-forward@1' AND e.posting_date = p_boundary - 1
       AND e.created_xact = pg_current_xact_id()::text::bigint
  ), keyed AS (
    SELECT x.side,
           CASE WHEN a.type IN ('revenue', 'expense') THEN (SELECT id FROM re) ELSE x.account_id END AS account_id,
           x.dimension_set_id,
           CASE WHEN a.type IN ('revenue', 'expense') THEN x.base_currency ELSE x.currency END AS currency,
           CASE WHEN a.type IN ('revenue', 'expense') THEN NULL ELSE x.party_id END AS party_id,
           CASE WHEN a.type IN ('revenue', 'expense') THEN NULL ELSE x.instrument_id END AS instrument_id,
           CASE WHEN a.type IN ('revenue', 'expense') OR a.role = 'retained-earnings' THEN ''
                ELSE x.source_type || ':' || x.source_id END AS source,
           CASE WHEN a.type IN ('revenue', 'expense') THEN x.base_amount ELSE x.amount END AS amount,
           x.base_amount
      FROM lines x JOIN fyniq.account a ON a.organisation_id = x.organisation_id AND a.id = x.account_id
  )
  SELECT k.account_id, k.currency, k.source, sum(k.side * k.amount) AS amount, sum(k.side * k.base_amount) AS base_amount INTO d
    FROM keyed k
   GROUP BY k.account_id, k.dimension_set_id, k.currency, k.party_id, k.instrument_id, k.source
  HAVING sum(k.side * k.amount) <> 0 OR sum(k.side * k.base_amount) <> 0
   LIMIT 1;
  IF FOUND THEN
    RAISE EXCEPTION 'The carry-forward does not replace what is purged: account %, % %, differs by % (base %)',
      d.account_id, d.currency, nullif(d.source, ''), d.amount, d.base_amount USING ERRCODE = 'FY305',
      HINT = 'Post the carry-forward of exactly the entries purged, dated the day before the boundary (AUD-10).';
  END IF;
  INSERT INTO fyniq.ledger_purged_entry (organisation_id, id, posting_identity, source_type, source_id, posting_date, ledger_revision,
                                         reverses_entry_id, boundary)
  SELECT e.organisation_id, e.id, e.posting_identity, e.source_type, e.source_id, e.posting_date, e.ledger_revision, e.reverses_entry_id, p_boundary
    FROM fyniq.journal_entry e WHERE e.organisation_id = org AND e.id = ANY (p_entries);
  PERFORM set_config('fyniq.ledger_purging', 'on', true);
  DELETE FROM fyniq.journal_line l WHERE l.organisation_id = org AND l.entry_id = ANY (p_entries);
  DELETE FROM fyniq.journal_entry e WHERE e.organisation_id = org AND e.id = ANY (p_entries);
  GET DIAGNOSTICS n = ROW_COUNT;
  PERFORM set_config('fyniq.ledger_purging', 'off', true);
  PERFORM fyniq.rebuild_balances();
  UPDATE fyniq.period p SET purged_on = current_date
   WHERE p.organisation_id = org AND p.ends_on < p_boundary AND p.purged_on IS NULL;
  UPDATE fyniq.ledger_boundary x SET carried_revision = (SELECT r.revision FROM fyniq.ledger_revision r WHERE r.organisation_id = org)
   WHERE x.organisation_id = org AND x.id = b;
  RETURN n;
END
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION fyniq.ledger_purge(date, uuid[]) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.ledger_purge(date, uuid[]) TO fyniq_worker;

ALTER TABLE fyniq.ledger_boundary ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.ledger_boundary FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.ledger_boundary
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

-- A tombstone never changes or goes.
CREATE TRIGGER ledger_purged_entry_immutable BEFORE UPDATE OR DELETE ON fyniq.ledger_purged_entry
  FOR EACH ROW EXECUTE FUNCTION fyniq.journal_immutable();
CREATE TRIGGER ledger_purged_entry_no_truncate BEFORE TRUNCATE ON fyniq.ledger_purged_entry
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.journal_immutable();
ALTER TABLE fyniq.ledger_purged_entry ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.ledger_purged_entry FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.ledger_purged_entry
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
GRANT SELECT ON fyniq.ledger_purged_entry TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;

GRANT INSERT (boundary, approval, counts, purged_hash, anchor_key, written_at) ON fyniq.ledger_boundary TO fyniq_worker;
GRANT SELECT ON fyniq.ledger_boundary TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
