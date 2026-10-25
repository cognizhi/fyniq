-- Bank rules, matches, transfers, pending signals and reconciliations (PRD-001 FR-BNK-05, FR-BNK-07, FR-BNK-09,
-- FR-BNK-10, FR-BNK-14, FR-BNK-16; p1-bank-import-reconcile task 2.2, design D5 to D10).
--
-- A rule codes lines in its order; it auto-applies only under an approved request (FR-BNK-14). A match is what a line
-- settled or became: a category posted, a receipt or bill payment made from it, one recorded by hand, a transfer, or
-- suspense; a line has one live match, made by a person or by the system principal (an exact match, both sides of a
-- transfer, or an auto-apply rule), and undone once, only while its period is open. A transfer is one record with a
-- leg on each account, in transit until both lines are known. A pending signal (an incoming-funds alert) posts nothing
-- and closes or expires once. A reconciliation is prepared and then signed off by another person, and stays as signed.
-- Every table is a tenant table under FORCE RLS, audited in the same transaction.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- ------------------------------------------------------------------ rules (FR-BNK-05)
CREATE TABLE fyniq.bank_rule (
  organisation_id        uuid        NOT NULL DEFAULT fyniq.current_org(),
  id                     uuid        NOT NULL DEFAULT uuidv7(),
  name                   text        NOT NULL,
  position               integer     NOT NULL,
  conditions             jsonb       NOT NULL,
  action                 jsonb       NOT NULL,
  active                 boolean     NOT NULL DEFAULT true,
  from_ai_proposal       boolean     NOT NULL DEFAULT false,
  auto_apply             boolean     NOT NULL DEFAULT false,
  auto_apply_approval_id uuid,
  created_by             text        NOT NULL,
  created_at             timestamptz NOT NULL,
  changed_by             text        NOT NULL,
  changed_at             timestamptz NOT NULL,
  version                integer     NOT NULL DEFAULT 1,
  CONSTRAINT bank_rule_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT bank_rule_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT bank_rule_approval_fkey FOREIGN KEY (organisation_id, auto_apply_approval_id)
    REFERENCES fyniq.approval_request (organisation_id, id),
  CONSTRAINT bank_rule_name_length CHECK (length(btrim(name)) BETWEEN 1 AND 120),
  CONSTRAINT bank_rule_position_positive CHECK (position BETWEEN 1 AND 10000),
  CONSTRAINT bank_rule_conditions_object CHECK (jsonb_typeof(conditions) = 'object' AND length(conditions::text) <= 8192),
  CONSTRAINT bank_rule_action_object CHECK (jsonb_typeof(action) = 'object' AND length(action::text) <= 8192),
  CONSTRAINT bank_rule_auto_apply_approved CHECK (auto_apply <= (auto_apply_approval_id IS NOT NULL)),
  CONSTRAINT bank_rule_by_form CHECK (created_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$' AND changed_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$'),
  CONSTRAINT bank_rule_version_positive CHECK (version >= 1)
);

