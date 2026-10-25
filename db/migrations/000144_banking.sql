-- Money accounts, statements and bank lines (PRD-001 FR-BNK-01, FR-BNK-02, FR-BNK-04, FR-BNK-15, FR-BNK-16;
-- p1-bank-import-reconcile task 2.1, design D1 and D2).
--
-- A money account is the register of a chart account marked is_money_account: its kind, its currency, the identifier
-- its statements name it by, and its data source, effective-dated so a switch keeps its history (UC-BNK-03). Files,
-- emailed reports and API connectors all produce the same statement and lines. A statement and its lines never change
-- once imported: the lines arrive in their statement's transaction, which audits the import, and only a line's status
-- moves afterwards, forward except an undo while its period is open. A break in the balance chain is a gap, recorded
-- and resolved by a person, never filled by guesswork. Every table is a tenant table under FORCE RLS.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- ------------------------------------------------------------------ money accounts
CREATE TABLE fyniq.money_account (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  account_id      uuid        NOT NULL,
  kind            text        NOT NULL,
  name            text        NOT NULL,
  institution     text,
  identifier      text,
  currency        text        NOT NULL,
  opened_on       date,
  closed_on       date,
  created_by      text        NOT NULL,
  created_at      timestamptz NOT NULL,
  changed_by      text        NOT NULL,
  changed_at      timestamptz NOT NULL,
  version         integer     NOT NULL DEFAULT 1,
  CONSTRAINT money_account_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT money_account_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT money_account_account_fkey FOREIGN KEY (organisation_id, account_id) REFERENCES fyniq.account (organisation_id, id),
  CONSTRAINT money_account_account_key UNIQUE (organisation_id, account_id),
  CONSTRAINT money_account_kind_known CHECK (kind IN ('bank', 'card', 'wallet', 'payment_gateway', 'cash')),
  CONSTRAINT money_account_name_length CHECK (length(btrim(name)) BETWEEN 1 AND 120),
  CONSTRAINT money_account_institution_length CHECK (length(btrim(institution)) BETWEEN 1 AND 120),
  CONSTRAINT money_account_identifier_form CHECK (identifier ~ '^[A-Za-z0-9][A-Za-z0-9 ./-]*$' AND length(identifier) <= 64),
  CONSTRAINT money_account_currency_form CHECK (currency ~ '^[A-Z]{3}$'),
  CONSTRAINT money_account_dates_ordered CHECK (closed_on >= opened_on),
  CONSTRAINT money_account_by_form CHECK (created_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$' AND changed_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$'),
  CONSTRAINT money_account_version_positive CHECK (version >= 1)
);
-- One money account per identifier and currency, so a statement names exactly one.
CREATE UNIQUE INDEX money_account_identifier ON fyniq.money_account (organisation_id, identifier, currency) WHERE identifier IS NOT NULL;

-- ------------------------------------------------------------------ saved column mappings for CSV and XLSX
CREATE TABLE fyniq.statement_mapping (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  name            text        NOT NULL,
  institution     text,
  format          text        NOT NULL,
  spec            jsonb       NOT NULL,
  created_by      text        NOT NULL,
  created_at      timestamptz NOT NULL,
  changed_by      text        NOT NULL,
  changed_at      timestamptz NOT NULL,
  version         integer     NOT NULL DEFAULT 1,
  CONSTRAINT statement_mapping_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT statement_mapping_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT statement_mapping_name_key UNIQUE (organisation_id, name),
  CONSTRAINT statement_mapping_name_length CHECK (length(btrim(name)) BETWEEN 1 AND 120),
  CONSTRAINT statement_mapping_institution_length CHECK (length(btrim(institution)) BETWEEN 1 AND 120),
  CONSTRAINT statement_mapping_format_known CHECK (format IN ('csv', 'xlsx')),
  CONSTRAINT statement_mapping_spec_object CHECK (jsonb_typeof(spec) = 'object' AND length(spec::text) <= 16384),
  CONSTRAINT statement_mapping_by_form CHECK (created_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$' AND changed_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$'),
  CONSTRAINT statement_mapping_version_positive CHECK (version >= 1)
);

