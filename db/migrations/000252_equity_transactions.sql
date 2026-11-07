-- Equity transactions and the holdings behind every share (PRD-001 FR-EQ-17, INV-13, B.7; rev 0.205; p1-equity-register
-- slice 2, tasks 3.1 and 3.2, design D3 to D5).
--
-- An equity transaction is an equity_transaction document, numbered EQ- on its approval: a cash issue to one subscriber,
-- a transfer between two holders, a conversion of one holder's shares between classes, or the reversal of one of these.
-- Made effective with ACRA's date, it writes its holding movements, and a cash issue its share issue and entry, in one
-- transaction. What a holder holds is the sum of their movements, by account and class. The database is the backstop:
--   - a movement is written once and never changed, deleted or truncated; it names an approved equity transaction made
--     effective in this transaction, or a share issue (FY288);
--   - an issue's movement equals its share issue, and every share issue has its movement (FY288);
--   - a transfer's two movements net to zero between two holders; a conversion moves one holder's shares out of one
--     class into another; a reversal negates every movement of one transaction, once (FY288);
--   - no holding goes below zero on any day, and a movement other than an issue falls in an open period (FY288, FY030,
--     FY003);
--   - share capital takes postings from the register of share issues, the opening balances or a conversion between
--     classes whose accounts differ, which commits only with its movements (FY007, FY289; INV-13, rev 0.205);
--   - 2040 Share application monies (the role share-application-monies) names its subscriber, a shareholder or an
--     investor, on every line, as a director's account names its person (FY250, FY249; B.7).
-- Shares an issue allotted with no class, before its account had a recorded class, count in the account's first
-- recorded class once it has one (design D2): fyniq.share_class_of. Existing share issues are given their movements
-- below. The widened checks are added NOT VALID and validated by 000253 (squawk: constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.document DROP CONSTRAINT document_type_known;
ALTER TABLE fyniq.document ADD CONSTRAINT document_type_known
  CHECK (type IN ('journal', 'opening_balance', 'fx_revaluation', 'bill', 'bill_payment', 'vendor_credit', 'credit_application',
                  'supplier_refund', 'purchase_order', 'invoice', 'customer_receipt', 'write_off', 'credit_note',
                  'customer_credit_application', 'customer_refund', 'customer_deposit', 'quote', 'sales_order', 'statement',
                  'bank_transaction', 'bank_agreement', 'expense_claim', 'claim_reimbursement', 'cash_advance', 'fixed_asset',
                  'asset_disposal', 'payee_bank_change', 'claimant_bank_change', 'payment_batch',
                  'pre_incorporation_cost', 'director_pay_choice', 'director_fee', 'exempt_status',
                  'director_confirmation', 'director_settlement', 'director_repayment', 'account_merge',
                  'owner_succession', 'holder_record', 'share_class', 'equity_transaction')) NOT VALID;

ALTER TABLE fyniq.document_sequence DROP CONSTRAINT document_sequence_type_known;
ALTER TABLE fyniq.document_sequence ADD CONSTRAINT document_sequence_type_known
  CHECK (document_type IN ('journal', 'bill', 'bill_payment', 'vendor_credit', 'supplier_refund', 'purchase_order', 'invoice',
                           'customer_receipt', 'write_off', 'credit_note', 'customer_refund', 'customer_deposit', 'quote',
                           'sales_order', 'statement', 'expense_claim', 'claim_reimbursement', 'cash_advance', 'fixed_asset',
                           'asset_disposal', 'payment_batch', 'pre_incorporation_cost', 'director_fee',
                           'director_settlement', 'director_repayment', 'equity_transaction')) NOT VALID;

ALTER TABLE fyniq.approval_request DROP CONSTRAINT approval_request_object_known;
ALTER TABLE fyniq.approval_request ADD CONSTRAINT approval_request_object_known CHECK (
  (object_type, operation) IN (('organisation_setting', 'organisation_setting.change'), ('membership', 'role.assign'),
                               ('membership', 'role.revoke'), ('membership', 'member.remove'),
                               ('member_invitation', 'member_invitation.create'),
                               ('approval_policy', 'approval_policy.change'), ('statutory_pack', 'statutory_pack.activate'),
                               ('team_mode', 'team_mode.change'), ('period', 'period.reopen'),
                               ('journal', 'journal.post'), ('journal', 'journal.reverse'), ('journal', 'journal.edit'),
                               ('opening_balance', 'opening_balance.edit'),
                               ('bill', 'bill.post'), ('bill', 'bill.void'), ('bill', 'bill.edit'),
                               ('bill_payment', 'bill_payment.void'),
                               ('credit_application', 'credit_application.void'),
                               ('supplier_refund', 'supplier_refund.void'),
                               ('purchase_order', 'purchase_order.approve'),
                               ('document_template', 'document_template.publish'),
                               ('invoice', 'invoice.post'), ('invoice', 'invoice.void'), ('invoice', 'invoice.edit'),
                               ('customer_receipt', 'customer_receipt.void'),
                               ('write_off', 'write_off.post'), ('write_off', 'write_off.void'),
                               ('customer_credit_application', 'customer_credit_application.void'),
                               ('customer_refund', 'customer_refund.void'),
                               ('customer_deposit', 'customer_deposit.void'),
                               ('recurring_invoice', 'recurring_invoice.approve'),
                               ('connector', 'connector.change'), ('bank_rule', 'bank_rule.auto_apply'),
                               ('expense_claim', 'expense_claim.post'), ('claim_reimbursement', 'claim_reimbursement.void'),
                               ('cash_advance', 'cash_advance.void'), ('asset_disposal', 'asset_disposal.void'),
                               ('fixed_asset', 'fixed_asset.void'),
                               ('payee_bank_change', 'payee_bank_change.approve'),
                               ('claimant_bank_change', 'claimant_bank_change.approve'),
                               ('payment_batch', 'payment_batch.approve'), ('payment_batch', 'payment_batch.reapprove'),
                               ('contact_member_link', 'contact_member_link.change'),
                               ('director_fee', 'director_fee.post'),
                               ('director_settlement', 'director_settlement.approve'),
                               ('director_repayment', 'director_repayment.void'),
                               ('account', 'account.merge'),
                               ('bank_reconciliation', 'bank_reconciliation.sign'),
                               ('holder_record', 'holder_record.record'), ('share_class', 'share_class.record'),
                               ('equity_transaction', 'equity_transaction.approve'))) NOT VALID;
ALTER TABLE fyniq.approval_policy DROP CONSTRAINT approval_policy_object_known;
ALTER TABLE fyniq.approval_policy ADD CONSTRAINT approval_policy_object_known CHECK (
  object_type IN ('organisation_setting', 'membership', 'member_invitation', 'statutory_pack', 'team_mode', 'period', 'journal',
                  'opening_balance', 'bill', 'bill_payment', 'credit_application', 'supplier_refund', 'purchase_order',
                  'document_template', 'invoice', 'customer_receipt', 'write_off', 'customer_credit_application',
                  'customer_refund', 'customer_deposit', 'recurring_invoice', 'connector', 'bank_rule', 'expense_claim',
                  'claim_reimbursement', 'cash_advance', 'asset_disposal', 'fixed_asset',
                  'payee_bank_change', 'claimant_bank_change', 'payment_batch', 'contact_member_link',
                  'director_fee', 'director_settlement', 'director_repayment', 'account', 'bank_reconciliation',
                  'holder_record', 'share_class', 'equity_transaction')) NOT VALID;

ALTER TABLE fyniq.account DROP CONSTRAINT account_role_known;
ALTER TABLE fyniq.account ADD CONSTRAINT account_role_known CHECK (role IN ('accounts-receivable', 'accounts-payable', 'gst-control',
  'fx-rounding', 'unrealised-fx', 'deferred-revenue', 'prepayments', 'salaries-payable', 'cpf-sdl-payable', 'employer-cpf', 'sdl',
  'depreciation', 'accumulated-depreciation', 'suspense', 'retained-earnings', 'expense-claims-payable', 'income-tax-payable',
  'income-tax-expense', 'director-loan', 'deferred-remuneration', 'share-capital', 'capital-contribution',
  'withholding-tax-payable', 'customer-deposits', 'share-application-monies')) NOT VALID;

-- +goose StatementBegin
-- The class a movement's shares count in: the class it names, or, for shares allotted before their account had a
-- recorded class, the account's first recorded class (design D2, equity.DefaultClass); NULL while it has none.
CREATE FUNCTION fyniq.share_class_of(p_org uuid, p_account uuid, p_class uuid) RETURNS uuid
LANGUAGE sql STABLE
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT coalesce(p_class, (
    SELECT d.id FROM fyniq.document d
      JOIN fyniq.document_version v ON v.organisation_id = d.organisation_id AND v.document_id = d.id AND v.version = d.current_version
     WHERE d.organisation_id = p_org AND d.type = 'share_class' AND d.status = 'posted'
       AND v.payload->>'account_id' = p_account::text AND NOT v.payload ? 'class_id'
     ORDER BY d.posted_at, d.id
     LIMIT 1))
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION fyniq.share_class_of(uuid, uuid, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.share_class_of(uuid, uuid, uuid) TO fyniq_api, fyniq_worker, fyniq_readonly;

CREATE TABLE fyniq.holding_movement (
  organisation_id  uuid           NOT NULL DEFAULT fyniq.current_org(),
  id               uuid           NOT NULL DEFAULT uuidv7(),
  kind             text           NOT NULL,
  transaction_id   uuid,                     -- the equity transaction made effective; none for an issue recorded outside one
  share_issue_id   uuid,                     -- an issue's row in the register of share issues
  reverses_id      uuid,                     -- the movement a reversal negates
  holder_id        uuid           NOT NULL,
  class_account_id uuid           NOT NULL,  -- the class's share-capital account
  class_id         uuid,                     -- the share class, by its first version's document
  shares           numeric(16, 0) NOT NULL,  -- signed: shares in (+) or out (-)
  effective_on     date           NOT NULL,  -- the day ACRA's register of members shows it
  recorded_by      uuid           NOT NULL,
  recorded_at      timestamptz    NOT NULL,
  created_xact     bigint         NOT NULL DEFAULT pg_current_xact_id()::text::bigint,
  CONSTRAINT holding_movement_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT holding_movement_transaction_fkey FOREIGN KEY (organisation_id, transaction_id) REFERENCES fyniq.document (organisation_id, id),
  CONSTRAINT holding_movement_share_issue_fkey FOREIGN KEY (organisation_id, share_issue_id) REFERENCES fyniq.share_issue (organisation_id, id),
  CONSTRAINT holding_movement_reverses_fkey FOREIGN KEY (organisation_id, reverses_id) REFERENCES fyniq.holding_movement (organisation_id, id),
  CONSTRAINT holding_movement_holder_fkey FOREIGN KEY (organisation_id, holder_id) REFERENCES fyniq.contact (organisation_id, id),
  CONSTRAINT holding_movement_account_fkey FOREIGN KEY (organisation_id, class_account_id) REFERENCES fyniq.account (organisation_id, id),
  CONSTRAINT holding_movement_class_fkey FOREIGN KEY (organisation_id, class_id) REFERENCES fyniq.document (organisation_id, id),
  CONSTRAINT holding_movement_recorded_by_fkey FOREIGN KEY (recorded_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT holding_movement_issue_once UNIQUE (organisation_id, share_issue_id),
  CONSTRAINT holding_movement_reversed_once UNIQUE (organisation_id, reverses_id),
  CONSTRAINT holding_movement_kind_known CHECK (kind IN ('issue', 'transfer', 'class_conversion', 'reversal')),
  CONSTRAINT holding_movement_shares_nonzero CHECK (shares <> 'NaN' AND shares <> 0),
  CONSTRAINT holding_movement_kind_names CHECK (
    (kind = 'issue' AND share_issue_id IS NOT NULL AND reverses_id IS NULL AND shares > 0)
    OR (kind IN ('transfer', 'class_conversion') AND transaction_id IS NOT NULL AND share_issue_id IS NULL AND reverses_id IS NULL)
    OR (kind = 'reversal' AND transaction_id IS NOT NULL AND share_issue_id IS NULL AND reverses_id IS NOT NULL))
);
CREATE INDEX holding_movement_holder ON fyniq.holding_movement (organisation_id, holder_id, class_account_id, effective_on);
CREATE INDEX holding_movement_transaction ON fyniq.holding_movement (organisation_id, transaction_id);

-- +goose StatementBegin
-- A movement is written once and never changed, deleted or truncated. It names an approved equity transaction of its
-- kind that no earlier transaction made effective, or a share issue it equals; a reversal negates the movement it names,
-- of the transaction its document reverses; shares moved in go to a director or shareholder; a transfer's or
-- conversion's class is a recorded class of its account, named once the account has one; and a movement other than an
-- issue falls in an open period (FY288, FY030, FY003).
CREATE FUNCTION fyniq.holding_movement_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  d       record;
  s       fyniq.share_issue;
  r       fyniq.holding_movement;
  p       record;
  account text;
  status  text;
BEGIN
  IF TG_OP = 'TRUNCATE' THEN
    RAISE EXCEPTION 'Holding movements are never deleted' USING ERRCODE = 'FY288';
  END IF;
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'Holding movement % is never changed or deleted', OLD.id USING ERRCODE = 'FY288',
      HINT = 'Correct an equity transaction with a reversing one (FR-EQ-17).';
  END IF;
  IF NEW.shares = 'NaN' THEN -- as the check says, before the guard reads anything (§11.3)
    RAISE EXCEPTION 'A holding movement''s shares are a number' USING ERRCODE = 'check_violation';
  END IF;
  NEW.created_xact := pg_current_xact_id()::text::bigint;

  IF NEW.transaction_id IS NOT NULL THEN
    SELECT dd.status, v.payload INTO d
      FROM fyniq.document dd
      JOIN fyniq.document_version v ON v.organisation_id = dd.organisation_id AND v.document_id = dd.id AND v.version = dd.current_version
     WHERE dd.organisation_id = NEW.organisation_id AND dd.id = NEW.transaction_id AND dd.type = 'equity_transaction';
    IF NOT FOUND OR d.status <> 'posted'
       OR (NEW.kind = 'issue' AND d.payload->>'kind' <> 'issue_cash')
       OR (NEW.kind <> 'issue' AND d.payload->>'kind' <> NEW.kind) THEN
      RAISE EXCEPTION 'Holding movement % names no approved equity transaction of its kind', NEW.id USING ERRCODE = 'FY288',
        HINT = 'Make an approved equity transaction effective, which writes its movements (FR-EQ-17).';
    END IF;
    IF EXISTS (SELECT 1 FROM fyniq.holding_movement m
                WHERE m.organisation_id = NEW.organisation_id AND m.transaction_id = NEW.transaction_id
                  AND m.created_xact <> NEW.created_xact) THEN
      RAISE EXCEPTION 'Equity transaction % is effective already, so it takes no more movements', NEW.transaction_id
        USING ERRCODE = 'FY288', HINT = 'Correct it with a reversing transaction (FR-EQ-17).';
    END IF;
  END IF;

  IF NEW.kind = 'issue' THEN
    SELECT * INTO s FROM fyniq.share_issue x WHERE x.organisation_id = NEW.organisation_id AND x.id = NEW.share_issue_id;
    IF s.id IS NULL OR s.holder_id <> NEW.holder_id OR s.class_account_id <> NEW.class_account_id
       OR s.class_id IS DISTINCT FROM NEW.class_id OR s.shares <> NEW.shares OR s.effective_on <> NEW.effective_on THEN
      RAISE EXCEPTION 'Holding movement % does not equal the share issue it names', NEW.id USING ERRCODE = 'FY288',
        HINT = 'An issue''s movement is its holder, class, shares and effective date (FR-EQ-17).';
    END IF;
    RETURN NEW;
  END IF;
  IF NEW.transaction_id IS NULL THEN
    RAISE EXCEPTION 'Holding movement % names no equity transaction', NEW.id USING ERRCODE = 'FY288',
      HINT = 'Only an issue is recorded outside an equity transaction (FR-EQ-17).';
  END IF;

  IF NEW.kind = 'reversal' THEN
    SELECT * INTO r FROM fyniq.holding_movement x WHERE x.organisation_id = NEW.organisation_id AND x.id = NEW.reverses_id;
    IF r.id IS NULL OR r.kind = 'reversal' OR r.transaction_id IS NULL OR r.transaction_id::text IS DISTINCT FROM d.payload->>'reverses_id'
       OR r.holder_id <> NEW.holder_id OR r.class_account_id <> NEW.class_account_id OR r.class_id IS DISTINCT FROM NEW.class_id
       OR r.shares <> -NEW.shares OR NEW.effective_on < r.effective_on THEN
      RAISE EXCEPTION 'Holding movement % does not negate a movement of the transaction its reversal names', NEW.id
        USING ERRCODE = 'FY288', HINT = 'A reversal undoes every movement of one transaction, on or after its date (FR-EQ-17).';
    END IF;
  ELSE
    IF NOT EXISTS (SELECT 1 FROM fyniq.account a WHERE a.organisation_id = NEW.organisation_id AND a.id = NEW.class_account_id
                    AND a.role = 'share-capital' AND a.is_active AND NOT a.is_group) THEN
      RAISE EXCEPTION 'The class of holding movement % is not an active share-capital account', NEW.id USING ERRCODE = 'FY288',
        HINT = 'Move shares of 3010 Ordinary or 3020 Preference share capital (FR-EQ-17).';
    END IF;
    IF NEW.class_id IS NULL THEN
      IF fyniq.share_class_of(NEW.organisation_id, NEW.class_account_id, NULL) IS NOT NULL THEN
        RAISE EXCEPTION 'Holding movement % names no class, and its account has a recorded class', NEW.id USING ERRCODE = 'FY288',
          HINT = 'Name the class the shares move in (FR-EQ-16).';
      END IF;
    ELSE
      SELECT dd.status, v.payload->>'account_id' INTO status, account
        FROM fyniq.document dd
        JOIN fyniq.document_version v ON v.organisation_id = dd.organisation_id AND v.document_id = dd.id AND v.version = dd.current_version
       WHERE dd.organisation_id = NEW.organisation_id AND dd.id = NEW.class_id AND dd.type = 'share_class';
      IF status IS DISTINCT FROM 'posted' OR account IS DISTINCT FROM NEW.class_account_id::text THEN
        RAISE EXCEPTION 'The class of holding movement % is not a recorded class of its account', NEW.id USING ERRCODE = 'FY288';
      END IF;
    END IF;
  END IF;
  IF NEW.shares > 0 AND NEW.kind <> 'reversal'
     AND NOT EXISTS (SELECT 1 FROM fyniq.contact c WHERE c.organisation_id = NEW.organisation_id AND c.id = NEW.holder_id
                       AND (c.is_director OR c.is_shareholder)) THEN
    RAISE EXCEPTION 'The holder shares move to in holding movement % is not a director or shareholder', NEW.id USING ERRCODE = 'FY288',
      HINT = 'Mark the contact a shareholder first (FR-EQ-17).';
  END IF;
  SELECT x.status INTO p FROM fyniq.period x
   WHERE x.organisation_id = NEW.organisation_id AND daterange(x.starts_on, x.ends_on, '[]') @> NEW.effective_on;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'No period covers the effective date %', NEW.effective_on
      USING ERRCODE = 'FY030', HINT = 'Create the period first, or record the transaction on a date inside one.';
  END IF;
  IF p.status = 'closed' THEN
    RAISE EXCEPTION 'The period of % is closed, so no holding moves on it', NEW.effective_on
      USING ERRCODE = 'FY003', HINT = 'Ask an Owner to reopen the period; an effective date is never moved later (FR-EQ-17, INV-03).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER holding_movement_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.holding_movement
  FOR EACH ROW EXECUTE FUNCTION fyniq.holding_movement_guard();
CREATE TRIGGER holding_movement_no_truncate BEFORE TRUNCATE ON fyniq.holding_movement
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.holding_movement_guard();

-- +goose StatementBegin
-- At commit, a movement leaves its transaction whole and every holding at zero or above (FY288): a transfer is one
-- movement out of a holder and one into another, of the same class on the same day, netting to zero; a conversion is
-- one movement out of a class and one into another, of one holder on one day, with its entry when the accounts differ
-- (FY289 holds the entry to it); a cash issue moves one subscriber's shares; a reversal negates every movement of its
-- transaction; and no holder holds fewer than no shares of a class on any day from the movement's.
CREATE FUNCTION fyniq.holding_movement_settled() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  n        int;
  outs     fyniq.holding_movement;
  ins      fyniq.holding_movement;
  reversed text;
  cls      uuid := fyniq.share_class_of(NEW.organisation_id, NEW.class_account_id, NEW.class_id);
  low      numeric;
BEGIN
  IF NEW.transaction_id IS NOT NULL THEN
    SELECT count(*) INTO n FROM fyniq.holding_movement m
     WHERE m.organisation_id = NEW.organisation_id AND m.transaction_id = NEW.transaction_id;
    IF NEW.kind IN ('transfer', 'class_conversion') THEN
      SELECT * INTO outs FROM fyniq.holding_movement m
       WHERE m.organisation_id = NEW.organisation_id AND m.transaction_id = NEW.transaction_id AND m.shares < 0;
      SELECT * INTO ins FROM fyniq.holding_movement m
       WHERE m.organisation_id = NEW.organisation_id AND m.transaction_id = NEW.transaction_id AND m.shares > 0;
      IF n <> 2 OR outs.id IS NULL OR ins.id IS NULL OR outs.kind <> NEW.kind OR ins.kind <> NEW.kind
         OR outs.effective_on <> ins.effective_on
         OR (NEW.kind = 'transfer' AND (outs.holder_id = ins.holder_id OR outs.class_account_id <> ins.class_account_id
             OR outs.class_id IS DISTINCT FROM ins.class_id OR outs.shares <> -ins.shares))
         OR (NEW.kind = 'class_conversion' AND (outs.holder_id <> ins.holder_id
             OR (outs.class_account_id = ins.class_account_id AND outs.class_id IS NOT DISTINCT FROM ins.class_id))) THEN
        RAISE EXCEPTION 'Equity transaction % does not move shares as a % does', NEW.transaction_id, replace(NEW.kind, '_', ' ')
          USING ERRCODE = 'FY288',
                HINT = 'A transfer moves one class''s shares from one holder to another; a conversion moves one holder''s shares between classes (FR-EQ-17).';
      END IF;
      IF NEW.kind = 'class_conversion' AND outs.class_account_id <> ins.class_account_id
         AND NOT EXISTS (SELECT 1 FROM fyniq.journal_entry e
                          WHERE e.organisation_id = NEW.organisation_id AND e.source_type = 'equity_transaction'
                            AND e.source_id = NEW.transaction_id::text AND e.reverses_entry_id IS NULL
                            AND e.posting_date = NEW.effective_on) THEN
        RAISE EXCEPTION 'Conversion % moves shares between accounts with no entry', NEW.transaction_id USING ERRCODE = 'FY288',
          HINT = 'Post the conversion''s entry between the two share-capital accounts with its movements (FR-EQ-17).';
      END IF;
    ELSIF NEW.kind = 'issue' AND n <> 1 THEN
      RAISE EXCEPTION 'Cash issue % allots shares to one subscriber', NEW.transaction_id USING ERRCODE = 'FY288',
        HINT = 'Draft a cash issue for each subscriber (FR-EQ-17).';
    ELSIF NEW.kind = 'reversal' THEN
      SELECT v.payload->>'reverses_id' INTO reversed
        FROM fyniq.document dd
        JOIN fyniq.document_version v ON v.organisation_id = dd.organisation_id AND v.document_id = dd.id AND v.version = dd.current_version
       WHERE dd.organisation_id = NEW.organisation_id AND dd.id = NEW.transaction_id;
      IF EXISTS (SELECT 1 FROM fyniq.holding_movement m
                  WHERE m.organisation_id = NEW.organisation_id AND m.transaction_id::text = reversed
                    AND NOT EXISTS (SELECT 1 FROM fyniq.holding_movement x
                                     WHERE x.organisation_id = m.organisation_id AND x.reverses_id = m.id
                                       AND x.transaction_id = NEW.transaction_id))
         OR n <> (SELECT count(*) FROM fyniq.holding_movement m
                   WHERE m.organisation_id = NEW.organisation_id AND m.transaction_id::text = reversed) THEN
        RAISE EXCEPTION 'Reversal % does not negate every movement of transaction %', NEW.transaction_id, reversed
          USING ERRCODE = 'FY288', HINT = 'A reversal undoes the whole of one transaction, once (FR-EQ-17).';
      END IF;
    END IF;
  END IF;
  IF NEW.shares < 0 THEN
    SELECT min(x.running) INTO low FROM (
      SELECT m.effective_on, sum(m.shares) OVER (ORDER BY m.effective_on RANGE UNBOUNDED PRECEDING) AS running
        FROM fyniq.holding_movement m
       WHERE m.organisation_id = NEW.organisation_id AND m.holder_id = NEW.holder_id AND m.class_account_id = NEW.class_account_id
         AND fyniq.share_class_of(m.organisation_id, m.class_account_id, m.class_id) IS NOT DISTINCT FROM cls) x
     WHERE x.effective_on >= NEW.effective_on;
    IF low < 0 THEN
      RAISE EXCEPTION 'The movements leave a holder with fewer than no shares from %', NEW.effective_on USING ERRCODE = 'FY288',
        HINT = 'A holder transfers, converts or gives back only shares they hold on the day (FR-EQ-17).';
    END IF;
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER holding_movement_settled AFTER INSERT ON fyniq.holding_movement
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.holding_movement_settled();

ALTER TABLE fyniq.holding_movement ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.holding_movement FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.holding_movement
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

REVOKE ALL ON fyniq.holding_movement FROM PUBLIC;
GRANT SELECT ON fyniq.holding_movement TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (id, kind, transaction_id, share_issue_id, reverses_id, holder_id, class_account_id, class_id, shares, effective_on,
              recorded_by, recorded_at) ON fyniq.holding_movement TO fyniq_api;

-- +goose StatementBegin
-- The register's existing issues, each given its movement, organisation by organisation under row-level security. They
-- were audited when recorded; their movements carry the issue's own recorder and time.
DO $$
DECLARE
  org      uuid;
  previous text := current_setting('app.org_id', true);
BEGIN
  FOR org IN SELECT t.organisation_id FROM fyniq.tenant_directory t ORDER BY t.organisation_id LOOP
    PERFORM set_config('app.org_id', org::text, true);
    INSERT INTO fyniq.holding_movement (organisation_id, kind, share_issue_id, holder_id, class_account_id, class_id, shares,
                                        effective_on, recorded_by, recorded_at)
    SELECT s.organisation_id, 'issue', s.id, s.holder_id, s.class_account_id, s.class_id, s.shares, s.effective_on, s.recorded_by,
           s.recorded_at
      FROM fyniq.share_issue s
     WHERE s.organisation_id = org
     ORDER BY s.effective_on, s.id;
  END LOOP;
  PERFORM set_config('app.org_id', coalesce(previous, ''), true);
END
$$;
-- +goose StatementEnd

-- Each movement from now on is audited in the same transaction (AUD-01).
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.holding_movement
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('holding_movement', 'id');

-- +goose StatementBegin
-- Every share issue has its holding movement by commit (FY288): the register and the holdings never disagree.
CREATE FUNCTION fyniq.share_issue_moved() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM fyniq.holding_movement m WHERE m.organisation_id = NEW.organisation_id AND m.share_issue_id = NEW.id) THEN
    RAISE EXCEPTION 'Share issue % has no holding movement', NEW.id USING ERRCODE = 'FY288',
      HINT = 'Record the issue''s movement with it, which the register does (FR-EQ-17).';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER share_issue_moved AFTER INSERT ON fyniq.share_issue
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.share_issue_moved();

-- +goose StatementBegin
-- An entry from the source equity_transaction commits only with the movements of the conversion between classes that
-- names it, on their date, or as the reversal of an entry that has them (FY289; INV-13, rev 0.205).
CREATE FUNCTION fyniq.equity_transaction_entry_registered() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF NEW.reverses_entry_id IS NOT NULL THEN
    IF EXISTS (SELECT 1 FROM fyniq.journal_entry o WHERE o.organisation_id = NEW.organisation_id AND o.id = NEW.reverses_entry_id
                AND o.source_type = 'equity_transaction' AND o.source_id = NEW.source_id) THEN
      RETURN NULL;
    END IF;
  ELSIF EXISTS (SELECT 1 FROM fyniq.holding_movement m
                 WHERE m.organisation_id = NEW.organisation_id AND m.transaction_id::text = NEW.source_id
                   AND m.kind = 'class_conversion' AND m.effective_on = NEW.posting_date) THEN
    RETURN NULL;
  END IF;
  RAISE EXCEPTION 'Entry % comes from an equity transaction, but no conversion between classes names it', NEW.id
    USING ERRCODE = 'FY289', HINT = 'Make the conversion effective, which posts its entry with its movements (FR-EQ-17, INV-13).';
END
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER equity_transaction_entry_registered AFTER INSERT ON fyniq.journal_entry
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (NEW.source_type = 'equity_transaction')
  EXECUTE FUNCTION fyniq.equity_transaction_entry_registered();

-- +goose StatementBegin
-- (000252) A journal line keeps every rule 000199 gave it; share capital also takes a conversion between classes from the
-- equity register (INV-13, rev 0.205), and a line on share application monies names its subscriber, a shareholder or
-- investor, held for share like a director's person (B.7; FY250).
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

-- +goose StatementBegin
-- (000252) FY249 and FY251's first half: a contact with lines on a director's account stays a director or shareholder,
-- one with lines on share application monies stays a shareholder or investor, and a contact is created with no member.
CREATE OR REPLACE FUNCTION fyniq.contact_party_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF NEW.member_user_id IS NOT NULL THEN
      RAISE EXCEPTION 'Contact % is created with no member: its member is linked by an approved change', NEW.legal_name
        USING ERRCODE = 'FY251', HINT = 'Create the contact, then ask for its member to be linked (FR-EQ-01, FM-1).';
    END IF;
    RETURN NEW;
  END IF;
  IF (OLD.is_director OR OLD.is_shareholder) AND NOT (NEW.is_director OR NEW.is_shareholder)
     AND EXISTS (SELECT 1 FROM fyniq.journal_line l JOIN fyniq.account a ON a.organisation_id = l.organisation_id AND a.id = l.account_id
                  WHERE l.organisation_id = OLD.organisation_id AND l.party_id = OLD.id
                    AND a.role IN ('director-loan', 'deferred-remuneration')) THEN
    RAISE EXCEPTION 'Contact % has amounts on a director''s account, so it stays a director or shareholder', OLD.legal_name
      USING ERRCODE = 'FY249', HINT = 'Keep the role; the account''s history names the person (FR-EQ-01, B.7).';
  END IF;
  IF (OLD.is_shareholder OR OLD.is_investor) AND NOT (NEW.is_shareholder OR NEW.is_investor)
     AND EXISTS (SELECT 1 FROM fyniq.journal_line l JOIN fyniq.account a ON a.organisation_id = l.organisation_id AND a.id = l.account_id
                  WHERE l.organisation_id = OLD.organisation_id AND l.party_id = OLD.id AND a.role = 'share-application-monies') THEN
    RAISE EXCEPTION 'Contact % has share application monies, so it stays a shareholder or investor', OLD.legal_name
      USING ERRCODE = 'FY249', HINT = 'Keep the mark; the account''s history names the subscriber (B.7).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
