-- The register of share issues (PRD-001 FR-EQ-08, INV-13; p1-founders-money task 5.2, design D11).
--
-- Every allotment of shares is a share_issue row: the holder, the class (a share-capital account, 3010 ordinary or 3020
-- preference), the number of shares, the price per share, the amount in the base currency, the consideration (cash,
-- debt capitalised, services, a SAFE or note converted), the resolution references, the ACRA lodgement reference and the
-- effective date, the day ACRA's electronic register of members shows the allotment. An issue before the books start
-- (the incorporation subscription) is recorded without an entry, as the opening balances hold its share capital; any
-- later one is posted from the register in the same transaction, from the source share_issue (Dr the consideration · Cr
-- the class), and a conversion names the settlement it decides. The database is the backstop:
--   - a row is written once and never changed, deleted or truncated (FY255);
--   - its class is an active share-capital account, its holder a director or shareholder, its amount at the currency's
--     scale and its shares whole (FY255 and the checks);
--   - one with no entry is dated before the books start; one with an entry names an entry of the source share_issue for
--     it, posted on its effective date, that credits its class with its amount (FY255);
--   - an entry from the source share_issue commits only with its row, or as the reversal of one that has it (FY255), so
--     share capital never moves without the register (INV-13, which journal_line_check already enforces by source).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.share_issue (
  organisation_id  uuid           NOT NULL DEFAULT fyniq.current_org(),
  id               uuid           NOT NULL DEFAULT uuidv7(),
  holder_id        uuid           NOT NULL,  -- the contact the shares are allotted to
  class_account_id uuid           NOT NULL,  -- the share-capital account of the class allotted
  shares           numeric(16, 0) NOT NULL,
  price            numeric(20, 6) NOT NULL,  -- per share, in the currency
  currency         text           NOT NULL,
  minor_units      smallint       NOT NULL,
  amount           numeric(20, 4) NOT NULL,  -- what the shares are paid up with: RND-SHARE's converted amount for a conversion
  consideration    text           NOT NULL,
  resolution_refs  text           NOT NULL,  -- the shareholders' approval and the board resolution, by reference
  lodgement_ref    text,                     -- ACRA's lodgement reference for the allotment
  effective_on     date           NOT NULL,  -- the day ACRA's register of members shows it; never backdated
  settlement_id    uuid,                     -- the settlement a conversion decides
  entry_id         uuid,                     -- the entry it posted; none before the books start
  recorded_by      uuid           NOT NULL,
  recorded_at      timestamptz    NOT NULL,
  CONSTRAINT share_issue_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT share_issue_holder_fkey FOREIGN KEY (organisation_id, holder_id) REFERENCES fyniq.contact (organisation_id, id),
  CONSTRAINT share_issue_class_fkey FOREIGN KEY (organisation_id, class_account_id) REFERENCES fyniq.account (organisation_id, id),
  CONSTRAINT share_issue_settlement_fkey FOREIGN KEY (organisation_id, settlement_id) REFERENCES fyniq.document (organisation_id, id),
  CONSTRAINT share_issue_entry_fkey FOREIGN KEY (organisation_id, entry_id) REFERENCES fyniq.journal_entry (organisation_id, id),
  CONSTRAINT share_issue_recorded_by_fkey FOREIGN KEY (recorded_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT share_issue_entry_once UNIQUE (organisation_id, entry_id),
  CONSTRAINT share_issue_shares_positive CHECK (shares <> 'NaN' AND shares > 0),
  CONSTRAINT share_issue_price_positive CHECK (price <> 'NaN' AND price > 0),
  CONSTRAINT share_issue_currency_format CHECK (currency ~ '^[A-Z]{3}$'),
  CONSTRAINT share_issue_minor_units CHECK (minor_units BETWEEN 0 AND 4),
  CONSTRAINT share_issue_amount_scale CHECK (amount <> 'NaN' AND amount = trunc(amount, minor_units) AND amount > 0),
  CONSTRAINT share_issue_consideration_known CHECK (consideration IN ('cash', 'debt_capitalised', 'services', 'safe_or_note')),
  CONSTRAINT share_issue_conversion_names_settlement CHECK (settlement_id IS NULL OR consideration = 'debt_capitalised'),
  CONSTRAINT share_issue_resolution_length CHECK (length(btrim(resolution_refs)) BETWEEN 1 AND 500),
  CONSTRAINT share_issue_lodgement_length CHECK (lodgement_ref IS NULL OR length(btrim(lodgement_ref)) BETWEEN 1 AND 100)
);
CREATE INDEX share_issue_holder ON fyniq.share_issue (organisation_id, holder_id);
CREATE INDEX share_issue_settlement ON fyniq.share_issue (organisation_id, settlement_id);

-- +goose StatementBegin
-- A share issue is written once, never changed, deleted or truncated; its class is an active share-capital account and
-- its holder a director or shareholder; with no entry it is dated before the books start, and with one it names an
-- entry of the source share_issue for it, posted on its effective date, that credits its class with its amount (FY255).
CREATE FUNCTION fyniq.share_issue_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  e        fyniq.journal_entry;
  start    date;
  credited numeric;
BEGIN
  IF TG_OP = 'TRUNCATE' THEN
    RAISE EXCEPTION 'The register of share issues is never deleted' USING ERRCODE = 'FY255';
  END IF;
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'Share issue % is never changed or deleted', OLD.id USING ERRCODE = 'FY255',
      HINT = 'An allotment stays in the register; correct it with a new issue or as the accountant advises (FR-EQ-08).';
  END IF;
  IF NEW.shares = 'NaN' OR NEW.price = 'NaN' OR NEW.amount = 'NaN' THEN -- as the checks say, before the guard reads anything (§11.3)
    RAISE EXCEPTION 'A share issue''s shares, price and amount are numbers' USING ERRCODE = 'check_violation';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM fyniq.account a WHERE a.organisation_id = NEW.organisation_id AND a.id = NEW.class_account_id
                  AND a.role = 'share-capital' AND a.is_active AND NOT a.is_group) THEN
    RAISE EXCEPTION 'The class of share issue % is not an active share-capital account', NEW.id USING ERRCODE = 'FY255',
      HINT = 'Allot shares to 3010 Ordinary or 3020 Preference share capital (FR-EQ-08).';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM fyniq.contact c WHERE c.organisation_id = NEW.organisation_id AND c.id = NEW.holder_id
                  AND (c.is_director OR c.is_shareholder)) THEN
    RAISE EXCEPTION 'The holder of share issue % is not a director or shareholder', NEW.id USING ERRCODE = 'FY255',
      HINT = 'Mark the contact a shareholder first (FR-EQ-08).';
  END IF;
  IF NEW.entry_id IS NULL THEN
    SELECT o.books_start INTO start FROM fyniq.organisation o WHERE o.organisation_id = NEW.organisation_id;
    IF start IS NULL OR NEW.effective_on >= start THEN
      RAISE EXCEPTION 'Share issue % has no entry, so it is dated before the books start', NEW.id USING ERRCODE = 'FY255',
        HINT = 'Only an issue before the books start is recorded without an entry; its share capital is in the opening balances (FR-EQ-08).';
    END IF;
    RETURN NEW;
  END IF;
  SELECT * INTO e FROM fyniq.journal_entry WHERE organisation_id = NEW.organisation_id AND id = NEW.entry_id;
  SELECT -sum(l.base_amount) INTO credited FROM fyniq.journal_line l
   WHERE l.organisation_id = NEW.organisation_id AND l.entry_id = NEW.entry_id AND l.account_id = NEW.class_account_id;
  IF e.id IS NULL OR e.source_type <> 'share_issue' OR e.source_id <> NEW.id::text OR e.reverses_entry_id IS NOT NULL
     OR e.posting_date <> NEW.effective_on OR credited IS DISTINCT FROM NEW.amount THEN
    RAISE EXCEPTION 'Share issue % names an entry that does not post it: from the register, on its effective date, crediting its class with its amount', NEW.id
      USING ERRCODE = 'FY255', HINT = 'Post the issue from the register of share issues, never backdated (FR-EQ-06, FR-EQ-08).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER share_issue_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.share_issue
  FOR EACH ROW EXECUTE FUNCTION fyniq.share_issue_guard();
