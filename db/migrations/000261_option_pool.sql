-- The option pool and its grants (PRD-001 FR-EQ-19; p1-equity-register slice 5a, task 6.1, design D9).
--
-- A pool resolution is an option_pool document: the shares a resolution sets aside for options, the class they are of,
-- the resolution and the day it was passed. A grant is an option_grant document: its holder, the number of options, the
-- exercise price, the grant date, the vesting (cliff, months, acceleration) and the expiry. Each is drafted, takes its
-- evidence (the resolution; the grant letter) and is recorded on another person's approval (FM-1); a recorded one
-- never changes. The pool's size on a day is the latest recorded resolution passed on or before it.
--
-- The database is the backstop (FY293):
--   - a pool resolution's or grant's version keeps its own keys, a size or number of whole shares above zero, an
--     exercise price from zero with at most 6 decimals, a holder marked investor or shareholder, and an active
--     share-capital class;
--   - a grant is recorded only with a recorded resolution in force on its grant date, of its class; and once a grant
--     or a resolution is recorded, the pool covers the grants made by every later day, under a lock on the pool;
--   - (FY249) a contact holding a recorded grant stays an investor or shareholder.
-- An investor may already have a linked member (contact_member_person, 000250), so an option holder who holds no
-- shares signs in to see their own grants. The widened checks are added NOT VALID and validated by 000262.

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
                  'owner_succession', 'holder_record', 'share_class', 'equity_transaction', 'instrument',
                  'instrument_template', 'option_pool', 'option_grant')) NOT VALID;

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
                               ('equity_transaction', 'equity_transaction.approve'),
                               ('instrument', 'instrument.record'),
                               ('instrument_template', 'instrument_template.enable'),
                               ('option_pool', 'option_pool.record'), ('option_grant', 'option_grant.record'))) NOT VALID;
ALTER TABLE fyniq.approval_policy DROP CONSTRAINT approval_policy_object_known;
ALTER TABLE fyniq.approval_policy ADD CONSTRAINT approval_policy_object_known CHECK (
  object_type IN ('organisation_setting', 'membership', 'member_invitation', 'statutory_pack', 'team_mode', 'period', 'journal',
                  'opening_balance', 'bill', 'bill_payment', 'credit_application', 'supplier_refund', 'purchase_order',
                  'document_template', 'invoice', 'customer_receipt', 'write_off', 'customer_credit_application',
                  'customer_refund', 'customer_deposit', 'recurring_invoice', 'connector', 'bank_rule', 'expense_claim',
                  'claim_reimbursement', 'cash_advance', 'asset_disposal', 'fixed_asset',
                  'payee_bank_change', 'claimant_bank_change', 'payment_batch', 'contact_member_link',
                  'director_fee', 'director_settlement', 'director_repayment', 'account', 'bank_reconciliation',
                  'holder_record', 'share_class', 'equity_transaction', 'instrument', 'instrument_template',
                  'option_pool', 'option_grant')) NOT VALID;

-- +goose StatementBegin
-- A pool resolution's or grant's version keeps its rules whoever writes it (FY293).
CREATE FUNCTION fyniq.option_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  t     text;
  p     jsonb := NEW.payload;
  extra text;
  ok    boolean;