-- ------------------------------------------------------------------ transfers (FR-BNK-09)
CREATE TABLE fyniq.bank_transfer (
  organisation_id      uuid          NOT NULL DEFAULT fyniq.current_org(),
  id                   uuid          NOT NULL DEFAULT uuidv7(),
  from_account_id      uuid          NOT NULL,
  to_account_id        uuid          NOT NULL,
  from_line_id         uuid,
  to_line_id           uuid,
  on_date              date          NOT NULL,
  from_currency        text          NOT NULL,
  from_minor_units     smallint      NOT NULL,
  from_amount          numeric(20,4) NOT NULL,
  to_currency          text          NOT NULL,
  to_minor_units       smallint      NOT NULL,
  to_amount            numeric(20,4) NOT NULL,
  fee                  numeric(20,4),
  entry_id             uuid,
  made_by              text          NOT NULL,
  made_at              timestamptz   NOT NULL,
  version              integer       NOT NULL DEFAULT 1,
  CONSTRAINT bank_transfer_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT bank_transfer_from_fkey FOREIGN KEY (organisation_id, from_account_id) REFERENCES fyniq.money_account (organisation_id, id),
  CONSTRAINT bank_transfer_to_fkey FOREIGN KEY (organisation_id, to_account_id) REFERENCES fyniq.money_account (organisation_id, id),
  CONSTRAINT bank_transfer_from_line_fkey FOREIGN KEY (organisation_id, from_line_id) REFERENCES fyniq.bank_line (organisation_id, id),
  CONSTRAINT bank_transfer_to_line_fkey FOREIGN KEY (organisation_id, to_line_id) REFERENCES fyniq.bank_line (organisation_id, id),
  CONSTRAINT bank_transfer_entry_fkey FOREIGN KEY (organisation_id, entry_id) REFERENCES fyniq.journal_entry (organisation_id, id),
  CONSTRAINT bank_transfer_from_currency_fkey FOREIGN KEY (from_currency, from_minor_units) REFERENCES fyniq.currency (code, minor_units),
  CONSTRAINT bank_transfer_to_currency_fkey FOREIGN KEY (to_currency, to_minor_units) REFERENCES fyniq.currency (code, minor_units),
  CONSTRAINT bank_transfer_two_accounts CHECK (from_account_id <> to_account_id),
  CONSTRAINT bank_transfer_a_line CHECK (from_line_id IS NOT NULL OR to_line_id IS NOT NULL),
  CONSTRAINT bank_transfer_amounts_scale CHECK (from_amount <> 'NaN' AND from_amount > 0 AND from_amount = trunc(from_amount, from_minor_units)
    AND to_amount <> 'NaN' AND to_amount > 0 AND to_amount = trunc(to_amount, to_minor_units)),
  CONSTRAINT bank_transfer_fee_scale CHECK (fee <> 'NaN' AND fee >= 0 AND fee = trunc(fee, from_minor_units)),
  CONSTRAINT bank_transfer_by_form CHECK (made_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$'),
  CONSTRAINT bank_transfer_version_positive CHECK (version >= 1)
);
CREATE UNIQUE INDEX bank_transfer_from_line ON fyniq.bank_transfer (organisation_id, from_line_id) WHERE from_line_id IS NOT NULL;
CREATE UNIQUE INDEX bank_transfer_to_line ON fyniq.bank_transfer (organisation_id, to_line_id) WHERE to_line_id IS NOT NULL;
CREATE INDEX bank_transfer_in_transit ON fyniq.bank_transfer (organisation_id, to_account_id)
  WHERE from_line_id IS NULL OR to_line_id IS NULL;

-- ------------------------------------------------------------------ matches (FR-BNK-07, FR-BNK-14)
CREATE TABLE fyniq.bank_match (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  line_id         uuid        NOT NULL,
  kind            text        NOT NULL,
  document_id     uuid,
  entry_id        uuid,
  transfer_id     uuid,
  basis           text        NOT NULL,
  rule_id         uuid,
  made_by         text        NOT NULL,
  made_at         timestamptz NOT NULL,
  undone_by       text,
  undone_at       timestamptz,
  undo_reason     text,
  CONSTRAINT bank_match_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT bank_match_line_fkey FOREIGN KEY (organisation_id, line_id) REFERENCES fyniq.bank_line (organisation_id, id),
  CONSTRAINT bank_match_document_fkey FOREIGN KEY (organisation_id, document_id) REFERENCES fyniq.document (organisation_id, id),
  CONSTRAINT bank_match_entry_fkey FOREIGN KEY (organisation_id, entry_id) REFERENCES fyniq.journal_entry (organisation_id, id),
  CONSTRAINT bank_match_transfer_fkey FOREIGN KEY (organisation_id, transfer_id) REFERENCES fyniq.bank_transfer (organisation_id, id),
  CONSTRAINT bank_match_rule_fkey FOREIGN KEY (organisation_id, rule_id) REFERENCES fyniq.bank_rule (organisation_id, id),
  -- categorised: a category posted from the line; receipt / bill_payment: made from the line; recorded: one entered by
  -- hand that the line matches; transfer; suspense: posted to 2999 until classified.
  CONSTRAINT bank_match_kind_known CHECK (kind IN ('categorised', 'receipt', 'bill_payment', 'recorded', 'transfer', 'suspense')),
  CONSTRAINT bank_match_kind_refs CHECK (CASE kind
    WHEN 'categorised' THEN entry_id IS NOT NULL AND document_id IS NULL AND transfer_id IS NULL
    WHEN 'suspense' THEN entry_id IS NOT NULL AND document_id IS NULL AND transfer_id IS NULL
    WHEN 'receipt' THEN document_id IS NOT NULL AND transfer_id IS NULL
    WHEN 'bill_payment' THEN document_id IS NOT NULL AND transfer_id IS NULL
    WHEN 'recorded' THEN document_id IS NOT NULL AND entry_id IS NULL AND transfer_id IS NULL
    ELSE transfer_id IS NOT NULL AND document_id IS NULL END),
  -- person: a person matched it; exact: the one open document of its amount, currency, reference and contact;
  -- transfer: both sides of a transfer; rule: an approved auto-apply rule (FR-BNK-14).
  CONSTRAINT bank_match_basis_known CHECK (basis IN ('person', 'exact', 'transfer', 'rule')),
  CONSTRAINT bank_match_rule_basis CHECK ((basis = 'rule') = (rule_id IS NOT NULL)),
  CONSTRAINT bank_match_undone_whole CHECK ((undone_by IS NULL) = (undone_at IS NULL) AND (undone_by IS NULL) = (undo_reason IS NULL)),
  CONSTRAINT bank_match_reason_length CHECK (length(btrim(undo_reason)) BETWEEN 1 AND 500),
  CONSTRAINT bank_match_by_form CHECK (made_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$' AND undone_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);
-- A line has one live match; a document recorded by hand is matched by one line at a time.
CREATE UNIQUE INDEX bank_match_live_line ON fyniq.bank_match (organisation_id, line_id) WHERE undone_at IS NULL;
CREATE UNIQUE INDEX bank_match_live_recorded ON fyniq.bank_match (organisation_id, document_id) WHERE undone_at IS NULL AND kind = 'recorded';
CREATE INDEX bank_match_system ON fyniq.bank_match (organisation_id, made_at) WHERE basis <> 'person';

-- ------------------------------------------------------------------ pending signals (FR-BNK-16, UC-BNK-02)
CREATE TABLE fyniq.bank_signal (
  organisation_id    uuid          NOT NULL DEFAULT fyniq.current_org(),
  id                 uuid          NOT NULL DEFAULT uuidv7(),
  money_account_id   uuid          NOT NULL,
  amount             numeric(20,4) NOT NULL,
  currency           text          NOT NULL,
  minor_units        smallint      NOT NULL,
  payer              text,
  reference          text,
  received_at        timestamptz   NOT NULL,
  inbound_message_id uuid,
  invoice_id         uuid,
  status             text          NOT NULL DEFAULT 'pending',
  closed_line_id     uuid,
  closed_at          timestamptz,
  CONSTRAINT bank_signal_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT bank_signal_account_fkey FOREIGN KEY (organisation_id, money_account_id) REFERENCES fyniq.money_account (organisation_id, id),
  CONSTRAINT bank_signal_message_fkey FOREIGN KEY (organisation_id, inbound_message_id) REFERENCES fyniq.inbound_message (organisation_id, id),
  CONSTRAINT bank_signal_invoice_fkey FOREIGN KEY (organisation_id, invoice_id) REFERENCES fyniq.document (organisation_id, id),
  CONSTRAINT bank_signal_line_fkey FOREIGN KEY (organisation_id, closed_line_id) REFERENCES fyniq.bank_line (organisation_id, id),
  CONSTRAINT bank_signal_currency_fkey FOREIGN KEY (currency, minor_units) REFERENCES fyniq.currency (code, minor_units),
  CONSTRAINT bank_signal_amount_scale CHECK (amount <> 'NaN' AND amount > 0 AND amount = trunc(amount, minor_units)),
  CONSTRAINT bank_signal_payer_length CHECK (length(payer) BETWEEN 1 AND 200),
  CONSTRAINT bank_signal_reference_length CHECK (length(reference) BETWEEN 1 AND 140),
  CONSTRAINT bank_signal_status_known CHECK (status IN ('pending', 'closed', 'expired')),
  CONSTRAINT bank_signal_closed_whole CHECK ((status = 'pending') = (closed_at IS NULL) AND (closed_line_id IS NULL OR status = 'closed'))
);
CREATE INDEX bank_signal_pending ON fyniq.bank_signal (organisation_id, invoice_id) WHERE status = 'pending';

-- ------------------------------------------------------------------ reconciliations (FR-BNK-10)
CREATE TABLE fyniq.bank_reconciliation (
  organisation_id    uuid          NOT NULL DEFAULT fyniq.current_org(),
  id                 uuid          NOT NULL DEFAULT uuidv7(),
  money_account_id   uuid          NOT NULL,
  period_id          uuid          NOT NULL,
  currency           text          NOT NULL,
  minor_units        smallint      NOT NULL,
  statement_balance  numeric(20,4) NOT NULL,
  book_balance       numeric(20,4) NOT NULL,
  unreconciled_total numeric(20,4) NOT NULL,
  unreconciled_count integer       NOT NULL,
  difference         numeric(20,4) NOT NULL,
  ledger_revision    bigint        NOT NULL,
  prepared_by        text          NOT NULL,
  prepared_at        timestamptz   NOT NULL,
  signed_by          text,
  signed_at          timestamptz,
  signer_mfa_at      timestamptz,
  version            integer       NOT NULL DEFAULT 1,
  CONSTRAINT bank_reconciliation_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT bank_reconciliation_account_fkey FOREIGN KEY (organisation_id, money_account_id)
    REFERENCES fyniq.money_account (organisation_id, id),
  CONSTRAINT bank_reconciliation_period_fkey FOREIGN KEY (organisation_id, period_id) REFERENCES fyniq.period (organisation_id, id),
  CONSTRAINT bank_reconciliation_currency_fkey FOREIGN KEY (currency, minor_units) REFERENCES fyniq.currency (code, minor_units),
  CONSTRAINT bank_reconciliation_period_key UNIQUE (organisation_id, money_account_id, period_id),
  CONSTRAINT bank_reconciliation_amounts_scale CHECK (statement_balance <> 'NaN' AND statement_balance = trunc(statement_balance, minor_units)
    AND book_balance <> 'NaN' AND book_balance = trunc(book_balance, minor_units)
    AND unreconciled_total <> 'NaN' AND unreconciled_total = trunc(unreconciled_total, minor_units)
    AND difference <> 'NaN' AND difference = trunc(difference, minor_units)),
  -- The difference is what the unreconciled items do not explain (FR-BNK-10).
  CONSTRAINT bank_reconciliation_difference CHECK (difference = statement_balance - book_balance - unreconciled_total),
  CONSTRAINT bank_reconciliation_count CHECK (unreconciled_count >= 0),
  CONSTRAINT bank_reconciliation_revision CHECK (ledger_revision >= 0),
  CONSTRAINT bank_reconciliation_signed_whole CHECK ((signed_by IS NULL) = (signed_at IS NULL) AND (signed_by IS NULL) = (signer_mfa_at IS NULL)),
  CONSTRAINT bank_reconciliation_by_form CHECK (prepared_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$' AND signed_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$'),
  CONSTRAINT bank_reconciliation_version_positive CHECK (version >= 1)
);

-- ------------------------------------------------------------------ guards
-- +goose StatementBegin
-- A rule changes one version at a time and is never deleted (deactivate it instead); it auto-applies only under an
-- approved request for its own key (FY222, FR-BNK-14).
CREATE FUNCTION fyniq.bank_rule_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'Bank rule % is never deleted', OLD.id USING ERRCODE = 'FY222', HINT = 'Deactivate it instead: its matches name it.';
  END IF;
  IF TG_OP = 'UPDATE' AND (NEW.version <> OLD.version + 1 OR NEW.created_by IS DISTINCT FROM OLD.created_by
                           OR NEW.created_at IS DISTINCT FROM OLD.created_at OR NEW.from_ai_proposal IS DISTINCT FROM OLD.from_ai_proposal) THEN
    RAISE EXCEPTION 'Bank rule % changes one version at a time and keeps its origin', OLD.id USING ERRCODE = 'FY222';
  END IF;
  IF TG_OP = 'INSERT' AND (NEW.version <> 1 OR NEW.auto_apply) THEN
    RAISE EXCEPTION 'A new bank rule starts at version 1 without auto-apply' USING ERRCODE = 'FY222',
      HINT = 'Ask for auto-apply once the rule exists: an approver decides it with the back-test (FR-BNK-14).';
  END IF;
  IF NEW.auto_apply AND (TG_OP = 'INSERT' OR NOT OLD.auto_apply OR NEW.auto_apply_approval_id IS DISTINCT FROM OLD.auto_apply_approval_id) THEN
    IF NOT EXISTS (SELECT 1 FROM fyniq.approval_request r WHERE r.organisation_id = NEW.organisation_id AND r.id = NEW.auto_apply_approval_id
                     AND r.object_type = 'bank_rule' AND r.object_key = NEW.id::text AND r.status = 'approved') THEN
      RAISE EXCEPTION 'Bank rule % auto-applies only under its approved request', NEW.id USING ERRCODE = 'FY222';
    END IF;
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER bank_rule_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.bank_rule
  FOR EACH ROW EXECUTE FUNCTION fyniq.bank_rule_guard();

-- +goose StatementBegin
-- A transfer keeps its accounts, date and amounts; a missing side's line is added once, on the right account in the
-- right currency, and never changed; its entry is set once; it is never deleted (FY223).
CREATE FUNCTION fyniq.bank_transfer_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  l fyniq.bank_line;
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'Transfer % is never deleted', OLD.id USING ERRCODE = 'FY223', HINT = 'Reverse its entry and undo its matches instead.';
  END IF;
  IF TG_OP = 'UPDATE' THEN
    IF (NEW.from_account_id, NEW.to_account_id, NEW.on_date, NEW.from_currency, NEW.from_amount, NEW.to_currency, NEW.to_amount, NEW.fee,
        NEW.made_by, NEW.made_at)
       IS DISTINCT FROM (OLD.from_account_id, OLD.to_account_id, OLD.on_date, OLD.from_currency, OLD.from_amount, OLD.to_currency,
        OLD.to_amount, OLD.fee, OLD.made_by, OLD.made_at)
       OR (OLD.from_line_id IS NOT NULL AND NEW.from_line_id IS DISTINCT FROM OLD.from_line_id)
       OR (OLD.to_line_id IS NOT NULL AND NEW.to_line_id IS DISTINCT FROM OLD.to_line_id)
       OR (OLD.entry_id IS NOT NULL AND NEW.entry_id IS DISTINCT FROM OLD.entry_id)
       OR NEW.version <> OLD.version + 1 THEN
      RAISE EXCEPTION 'Transfer % keeps what was recorded; only a missing side and its entry are added', OLD.id USING ERRCODE = 'FY223';
    END IF;
  END IF;
  IF NEW.from_line_id IS NOT NULL AND (TG_OP = 'INSERT' OR OLD.from_line_id IS NULL) THEN
    SELECT * INTO l FROM fyniq.bank_line WHERE organisation_id = NEW.organisation_id AND id = NEW.from_line_id;
    IF l.money_account_id <> NEW.from_account_id OR l.currency <> NEW.from_currency OR l.amount <> -NEW.from_amount THEN
      RAISE EXCEPTION 'The paying line of transfer % is not its money out', NEW.id USING ERRCODE = 'FY223';
    END IF;
  END IF;
  IF NEW.to_line_id IS NOT NULL AND (TG_OP = 'INSERT' OR OLD.to_line_id IS NULL) THEN
    SELECT * INTO l FROM fyniq.bank_line WHERE organisation_id = NEW.organisation_id AND id = NEW.to_line_id;
    IF l.money_account_id <> NEW.to_account_id OR l.currency <> NEW.to_currency OR l.amount <> NEW.to_amount THEN
      RAISE EXCEPTION 'The receiving line of transfer % is not its money in', NEW.id USING ERRCODE = 'FY223';
    END IF;
  END IF;
  IF TG_OP = 'INSERT' AND NEW.version <> 1 THEN
    RAISE EXCEPTION 'A new transfer starts at version 1' USING ERRCODE = 'FY223';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER bank_transfer_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.bank_transfer
  FOR EACH ROW EXECUTE FUNCTION fyniq.bank_transfer_guard();

-- +goose StatementBegin
-- A match keeps what it matched and is undone once, with a reason, only while the line's period is open; it is never
-- deleted (FY224). A match by a rule names an approved auto-apply rule.
CREATE FUNCTION fyniq.bank_match_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  closed boolean;
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A match is never deleted' USING ERRCODE = 'FY224', HINT = 'Undo it with a reason instead (FR-BNK-14).';
  END IF;
  IF TG_OP = 'INSERT' THEN
    IF NEW.undone_at IS NOT NULL THEN
      RAISE EXCEPTION 'A match is made live' USING ERRCODE = 'FY224';
    END IF;
    IF NEW.basis = 'rule' AND NOT EXISTS (SELECT 1 FROM fyniq.bank_rule r WHERE r.organisation_id = NEW.organisation_id
                                          AND r.id = NEW.rule_id AND r.auto_apply AND r.active) THEN
      RAISE EXCEPTION 'Rule % does not auto-apply', NEW.rule_id USING ERRCODE = 'FY224',
        HINT = 'An approver marks a rule auto-apply first (FR-BNK-14).';
    END IF;
    RETURN NEW;
  END IF;
  IF OLD.undone_at IS NOT NULL
     OR (NEW.line_id, NEW.kind, NEW.document_id, NEW.entry_id, NEW.transfer_id, NEW.basis, NEW.rule_id, NEW.made_by, NEW.made_at)
        IS DISTINCT FROM (OLD.line_id, OLD.kind, OLD.document_id, OLD.entry_id, OLD.transfer_id, OLD.basis, OLD.rule_id,
        OLD.made_by, OLD.made_at) THEN
    RAISE EXCEPTION 'Match % keeps what it matched and is undone once', OLD.id USING ERRCODE = 'FY224';
  END IF;
  SELECT p.status = 'closed' INTO closed FROM fyniq.period p JOIN fyniq.bank_line l ON l.organisation_id = p.organisation_id
   WHERE l.organisation_id = NEW.organisation_id AND l.id = NEW.line_id AND l.booking_date BETWEEN p.starts_on AND p.ends_on;
  IF coalesce(closed, false) THEN
    RAISE EXCEPTION 'Match % is of a line in a closed period', OLD.id USING ERRCODE = 'FY224',
      HINT = 'Undo only while the period is open (FR-BNK-14); reopen it first.';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER bank_match_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.bank_match
  FOR EACH ROW EXECUTE FUNCTION fyniq.bank_match_guard();

-- +goose StatementBegin
-- A signal keeps what the alert said, closes or expires once, and is never deleted (FY225): it posts nothing (UC-BNK-02).
CREATE FUNCTION fyniq.bank_signal_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A signal is never deleted' USING ERRCODE = 'FY225';
  END IF;
  IF TG_OP = 'INSERT' THEN
    IF NEW.status <> 'pending' THEN
      RAISE EXCEPTION 'A signal starts pending' USING ERRCODE = 'FY225';
    END IF;
    RETURN NEW;
  END IF;
  IF OLD.status <> 'pending'
     OR (NEW.money_account_id, NEW.amount, NEW.currency, NEW.minor_units, NEW.payer, NEW.reference, NEW.received_at, NEW.inbound_message_id)
        IS DISTINCT FROM (OLD.money_account_id, OLD.amount, OLD.currency, OLD.minor_units, OLD.payer, OLD.reference, OLD.received_at,
        OLD.inbound_message_id) THEN
    RAISE EXCEPTION 'Signal % keeps what the alert said and closes once', OLD.id USING ERRCODE = 'FY225';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER bank_signal_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.bank_signal
  FOR EACH ROW EXECUTE FUNCTION fyniq.bank_signal_guard();

-- +goose StatementBegin
-- A reconciliation is prepared, re-prepared while unsigned, and signed once by another person; once signed, or once its
-- period is closed, it never changes; it is never deleted (FY226; FR-BNK-10, FM-5).
CREATE FUNCTION fyniq.bank_reconciliation_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  closed boolean;
  cur text;
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A reconciliation is never deleted' USING ERRCODE = 'FY226';
  END IF;
  SELECT status = 'closed' INTO closed FROM fyniq.period WHERE organisation_id = NEW.organisation_id AND id = NEW.period_id;
  IF coalesce(closed, false) THEN
    RAISE EXCEPTION 'The reconciliation of a closed period does not change' USING ERRCODE = 'FY226',
      HINT = 'It is locked with its period (FR-BNK-10).';
  END IF;
  IF TG_OP = 'INSERT' THEN
    SELECT currency INTO cur FROM fyniq.money_account WHERE organisation_id = NEW.organisation_id AND id = NEW.money_account_id;
    IF NEW.currency <> cur OR NEW.signed_by IS NOT NULL OR NEW.version <> 1 THEN
      RAISE EXCEPTION 'A reconciliation starts unsigned, at version 1, in its account''s currency' USING ERRCODE = 'FY226';
    END IF;
    RETURN NEW;
  END IF;
  IF OLD.signed_by IS NOT NULL OR NEW.version <> OLD.version + 1
     OR (NEW.money_account_id, NEW.period_id, NEW.currency) IS DISTINCT FROM (OLD.money_account_id, OLD.period_id, OLD.currency) THEN
    RAISE EXCEPTION 'Reconciliation % is signed, or changes one version at a time for its account and period', OLD.id USING ERRCODE = 'FY226';
  END IF;
  IF NEW.signed_by IS NOT NULL THEN
    IF NEW.signed_by = NEW.prepared_by THEN
      RAISE EXCEPTION 'The preparer of reconciliation % cannot sign it off', OLD.id USING ERRCODE = 'FY226',
        HINT = 'Another person with the sign-off right signs it (FM-5, SOD-5).';
    END IF;
    IF (NEW.statement_balance, NEW.book_balance, NEW.unreconciled_total, NEW.unreconciled_count, NEW.difference, NEW.ledger_revision,
        NEW.prepared_by, NEW.prepared_at)
       IS DISTINCT FROM (OLD.statement_balance, OLD.book_balance, OLD.unreconciled_total, OLD.unreconciled_count, OLD.difference,
        OLD.ledger_revision, OLD.prepared_by, OLD.prepared_at) THEN
      RAISE EXCEPTION 'Reconciliation % is signed as it was prepared', OLD.id USING ERRCODE = 'FY226';
    END IF;
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER bank_reconciliation_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.bank_reconciliation
  FOR EACH ROW EXECUTE FUNCTION fyniq.bank_reconciliation_guard();

-- ------------------------------------------------------------------ tenancy
ALTER TABLE fyniq.bank_rule ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.bank_rule FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.bank_rule
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.bank_transfer ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.bank_transfer FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.bank_transfer
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.bank_match ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.bank_match FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.bank_match
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.bank_signal ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.bank_signal FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.bank_signal
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.bank_reconciliation ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.bank_reconciliation FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.bank_reconciliation
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

-- ------------------------------------------------------------------ audit (AUD-01)
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.bank_rule
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('bank_rule', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.bank_transfer
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('bank_transfer', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.bank_match
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('bank_line', 'line_id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.bank_signal
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('bank_signal', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.bank_reconciliation
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('bank_reconciliation', 'id');

-- ------------------------------------------------------------------ grants
REVOKE ALL ON fyniq.bank_rule, fyniq.bank_transfer, fyniq.bank_match, fyniq.bank_signal, fyniq.bank_reconciliation FROM PUBLIC;
GRANT SELECT ON fyniq.bank_rule, fyniq.bank_transfer, fyniq.bank_match, fyniq.bank_signal, fyniq.bank_reconciliation
  TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (name, position, conditions, action, active, from_ai_proposal, created_by, created_at, changed_by, changed_at)
  ON fyniq.bank_rule TO fyniq_api;
GRANT UPDATE (name, position, conditions, action, active, auto_apply, auto_apply_approval_id, changed_by, changed_at, version)
  ON fyniq.bank_rule TO fyniq_api;
-- People match and transfer through the API; the system principal auto-reconciles in the worker (FR-AUT-02).
GRANT INSERT (from_account_id, to_account_id, from_line_id, to_line_id, on_date, from_currency, from_minor_units, from_amount,
  to_currency, to_minor_units, to_amount, fee, entry_id, made_by, made_at) ON fyniq.bank_transfer TO fyniq_api, fyniq_worker;
GRANT UPDATE (from_line_id, to_line_id, entry_id, version) ON fyniq.bank_transfer TO fyniq_api, fyniq_worker;
GRANT INSERT (line_id, kind, document_id, entry_id, transfer_id, basis, rule_id, made_by, made_at) ON fyniq.bank_match TO fyniq_api, fyniq_worker;
GRANT UPDATE (undone_by, undone_at, undo_reason) ON fyniq.bank_match TO fyniq_api;
GRANT INSERT (money_account_id, amount, currency, minor_units, payer, reference, received_at, inbound_message_id, invoice_id)
  ON fyniq.bank_signal TO fyniq_worker;
GRANT UPDATE (invoice_id, status, closed_line_id, closed_at) ON fyniq.bank_signal TO fyniq_api, fyniq_worker;
GRANT INSERT (money_account_id, period_id, currency, minor_units, statement_balance, book_balance, unreconciled_total,
  unreconciled_count, difference, ledger_revision, prepared_by, prepared_at) ON fyniq.bank_reconciliation TO fyniq_api;
GRANT UPDATE (statement_balance, book_balance, unreconciled_total, unreconciled_count, difference, ledger_revision, prepared_by,
  prepared_at, signed_by, signed_at, signer_mfa_at, version) ON fyniq.bank_reconciliation TO fyniq_api;

REVOKE ALL ON FUNCTION fyniq.bank_rule_guard(), fyniq.bank_transfer_guard(), fyniq.bank_match_guard(), fyniq.bank_signal_guard(),
  fyniq.bank_reconciliation_guard() FROM PUBLIC;