CREATE TRIGGER share_issue_no_truncate BEFORE TRUNCATE ON fyniq.share_issue
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.share_issue_guard();

-- +goose StatementBegin
-- An entry from the source share_issue commits only with the register's row that names it, or as the reversal of an
-- entry that has one (FY255; INV-13): share capital never moves without the register.
CREATE FUNCTION fyniq.share_issue_entry_registered() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM fyniq.share_issue s WHERE s.organisation_id = NEW.organisation_id
                  AND s.entry_id = COALESCE(NEW.reverses_entry_id, NEW.id)) THEN
    RAISE EXCEPTION 'Entry % comes from the register of share issues, but no share issue names it', NEW.id
      USING ERRCODE = 'FY255', HINT = 'Record the allotment in the register of share issues, which posts it (FR-EQ-08, INV-13).';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER share_issue_entry_registered AFTER INSERT ON fyniq.journal_entry
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (NEW.source_type = 'share_issue')
  EXECUTE FUNCTION fyniq.share_issue_entry_registered();

ALTER TABLE fyniq.share_issue ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.share_issue FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.share_issue
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

-- Each issue is audited in the same transaction (AUD-01).
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.share_issue
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('share_issue', 'id');

REVOKE ALL ON fyniq.share_issue FROM PUBLIC;
GRANT SELECT ON fyniq.share_issue TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (id, holder_id, class_account_id, shares, price, currency, minor_units, amount, consideration, resolution_refs, lodgement_ref,
              effective_on, settlement_id, entry_id, recorded_by, recorded_at) ON fyniq.share_issue TO fyniq_api;