BEGIN
  SELECT d.type INTO t FROM fyniq.document d WHERE d.organisation_id = NEW.organisation_id AND d.id = NEW.document_id;
  IF t NOT IN ('option_pool', 'option_grant') THEN
    RETURN NEW;
  END IF;
  IF t = 'option_pool' THEN
    SELECT k INTO extra FROM jsonb_object_keys(p) k
     WHERE k NOT IN ('date', 'size', 'class_account_id', 'resolution_ref', 'resolved_on', 'note') LIMIT 1;
    IF extra IS NOT NULL OR NOT coalesce(p->>'size', '') ~ '^[1-9][0-9]{0,13}$' THEN
      RAISE EXCEPTION 'A pool resolution keeps its size, a whole number of shares above zero, its class and resolution only'
        USING ERRCODE = 'FY293', HINT = 'Record the shares the resolution sets aside for options (FR-EQ-19).';
    END IF;
  ELSE
    SELECT k INTO extra FROM jsonb_object_keys(p) k
     WHERE k NOT IN ('date', 'holder_id', 'number', 'exercise_price', 'currency', 'granted_on', 'cliff_months',
                     'vesting_months', 'acceleration', 'expires_on', 'class_account_id', 'note') LIMIT 1;
    IF extra IS NOT NULL OR NOT coalesce(p->>'number', '') ~ '^[1-9][0-9]{0,13}$'
       OR NOT coalesce(p->>'exercise_price', '') ~ '^[0-9]{1,10}(\.[0-9]{1,6})?$' THEN
      RAISE EXCEPTION 'A grant keeps its holder, a whole number of options above zero, an exercise price from zero and its terms only'
        USING ERRCODE = 'FY293', HINT = 'Grant whole options at a price per share (FR-EQ-19).';
    END IF;
    SELECT EXISTS (SELECT 1 FROM fyniq.contact c
                    WHERE c.organisation_id = NEW.organisation_id AND c.id::text = p->>'holder_id'
                      AND (c.is_shareholder OR c.is_investor))
      INTO ok;
    IF NOT ok THEN
      RAISE EXCEPTION 'A grant''s holder is a contact marked investor or shareholder' USING ERRCODE = 'FY293',
        HINT = 'Mark the contact an investor in Contacts first (FR-EQ-19).';
    END IF;
  END IF;
  SELECT EXISTS (SELECT 1 FROM fyniq.account a
                  WHERE a.organisation_id = NEW.organisation_id AND a.id::text = p->>'class_account_id'
                    AND a.role = 'share-capital' AND a.is_active AND NOT a.is_group)
    INTO ok;
  IF NOT ok THEN
    RAISE EXCEPTION 'The pool''s options are over the class of an active share-capital account' USING ERRCODE = 'FY293',
      HINT = 'Choose 3010 Ordinary share capital, or the class the options are over (FR-EQ-19).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER option_guard BEFORE INSERT ON fyniq.document_version
  FOR EACH ROW EXECUTE FUNCTION fyniq.option_guard();

-- +goose StatementBegin
-- The pool covers its grants on every day (FY293): a grant is recorded only with a recorded resolution in force on its
-- grant date, of its class; and once a grant or a resolution is recorded, on each day from its date on that a grant
-- was made or a resolution passed, the options granted on or before it are at most the size of the latest resolution
-- passed on or before it. The organisation's pool is locked while it is checked, so two grants recorded at once cannot
-- both take the last of it, and an earlier-dated grant or a smaller resolution cannot short a later day.
CREATE FUNCTION fyniq.option_pool_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  p     jsonb;
  pool  jsonb;
  since date;
  short record;