-- ------------------------------------------------------------------ where a money account's data comes from
CREATE TABLE fyniq.money_account_source (
  organisation_id  uuid        NOT NULL DEFAULT fyniq.current_org(),
  id               uuid        NOT NULL DEFAULT uuidv7(),
  money_account_id uuid        NOT NULL,
  kind             text        NOT NULL,
  connector_id     uuid,
  senders          text[]      NOT NULL DEFAULT '{}',
  report_format    text,
  mapping_id       uuid,
  from_date        date        NOT NULL,
  set_by           text        NOT NULL,
  set_at           timestamptz NOT NULL,
  CONSTRAINT money_account_source_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT money_account_source_account_fkey FOREIGN KEY (organisation_id, money_account_id)
    REFERENCES fyniq.money_account (organisation_id, id),
  CONSTRAINT money_account_source_connector_fkey FOREIGN KEY (organisation_id, connector_id)
    REFERENCES fyniq.organisation_connector (organisation_id, id),
  CONSTRAINT money_account_source_mapping_fkey FOREIGN KEY (organisation_id, mapping_id)
    REFERENCES fyniq.statement_mapping (organisation_id, id),
  CONSTRAINT money_account_source_from_key UNIQUE (organisation_id, money_account_id, from_date),
  CONSTRAINT money_account_source_kind_known CHECK (kind IN ('file', 'email', 'connector')),
  CONSTRAINT money_account_source_connector_kind CHECK ((kind = 'connector') = (connector_id IS NOT NULL)),
  -- Reports by email come only from the bank's registered addresses, in a known format (UC-BNK-01).
  CONSTRAINT money_account_source_email_senders CHECK (kind <> 'email' OR (cardinality(senders) BETWEEN 1 AND 10 AND report_format IS NOT NULL)),
  CONSTRAINT money_account_source_senders_form CHECK (cardinality(senders) <= 10
    AND array_to_string(senders, ' ') ~ '^([a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]+( [a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]+)*)?$'),
  CONSTRAINT money_account_source_format_known CHECK (report_format IN ('csv', 'xlsx', 'ofx', 'camt053', 'mt940', 'mt940_sap')),
  CONSTRAINT money_account_source_mapping_format CHECK (mapping_id IS NULL OR report_format IN ('csv', 'xlsx')),
  CONSTRAINT money_account_source_by_form CHECK (set_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);

-- ------------------------------------------------------------------ statements and their lines
CREATE TABLE fyniq.bank_statement (
  organisation_id  uuid          NOT NULL DEFAULT fyniq.current_org(),
  id               uuid          NOT NULL DEFAULT uuidv7(),
  money_account_id uuid          NOT NULL,
  source           text          NOT NULL,
  format           text          NOT NULL,
  parser_version   text          NOT NULL,
  period_from      date          NOT NULL,
  period_to        date          NOT NULL,
  currency         text          NOT NULL,
  minor_units      smallint      NOT NULL,
  opening_balance  numeric(20,4),
  closing_balance  numeric(20,4),
  line_count       integer       NOT NULL,
  file_ref         text,
  connector_id     uuid,
  imported_by      text          NOT NULL,
  imported_at      timestamptz   NOT NULL,
  created_xact     bigint        NOT NULL DEFAULT pg_current_xact_id()::text::bigint,
  CONSTRAINT bank_statement_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT bank_statement_account_fkey FOREIGN KEY (organisation_id, money_account_id)
    REFERENCES fyniq.money_account (organisation_id, id),
  CONSTRAINT bank_statement_connector_fkey FOREIGN KEY (organisation_id, connector_id)
    REFERENCES fyniq.organisation_connector (organisation_id, id),
  CONSTRAINT bank_statement_currency_fkey FOREIGN KEY (currency, minor_units) REFERENCES fyniq.currency (code, minor_units),
  CONSTRAINT bank_statement_source_known CHECK (source IN ('file', 'email', 'connector')),
  CONSTRAINT bank_statement_format_known CHECK (format IN ('csv', 'xlsx', 'ofx', 'camt053', 'mt940', 'mt940_sap', 'api')),
  CONSTRAINT bank_statement_connector_source CHECK ((source = 'connector') = (connector_id IS NOT NULL)),
  CONSTRAINT bank_statement_parser_form CHECK (parser_version ~ '^[a-z0-9_]+@[0-9]+$'),
  CONSTRAINT bank_statement_period_ordered CHECK (period_to >= period_from),
  CONSTRAINT bank_statement_balances_scale CHECK (opening_balance <> 'NaN' AND opening_balance = trunc(opening_balance, minor_units)
    AND closing_balance <> 'NaN' AND closing_balance = trunc(closing_balance, minor_units)),
  CONSTRAINT bank_statement_line_count CHECK (line_count BETWEEN 0 AND 100000),
  CONSTRAINT bank_statement_file_ref_form CHECK (length(file_ref) BETWEEN 1 AND 512 AND file_ref ~ '^[A-Za-z0-9:/_.-]+$'),
  CONSTRAINT bank_statement_by_form CHECK (imported_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);
CREATE INDEX bank_statement_account_period ON fyniq.bank_statement (organisation_id, money_account_id, period_to);

CREATE TABLE fyniq.bank_line (
  organisation_id  uuid          NOT NULL DEFAULT fyniq.current_org(),
  id               uuid          NOT NULL DEFAULT uuidv7(),
  statement_id     uuid          NOT NULL,
  money_account_id uuid          NOT NULL,
  position         integer       NOT NULL,
  booking_date     date          NOT NULL,
  value_date       date,
  amount           numeric(20,4) NOT NULL,
  currency         text          NOT NULL,
  minor_units      smallint      NOT NULL,
  reference        text,
  counterparty     text,
  description      text,
  running_balance  numeric(20,4),
  fingerprint      text          NOT NULL,
  status           text          NOT NULL,
  duplicate_of     uuid,
  status_by        text          NOT NULL,
  status_at        timestamptz   NOT NULL,
  created_xact     bigint        NOT NULL DEFAULT pg_current_xact_id()::text::bigint,
  version          integer       NOT NULL DEFAULT 1,
  CONSTRAINT bank_line_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT bank_line_statement_fkey FOREIGN KEY (organisation_id, statement_id)
    REFERENCES fyniq.bank_statement (organisation_id, id),
  CONSTRAINT bank_line_account_fkey FOREIGN KEY (organisation_id, money_account_id)
    REFERENCES fyniq.money_account (organisation_id, id),
  CONSTRAINT bank_line_duplicate_fkey FOREIGN KEY (organisation_id, duplicate_of)
    REFERENCES fyniq.bank_line (organisation_id, id),
  CONSTRAINT bank_line_currency_fkey FOREIGN KEY (currency, minor_units) REFERENCES fyniq.currency (code, minor_units),
  CONSTRAINT bank_line_position_key UNIQUE (organisation_id, statement_id, position),
  CONSTRAINT bank_line_position_positive CHECK (position >= 1),
  CONSTRAINT bank_line_amount_scale CHECK (amount <> 'NaN' AND amount <> 0 AND amount = trunc(amount, minor_units)),
  CONSTRAINT bank_line_running_balance_scale CHECK (running_balance <> 'NaN' AND running_balance = trunc(running_balance, minor_units)),
  CONSTRAINT bank_line_reference_length CHECK (length(reference) BETWEEN 1 AND 140),
  CONSTRAINT bank_line_counterparty_length CHECK (length(counterparty) BETWEEN 1 AND 200),
  CONSTRAINT bank_line_description_length CHECK (length(description) BETWEEN 1 AND 500),
  CONSTRAINT bank_line_fingerprint_form CHECK (fingerprint ~ '^[0-9a-f]{64}$'),
  CONSTRAINT bank_line_status_known CHECK (status IN ('unreconciled', 'possible_duplicate', 'categorised', 'matched', 'transferred',
                                                      'suspense', 'excluded')),
  CONSTRAINT bank_line_duplicate_of CHECK ((status = 'possible_duplicate') <= (duplicate_of IS NOT NULL)),
  CONSTRAINT bank_line_by_form CHECK (status_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$'),
  CONSTRAINT bank_line_version_positive CHECK (version >= 1)
);
CREATE INDEX bank_line_account_date ON fyniq.bank_line (organisation_id, money_account_id, booking_date);
CREATE INDEX bank_line_fingerprint ON fyniq.bank_line (organisation_id, fingerprint);
CREATE INDEX bank_line_queue ON fyniq.bank_line (organisation_id, money_account_id, booking_date)
  WHERE status IN ('unreconciled', 'possible_duplicate', 'suspense');

-- A break in an account's balance chain (FR-BNK-16): the opening balance a statement should have had, and the one it
-- had. A person resolves it once, with the reason.
CREATE TABLE fyniq.bank_gap (
  organisation_id  uuid          NOT NULL DEFAULT fyniq.current_org(),
  id               uuid          NOT NULL DEFAULT uuidv7(),
  money_account_id uuid          NOT NULL,
  statement_id     uuid          NOT NULL,
  after_date       date,
  before_date      date          NOT NULL,
  currency         text          NOT NULL,
  minor_units      smallint      NOT NULL,
  expected_opening numeric(20,4),
  found_opening    numeric(20,4),
  found_at         timestamptz   NOT NULL,
  resolved_by      text,
  resolved_at      timestamptz,
  resolution       text,
  CONSTRAINT bank_gap_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT bank_gap_account_fkey FOREIGN KEY (organisation_id, money_account_id)
    REFERENCES fyniq.money_account (organisation_id, id),
  CONSTRAINT bank_gap_statement_fkey FOREIGN KEY (organisation_id, statement_id)
    REFERENCES fyniq.bank_statement (organisation_id, id),
  CONSTRAINT bank_gap_currency_fkey FOREIGN KEY (currency, minor_units) REFERENCES fyniq.currency (code, minor_units),
  CONSTRAINT bank_gap_dates_ordered CHECK (before_date >= after_date),
  CONSTRAINT bank_gap_amounts_scale CHECK (expected_opening <> 'NaN' AND expected_opening = trunc(expected_opening, minor_units)
    AND found_opening <> 'NaN' AND found_opening = trunc(found_opening, minor_units)),
  CONSTRAINT bank_gap_resolved_whole CHECK ((resolved_by IS NULL) = (resolved_at IS NULL) AND (resolved_by IS NULL) = (resolution IS NULL)),
  CONSTRAINT bank_gap_resolution_length CHECK (length(btrim(resolution)) BETWEEN 1 AND 500),
  CONSTRAINT bank_gap_by_form CHECK (resolved_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);
CREATE INDEX bank_gap_open ON fyniq.bank_gap (organisation_id, money_account_id) WHERE resolved_at IS NULL;

-- ------------------------------------------------------------------ guards
-- +goose StatementBegin
-- A money account sits on a postable chart account marked a money account, in that account's currency (or the base
-- currency where the account names none); its account, currency and creation never change, its version rises by one,
-- and it is never deleted, only closed (FY216).
CREATE FUNCTION fyniq.money_account_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  a fyniq.account;
  base text;
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'Money account % is never deleted', OLD.id USING ERRCODE = 'FY216', HINT = 'Close it instead: its statements stay (FR-BNK-01).';
  END IF;
  IF TG_OP = 'UPDATE' THEN
    IF NEW.account_id IS DISTINCT FROM OLD.account_id OR NEW.currency IS DISTINCT FROM OLD.currency
       OR NEW.created_by IS DISTINCT FROM OLD.created_by OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
      RAISE EXCEPTION 'Money account % keeps its chart account, currency and creation', OLD.id
        USING ERRCODE = 'FY216', HINT = 'Open a new money account for another account or currency.';
    END IF;
    IF NEW.version <> OLD.version + 1 THEN
      RAISE EXCEPTION 'Money account % changes one version at a time', OLD.id USING ERRCODE = 'FY216';
    END IF;
    RETURN NEW;
  END IF;
  SELECT * INTO a FROM fyniq.account WHERE organisation_id = NEW.organisation_id AND id = NEW.account_id;
  IF NOT a.is_money_account OR a.is_group OR NOT a.is_active THEN
    RAISE EXCEPTION 'Account % is not an active, postable money account', a.code
      USING ERRCODE = 'FY216', HINT = 'Mark the chart account a money account first (FR-COA).';
  END IF;
  SELECT base_currency INTO base FROM fyniq.organisation WHERE organisation_id = NEW.organisation_id;
  IF NEW.currency <> coalesce(a.currency, base) THEN
    RAISE EXCEPTION 'Money account in % sits on account % in %', NEW.currency, a.code, coalesce(a.currency, base)
      USING ERRCODE = 'FY216', HINT = 'A money account takes its chart account''s currency.';
  END IF;
  IF NEW.version <> 1 THEN
    RAISE EXCEPTION 'A new money account starts at version 1' USING ERRCODE = 'FY216';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER money_account_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.money_account
  FOR EACH ROW EXECUTE FUNCTION fyniq.money_account_guard();

-- +goose StatementBegin
-- A source is added, never changed or removed, from a date after the account's current source (FY217): a switch keeps
-- the history of where the account's data came from (UC-BNK-03). A closed account takes no new source.
CREATE FUNCTION fyniq.money_account_source_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  latest date;
  closed date;
BEGIN
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'A money account''s source is never changed or removed' USING ERRCODE = 'FY217',
      HINT = 'Add a new source from a later date (UC-BNK-03).';
  END IF;
  SELECT max(from_date) INTO latest FROM fyniq.money_account_source
   WHERE organisation_id = NEW.organisation_id AND money_account_id = NEW.money_account_id;
  IF latest IS NOT NULL AND NEW.from_date <= latest THEN
    RAISE EXCEPTION 'A new source starts after % , the current one''s start', latest USING ERRCODE = 'FY217';
  END IF;
  SELECT closed_on INTO closed FROM fyniq.money_account WHERE organisation_id = NEW.organisation_id AND id = NEW.money_account_id;
  IF closed IS NOT NULL THEN
    RAISE EXCEPTION 'Money account % is closed', NEW.money_account_id USING ERRCODE = 'FY217';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER money_account_source_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.money_account_source
  FOR EACH ROW EXECUTE FUNCTION fyniq.money_account_source_guard();

-- +goose StatementBegin
-- A saved mapping changes one version at a time and is never deleted (FY218): statements imported through it name it.
CREATE FUNCTION fyniq.statement_mapping_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A saved mapping is never deleted' USING ERRCODE = 'FY218';
  END IF;
  IF TG_OP = 'UPDATE' AND (NEW.version <> OLD.version + 1 OR NEW.created_by IS DISTINCT FROM OLD.created_by
                           OR NEW.created_at IS DISTINCT FROM OLD.created_at) THEN
    RAISE EXCEPTION 'Saved mapping % changes one version at a time and keeps its creation', OLD.id USING ERRCODE = 'FY218';
  END IF;
  IF TG_OP = 'INSERT' AND NEW.version <> 1 THEN
    RAISE EXCEPTION 'A new saved mapping starts at version 1' USING ERRCODE = 'FY218';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER statement_mapping_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.statement_mapping
  FOR EACH ROW EXECUTE FUNCTION fyniq.statement_mapping_guard();

-- +goose StatementBegin
-- A statement is in its account's currency, and never changes or goes once imported (FY219).
CREATE FUNCTION fyniq.bank_statement_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  cur text;
BEGIN
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'Statement % never changes once imported', OLD.id USING ERRCODE = 'FY219',
      HINT = 'Its lines move on their own; a wrong statement''s lines are excluded, never edited.';
  END IF;
  SELECT currency INTO cur FROM fyniq.money_account WHERE organisation_id = NEW.organisation_id AND id = NEW.money_account_id;
  IF NEW.currency <> cur THEN
    RAISE EXCEPTION 'A statement in % for a money account in %', NEW.currency, cur USING ERRCODE = 'FY219';
  END IF;
  NEW.created_xact := pg_current_xact_id()::text::bigint;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER bank_statement_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.bank_statement
  FOR EACH ROW EXECUTE FUNCTION fyniq.bank_statement_guard();

-- +goose StatementBegin
-- A line arrives with its statement, in its transaction, in its currency and account, as unreconciled or possible
-- duplicate; its facts never change, it is never deleted, its version rises by one, and its status moves only along
-- the queue (FY220). Back to unreconciled is an undo, allowed while the period of its booking date is open.
CREATE FUNCTION fyniq.bank_line_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  s fyniq.bank_statement;
  closed boolean;
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'Bank line % is never deleted', OLD.id USING ERRCODE = 'FY220', HINT = 'Exclude it instead (FR-BNK-04).';
  END IF;
  IF TG_OP = 'INSERT' THEN
    SELECT * INTO s FROM fyniq.bank_statement WHERE organisation_id = NEW.organisation_id AND id = NEW.statement_id;
    IF s.created_xact <> pg_current_xact_id()::text::bigint THEN
      RAISE EXCEPTION 'Bank line arrives only with its statement %', NEW.statement_id USING ERRCODE = 'FY220',
        HINT = 'Import the statement again: its lines come in its transaction.';
    END IF;
    IF NEW.money_account_id <> s.money_account_id OR NEW.currency <> s.currency THEN
      RAISE EXCEPTION 'Bank line in another account or currency than its statement %', NEW.statement_id USING ERRCODE = 'FY220';
    END IF;
    IF NEW.status NOT IN ('unreconciled', 'possible_duplicate') OR NEW.version <> 1 THEN
      RAISE EXCEPTION 'A new bank line is unreconciled or a possible duplicate, at version 1' USING ERRCODE = 'FY220';
    END IF;
    NEW.created_xact := s.created_xact;
    RETURN NEW;
  END IF;
  IF (NEW.statement_id, NEW.money_account_id, NEW.position, NEW.booking_date, NEW.value_date, NEW.amount, NEW.currency,
      NEW.minor_units, NEW.reference, NEW.counterparty, NEW.description, NEW.running_balance, NEW.fingerprint,
      NEW.duplicate_of, NEW.created_xact)
     IS DISTINCT FROM
     (OLD.statement_id, OLD.money_account_id, OLD.position, OLD.booking_date, OLD.value_date, OLD.amount, OLD.currency,
      OLD.minor_units, OLD.reference, OLD.counterparty, OLD.description, OLD.running_balance, OLD.fingerprint,
      OLD.duplicate_of, OLD.created_xact) THEN
    RAISE EXCEPTION 'Bank line % keeps what the bank said', OLD.id USING ERRCODE = 'FY220';
  END IF;
  IF NEW.version <> OLD.version + 1 THEN
    RAISE EXCEPTION 'Bank line % changes one version at a time', OLD.id USING ERRCODE = 'FY220';
  END IF;
  IF NEW.status = OLD.status THEN
    RETURN NEW;
  END IF;
  IF NOT ((OLD.status = 'possible_duplicate' AND NEW.status IN ('unreconciled', 'excluded'))
       OR (OLD.status = 'unreconciled' AND NEW.status IN ('categorised', 'matched', 'transferred', 'suspense', 'excluded'))
       OR (OLD.status = 'suspense' AND NEW.status IN ('categorised', 'matched', 'transferred'))
       OR (OLD.status IN ('categorised', 'matched', 'transferred', 'excluded') AND NEW.status = 'unreconciled')) THEN
    RAISE EXCEPTION 'Bank line % cannot go from % to %', OLD.id, OLD.status, NEW.status USING ERRCODE = 'FY220';
  END IF;
  IF NEW.status = 'unreconciled' AND OLD.status <> 'possible_duplicate' THEN
    SELECT p.status = 'closed' INTO closed FROM fyniq.period p
     WHERE p.organisation_id = NEW.organisation_id AND OLD.booking_date BETWEEN p.starts_on AND p.ends_on;
    IF coalesce(closed, false) THEN
      RAISE EXCEPTION 'Bank line % of % is in a closed period', OLD.id, OLD.booking_date USING ERRCODE = 'FY220',
        HINT = 'Undo only while the period is open (FR-BNK-14); reopen it first.';
    END IF;
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER bank_line_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.bank_line
  FOR EACH ROW EXECUTE FUNCTION fyniq.bank_line_guard();

-- +goose StatementBegin
-- A gap is found once and resolved once, with its reason; it is never deleted (FY221).
CREATE FUNCTION fyniq.bank_gap_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A gap is never deleted' USING ERRCODE = 'FY221', HINT = 'Resolve it with the reason (FR-BNK-16).';
  END IF;
  IF TG_OP = 'INSERT' THEN
    IF NEW.resolved_at IS NOT NULL THEN
      RAISE EXCEPTION 'A gap is found unresolved' USING ERRCODE = 'FY221';
    END IF;
    RETURN NEW;
  END IF;
  IF OLD.resolved_at IS NOT NULL
     OR (NEW.money_account_id, NEW.statement_id, NEW.after_date, NEW.before_date, NEW.currency, NEW.minor_units,
         NEW.expected_opening, NEW.found_opening, NEW.found_at)
        IS DISTINCT FROM
        (OLD.money_account_id, OLD.statement_id, OLD.after_date, OLD.before_date, OLD.currency, OLD.minor_units,
         OLD.expected_opening, OLD.found_opening, OLD.found_at) THEN
    RAISE EXCEPTION 'Gap % is resolved once, and keeps what was found', OLD.id USING ERRCODE = 'FY221';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER bank_gap_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.bank_gap
  FOR EACH ROW EXECUTE FUNCTION fyniq.bank_gap_guard();

-- ------------------------------------------------------------------ tenancy
ALTER TABLE fyniq.money_account ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.money_account FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.money_account
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.statement_mapping ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.statement_mapping FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.statement_mapping
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.money_account_source ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.money_account_source FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.money_account_source
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.bank_statement ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.bank_statement FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.bank_statement
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.bank_line ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.bank_line FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.bank_line
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.bank_gap ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.bank_gap FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.bank_gap
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

-- ------------------------------------------------------------------ audit (AUD-01)
-- Each change is audited in its transaction. A statement's import event stands for its lines, which arrive with it;
-- a line's later status changes are audited each.
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.money_account
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('money_account', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.statement_mapping
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('statement_mapping', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.money_account_source
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('money_account', 'money_account_id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.bank_statement
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('bank_statement', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER UPDATE ON fyniq.bank_line
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('bank_line', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.bank_gap
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('bank_gap', 'id');

-- ------------------------------------------------------------------ grants
REVOKE ALL ON fyniq.money_account, fyniq.statement_mapping, fyniq.money_account_source, fyniq.bank_statement, fyniq.bank_line,
  fyniq.bank_gap FROM PUBLIC;
GRANT SELECT ON fyniq.money_account, fyniq.statement_mapping, fyniq.money_account_source, fyniq.bank_statement, fyniq.bank_line,
  fyniq.bank_gap TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (account_id, kind, name, institution, identifier, currency, opened_on, created_by, created_at, changed_by, changed_at)
  ON fyniq.money_account TO fyniq_api;
GRANT UPDATE (kind, name, institution, identifier, opened_on, closed_on, changed_by, changed_at, version) ON fyniq.money_account TO fyniq_api;
GRANT INSERT (name, institution, format, spec, created_by, created_at, changed_by, changed_at) ON fyniq.statement_mapping TO fyniq_api;
GRANT UPDATE (name, institution, spec, changed_by, changed_at, version) ON fyniq.statement_mapping TO fyniq_api;
GRANT INSERT (money_account_id, kind, connector_id, senders, report_format, mapping_id, from_date, set_by, set_at)
  ON fyniq.money_account_source TO fyniq_api;
-- Statements come from a person's upload (the API) and from emailed reports and connector syncs (the worker).
GRANT INSERT (money_account_id, source, format, parser_version, period_from, period_to, currency, minor_units, opening_balance,
  closing_balance, line_count, file_ref, connector_id, imported_by, imported_at) ON fyniq.bank_statement TO fyniq_api, fyniq_worker;
GRANT INSERT (statement_id, money_account_id, position, booking_date, value_date, amount, currency, minor_units, reference,
  counterparty, description, running_balance, fingerprint, status, duplicate_of, status_by, status_at)
  ON fyniq.bank_line TO fyniq_api, fyniq_worker;
GRANT UPDATE (status, status_by, status_at, version) ON fyniq.bank_line TO fyniq_api, fyniq_worker;
GRANT INSERT (money_account_id, statement_id, after_date, before_date, currency, minor_units, expected_opening, found_opening, found_at)
  ON fyniq.bank_gap TO fyniq_api, fyniq_worker;
GRANT UPDATE (resolved_by, resolved_at, resolution) ON fyniq.bank_gap TO fyniq_api;

REVOKE ALL ON FUNCTION fyniq.money_account_guard(), fyniq.money_account_source_guard(), fyniq.statement_mapping_guard(),
  fyniq.bank_statement_guard(), fyniq.bank_line_guard(), fyniq.bank_gap_guard() FROM PUBLIC;
