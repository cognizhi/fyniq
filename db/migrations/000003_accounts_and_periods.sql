-- The chart of accounts (minimal for P0) and accounting periods (PRD-001 §11.4; INV-03, INV-04).
-- The full account attributes of FR-COA-02 and account versions arrive with the P1 chart of
-- accounts; dimensions with FR-DIM.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- ------------------------------------------------------------------ accounts
-- Posting goes only to leaves (FR-COA-05): a group account is a heading for roll-ups and takes no
-- lines. An account is never deleted; it is deactivated (FR-COA-04).
CREATE TABLE fyniq.account (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  code            text        NOT NULL,
  name            text        NOT NULL,
  type            text        NOT NULL,
  parent_id       uuid,
  is_group        boolean     NOT NULL DEFAULT false,
  is_active       boolean     NOT NULL DEFAULT true,
  version         integer     NOT NULL DEFAULT 1,
  created_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT account_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT account_code_key UNIQUE (organisation_id, code),
  CONSTRAINT account_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT account_parent_fkey FOREIGN KEY (organisation_id, parent_id)
    REFERENCES fyniq.account (organisation_id, id),
  CONSTRAINT account_code_format CHECK (code ~ '^[0-9A-Za-z][0-9A-Za-z.-]{0,19}$'),
  CONSTRAINT account_name_length CHECK (length(btrim(name)) BETWEEN 1 AND 200),
  CONSTRAINT account_type_known CHECK (type IN ('asset', 'liability', 'equity', 'revenue', 'expense')),
  CONSTRAINT account_not_own_parent CHECK (parent_id <> id),
  CONSTRAINT account_version_positive CHECK (version > 0)
);
CREATE INDEX account_parent_idx ON fyniq.account (organisation_id, parent_id);

-- The hierarchy stays a tree of group accounts with leaves under them; a group never takes lines.
-- +goose StatementBegin
CREATE FUNCTION fyniq.account_guard() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'UPDATE' AND (NEW.code IS DISTINCT FROM OLD.code
     OR NEW.type IS DISTINCT FROM OLD.type OR NEW.created_at IS DISTINCT FROM OLD.created_at) THEN
    RAISE EXCEPTION 'The code and type of account % cannot change', OLD.code
      USING ERRCODE = 'FY040', HINT = 'Create a new account and deactivate this one (FR-COA-04).';
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
CREATE TRIGGER account_guard BEFORE INSERT OR UPDATE ON fyniq.account
  FOR EACH ROW EXECUTE FUNCTION fyniq.account_guard();

-- ------------------------------------------------------------------ periods
-- open → soft_locked → closed (the hard lock of INV-03); reopening goes back to open. Periods of one
-- organisation never overlap. closed_revision is the ledger revision at the close, set by period_guard
-- (ENG-47).
CREATE TABLE fyniq.period (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  starts_on       date        NOT NULL,
  ends_on         date        NOT NULL,
  status          text        NOT NULL DEFAULT 'open',
  closed_revision bigint,
  version         integer     NOT NULL DEFAULT 1,
  created_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT period_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT period_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT period_dates_ordered CHECK (ends_on >= starts_on),
  CONSTRAINT period_status_known CHECK (status IN ('open', 'soft_locked', 'closed')),
  CONSTRAINT period_closed_revision CHECK ((status = 'closed') = (closed_revision IS NOT NULL)),
  CONSTRAINT period_closed_revision_not_negative CHECK (closed_revision >= 0),
  CONSTRAINT period_version_positive CHECK (version > 0),
  CONSTRAINT period_no_overlap EXCLUDE USING gist
    (organisation_id WITH =, daterange(starts_on, ends_on, '[]') WITH &&)
);

-- A period's dates never change once it exists: entries and balances are keyed on it. A change of
-- status takes the ledger lock (ENG-47) and records the revision at the close, whoever writes it. The
-- unit of work takes that lock first, so here it is already held; a writer that skips the unit of
-- work at worst deadlocks with a posting, and one of the two is refused.
-- +goose StatementBegin
CREATE FUNCTION fyniq.period_guard() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF NEW.starts_on IS DISTINCT FROM OLD.starts_on OR NEW.ends_on IS DISTINCT FROM OLD.ends_on
     OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
    RAISE EXCEPTION 'The dates of the period starting % cannot change', OLD.starts_on
      USING ERRCODE = 'FY031';
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
CREATE TRIGGER period_guard BEFORE UPDATE ON fyniq.period
  FOR EACH ROW EXECUTE FUNCTION fyniq.period_guard();

-- ------------------------------------------------------------------ row-level security
ALTER TABLE fyniq.account ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.account FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.account
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

ALTER TABLE fyniq.period ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.period FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.period
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

-- ------------------------------------------------------------------ grants (db/README.md)
GRANT SELECT ON fyniq.account, fyniq.period TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
GRANT INSERT (code, name, type, parent_id, is_group, is_active),
      UPDATE (name, parent_id, is_group, is_active, version)
  ON fyniq.account TO fyniq_api;
GRANT INSERT (starts_on, ends_on), UPDATE (status, version)
  ON fyniq.period TO fyniq_api;