BEGIN
  PERFORM pg_advisory_xact_lock(hashtextextended('fyniq.option_pool:' || NEW.organisation_id::text, 0));
  SELECT v.payload INTO p FROM fyniq.document_version v
   WHERE v.organisation_id = NEW.organisation_id AND v.document_id = NEW.id AND v.version = NEW.current_version;
  IF NEW.type = 'option_grant' THEN
    since := (p->>'granted_on')::date;
    SELECT v.payload INTO pool FROM fyniq.document d
      JOIN fyniq.document_version v ON v.organisation_id = d.organisation_id AND v.document_id = d.id AND v.version = d.current_version
     WHERE d.organisation_id = NEW.organisation_id AND d.type = 'option_pool' AND d.status = 'posted'
       AND (v.payload->>'resolved_on')::date <= since
     ORDER BY (v.payload->>'resolved_on')::date DESC, d.created_at DESC
     LIMIT 1;
    IF pool IS NULL OR pool->>'class_account_id' IS DISTINCT FROM p->>'class_account_id' THEN
      RAISE EXCEPTION 'No recorded resolution sets an option pool of the grant''s class on %', since
        USING ERRCODE = 'FY293', HINT = 'Record the pool''s resolution first (FR-EQ-19).';
    END IF;
  ELSE
    since := (p->>'resolved_on')::date;
  END IF;
  WITH pools AS (
    SELECT (v.payload->>'resolved_on')::date AS day, (v.payload->>'size')::numeric AS size, d.created_at
      FROM fyniq.document d
      JOIN fyniq.document_version v ON v.organisation_id = d.organisation_id AND v.document_id = d.id AND v.version = d.current_version
     WHERE d.organisation_id = NEW.organisation_id AND d.type = 'option_pool' AND d.status = 'posted'
  ), grants AS (
    SELECT (v.payload->>'granted_on')::date AS day, (v.payload->>'number')::numeric AS number
      FROM fyniq.document d
      JOIN fyniq.document_version v ON v.organisation_id = d.organisation_id AND v.document_id = d.id AND v.version = d.current_version
     WHERE d.organisation_id = NEW.organisation_id AND d.type = 'option_grant' AND d.status = 'posted'
  ), days AS (
    SELECT since AS day UNION SELECT day FROM grants WHERE day >= since UNION SELECT day FROM pools WHERE day >= since
  ), totals AS (
    SELECT x.day,
           coalesce((SELECT pl.size FROM pools pl WHERE pl.day <= x.day ORDER BY pl.day DESC, pl.created_at DESC LIMIT 1), 0) AS size,
           (SELECT coalesce(sum(g.number), 0) FROM grants g WHERE g.day <= x.day) AS granted
      FROM days x
  )
  SELECT * INTO short FROM totals WHERE granted > size ORDER BY day LIMIT 1;
  IF short.day IS NOT NULL THEN
    RAISE EXCEPTION 'The pool of % shares on % is short of the % options granted by then', short.size, short.day, short.granted
      USING ERRCODE = 'FY293', HINT = 'Grant at most what the pool leaves on every day, or record a resolution that enlarges it (FR-EQ-19).';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER option_pool_guard AFTER UPDATE OF status ON fyniq.document
  FOR EACH ROW WHEN (NEW.type IN ('option_grant', 'option_pool') AND NEW.status = 'posted' AND OLD.status IS DISTINCT FROM 'posted')
  EXECUTE FUNCTION fyniq.option_pool_guard();

-- +goose StatementBegin
-- (000252, 000255, 000261) FY249 and FY251's first half: a contact with lines on a director's account stays a director
-- or shareholder, one with lines on share application monies, a recorded instrument or a recorded grant of options stays
-- a shareholder or investor, and a contact is created with no member.
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
  IF (OLD.is_shareholder OR OLD.is_investor) AND NOT (NEW.is_shareholder OR NEW.is_investor)
     AND EXISTS (SELECT 1 FROM fyniq.document d
                   JOIN fyniq.document_version v ON v.organisation_id = d.organisation_id AND v.document_id = d.id
                                                AND v.version = d.current_version
                  WHERE d.organisation_id = OLD.organisation_id AND d.type = 'instrument' AND d.status = 'posted'
                    AND v.payload->>'holder_id' = OLD.id::text) THEN
    RAISE EXCEPTION 'Contact % holds a recorded SAFE or convertible note, so it stays a shareholder or investor', OLD.legal_name
      USING ERRCODE = 'FY249', HINT = 'Keep the mark; the instrument names its holder (FR-EQ-18).';
  END IF;
  IF (OLD.is_shareholder OR OLD.is_investor) AND NOT (NEW.is_shareholder OR NEW.is_investor)
     AND EXISTS (SELECT 1 FROM fyniq.document d
                   JOIN fyniq.document_version v ON v.organisation_id = d.organisation_id AND v.document_id = d.id
                                                AND v.version = d.current_version
                  WHERE d.organisation_id = OLD.organisation_id AND d.type = 'option_grant' AND d.status = 'posted'
                    AND v.payload->>'holder_id' = OLD.id::text) THEN
    RAISE EXCEPTION 'Contact % holds a recorded grant of options, so it stays a shareholder or investor', OLD.legal_name
      USING ERRCODE = 'FY249', HINT = 'Keep the mark; the grant names its holder (FR-EQ-19).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
