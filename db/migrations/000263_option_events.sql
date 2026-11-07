-- A grant's forfeitures and expiries (PRD-001 FR-EQ-19; p1-equity-register slice 5b-i, task 6.1, design D9).
--
-- An option_event document forfeits a recorded grant's options not vested, or lets them lapse: drafted by the
-- Accountant with its evidence and recorded on another person's approval, never the grant's holder (FM-1); recorded, it
-- never changes, and its options return to the pool from its day.
--
-- The database is the backstop (FY293):
--   - an event's version keeps its own keys, a kind (forfeiture or expiry), a whole number of options above zero, and a
--     recorded grant dated on or before its day;
--   - recorded, a grant's events never take more options than the grant gave;
--   - the pool counts what they give back: option_pool_guard (000261) is replaced so a later grant may use it, from the
--     event's day on.
-- The widened checks are added NOT VALID and validated by 000264. Vesting, which limits a forfeiture to the options not
-- vested, is computed and so checked by the engine only (design D9).

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
                  'instrument_template', 'option_pool', 'option_grant', 'option_event')) NOT VALID;

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
                               ('option_pool', 'option_pool.record'), ('option_grant', 'option_grant.record'),
                               ('option_event', 'option_event.record'))) NOT VALID;
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
                  'option_pool', 'option_grant', 'option_event')) NOT VALID;

-- +goose StatementBegin
-- (000261, 000263) A pool resolution's, grant's or grant event's version keeps its rules whoever writes it (FY293).
CREATE OR REPLACE FUNCTION fyniq.option_guard() RETURNS trigger
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
  IF t NOT IN ('option_pool', 'option_grant', 'option_event') THEN
    RETURN NEW;
  END IF;
  IF t = 'option_event' THEN
    SELECT k INTO extra FROM jsonb_object_keys(p) k
     WHERE k NOT IN ('date', 'grant_id', 'kind', 'number', 'on', 'note') LIMIT 1;
    IF extra IS NOT NULL OR NOT coalesce(p->>'number', '') ~ '^[1-9][0-9]{0,13}$'
       OR coalesce(p->>'kind', '') NOT IN ('forfeiture', 'expiry') THEN
      RAISE EXCEPTION 'A grant''s event keeps its grant, a kind (forfeiture or expiry), a whole number of options above zero and its day only'
        USING ERRCODE = 'FY293', HINT = 'Forfeit or let lapse whole options of a recorded grant (FR-EQ-19).';
    END IF;
    SELECT EXISTS (SELECT 1 FROM fyniq.document d
                     JOIN fyniq.document_version v ON v.organisation_id = d.organisation_id AND v.document_id = d.id
                                                  AND v.version = d.current_version
                    WHERE d.organisation_id = NEW.organisation_id AND d.id::text = p->>'grant_id'
                      AND d.type = 'option_grant' AND d.status = 'posted'
                      AND (v.payload->>'granted_on')::date <= (p->>'on')::date)
      INTO ok;
    IF NOT ok THEN
      RAISE EXCEPTION 'A grant''s event names a recorded grant, on or after its grant date' USING ERRCODE = 'FY293',
        HINT = 'Record the grant first, and date the event on or after it (FR-EQ-19).';
    END IF;
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

-- +goose StatementBegin
-- (000261, 000263) The pool covers its grants on every day, counting what forfeitures and expiries give back (FY293).
CREATE OR REPLACE FUNCTION fyniq.option_pool_guard() RETURNS trigger
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
    UNION ALL  -- (000263) what forfeitures and expiries give back, from their day
    SELECT (v.payload->>'on')::date, -(v.payload->>'number')::numeric
      FROM fyniq.document d
      JOIN fyniq.document_version v ON v.organisation_id = d.organisation_id AND v.document_id = d.id AND v.version = d.current_version
     WHERE d.organisation_id = NEW.organisation_id AND d.type = 'option_event' AND d.status = 'posted'
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

-- +goose StatementBegin
-- A grant's recorded events never take more options than it gave (FY293), under the lock on the organisation's pool.
CREATE FUNCTION fyniq.option_event_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  e     jsonb;
  given numeric;
  taken numeric;
BEGIN
  PERFORM pg_advisory_xact_lock(hashtextextended('fyniq.option_pool:' || NEW.organisation_id::text, 0));
  SELECT v.payload INTO e FROM fyniq.document_version v
   WHERE v.organisation_id = NEW.organisation_id AND v.document_id = NEW.id AND v.version = NEW.current_version;
  SELECT (v.payload->>'number')::numeric INTO given FROM fyniq.document d
    JOIN fyniq.document_version v ON v.organisation_id = d.organisation_id AND v.document_id = d.id AND v.version = d.current_version
   WHERE d.organisation_id = NEW.organisation_id AND d.id::text = e->>'grant_id' AND d.type = 'option_grant' AND d.status = 'posted';
  SELECT coalesce(sum((v.payload->>'number')::numeric), 0) INTO taken FROM fyniq.document d
    JOIN fyniq.document_version v ON v.organisation_id = d.organisation_id AND v.document_id = d.id AND v.version = d.current_version
   WHERE d.organisation_id = NEW.organisation_id AND d.type = 'option_event' AND d.status = 'posted'
     AND v.payload->>'grant_id' = e->>'grant_id';
  IF given IS NULL OR taken > given THEN
    RAISE EXCEPTION 'The grant''s events take % options, more than the % it gave', taken, coalesce(given, 0)
      USING ERRCODE = 'FY293', HINT = 'Forfeit or let lapse at most what the grant leaves (FR-EQ-19).';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER option_event_guard AFTER UPDATE OF status ON fyniq.document
  FOR EACH ROW WHEN (NEW.type = 'option_event' AND NEW.status = 'posted' AND OLD.status IS DISTINCT FROM 'posted')
  EXECUTE FUNCTION fyniq.option_event_guard();
