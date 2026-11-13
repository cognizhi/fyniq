-- An Owner's price of a model, entered with a second approver (PRD-001 AI-82; p1-ai-providers task 4.2, design D4).
--
-- A model may be priced by an Owner, labelled with who entered it: an approval request of the operation
-- ai_connection.price, which a second approver approves. The API records it in force, naming that approved request; the
-- database refuses an Owner's price without one, or from the worker, and refuses the API any other source (FY333). A read
-- never overwrites it (000350). The Owner may withdraw it at once: it is then withdrawn, with who withdrew it and when,
-- and the next read prices the model from its source again. The checks are added NOT VALID and 000353 validates them
-- (squawk: constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.ai_price DROP CONSTRAINT ai_price_status_known;
ALTER TABLE fyniq.ai_price ADD CONSTRAINT ai_price_status_known
  CHECK (status IN ('active', 'pending', 'superseded', 'declined', 'withdrawn')) NOT VALID;
ALTER TABLE fyniq.ai_price ADD CONSTRAINT ai_price_withdrawn_entered CHECK (status <> 'withdrawn' OR source = 'owner') NOT VALID;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.ai_price_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  in_force record;
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A price and its rates are kept, so every past estimate can be reproduced' USING ERRCODE = 'FY333';
  END IF;
  IF TG_TABLE_NAME = 'ai_price_rate' THEN
    IF TG_OP <> 'INSERT' OR NOT EXISTS (SELECT 1 FROM fyniq.ai_price p WHERE p.organisation_id = NEW.organisation_id AND p.id = NEW.price_id
                                          AND p.created_xact = pg_current_xact_id()::text::bigint) THEN
      RAISE EXCEPTION 'A price''s rates are written with it, in its transaction, and never changed' USING ERRCODE = 'FY333';
    END IF;
    RETURN NEW;
  END IF;
  IF TG_OP = 'INSERT' THEN
    IF NEW.status NOT IN ('active', 'pending') OR NEW.decided_by IS NOT NULL OR NEW.created_xact <> pg_current_xact_id()::text::bigint THEN
      RAISE EXCEPTION 'A price starts in force or waiting for a review, undecided' USING ERRCODE = 'FY333';
    END IF;
    -- An Owner's price is the API's, in force, as its second approver approved it; the API records no other (AI-82). The
    -- request is read only for an Owner's price: the worker, which records the others, cannot read requests.
    IF NEW.source = 'owner' THEN
      IF current_user = 'fyniq_worker' OR NEW.status <> 'active' OR NOT EXISTS (
           SELECT 1 FROM fyniq.approval_request r
            WHERE r.organisation_id = NEW.organisation_id AND r.id = NEW.approval_id AND r.status = 'approved'
              AND r.operation = 'ai_connection.price' AND r.object_key = NEW.connection_id::text) THEN
        RAISE EXCEPTION 'An Owner''s price of % is recorded only as its approved request of ai_connection.price', NEW.model USING ERRCODE = 'FY333';
      END IF;
    ELSIF current_user = 'fyniq_api' THEN
      RAISE EXCEPTION 'Only a read of a price''s source records a price from it' USING ERRCODE = 'FY333';
    END IF;
  ELSE
    IF (NEW.organisation_id, NEW.id, NEW.connection_id, NEW.model, NEW.number, NEW.source, NEW.catalogue_id, NEW.catalogue_key, NEW.match,
        NEW.currency, NEW.ignored, NEW.review, NEW.review_detail, NEW.recorded_by, NEW.recorded_at, NEW.created_xact, NEW.approval_id)
       IS DISTINCT FROM
       (OLD.organisation_id, OLD.id, OLD.connection_id, OLD.model, OLD.number, OLD.source, OLD.catalogue_id, OLD.catalogue_key, OLD.match,
        OLD.currency, OLD.ignored, OLD.review, OLD.review_detail, OLD.recorded_by, OLD.recorded_at, OLD.created_xact, OLD.approval_id) THEN
      RAISE EXCEPTION 'A price is kept as it was recorded: only its state moves' USING ERRCODE = 'FY333';
    END IF;
    IF NEW.status = OLD.status AND (NEW.decided_by, NEW.decided_at) IS NOT DISTINCT FROM (OLD.decided_by, OLD.decided_at) THEN
      RETURN NEW;
    END IF;
    IF NOT ((OLD.status = 'pending' AND NEW.status IN ('active', 'declined') AND NEW.decided_by IS NOT NULL AND OLD.decided_by IS NULL)
         OR (OLD.status IN ('active', 'pending') AND NEW.status = 'superseded'
             AND (NEW.decided_by, NEW.decided_at) IS NOT DISTINCT FROM (OLD.decided_by, OLD.decided_at))
         OR (OLD.status = 'active' AND OLD.source = 'owner' AND NEW.status = 'withdrawn' AND NEW.decided_by IS NOT NULL
             AND OLD.decided_by IS NULL)) THEN
      RAISE EXCEPTION 'A price moves only from waiting to in force or declined by an Owner''s decision, to superseded, or, an Owner''s, to withdrawn'
        USING ERRCODE = 'FY333';
    END IF;
    IF NEW.status <> 'active' THEN
      RETURN NEW;
    END IF;
  END IF;
  -- A version that becomes active replaces the one in force and every older one waiting; one that starts waiting
  -- replaces the older one waiting. An Owner's price is replaced only by an Owner's (AI-82).
  IF NEW.status = 'active' THEN
    SELECT source, number INTO in_force FROM fyniq.ai_price
     WHERE organisation_id = NEW.organisation_id AND connection_id = NEW.connection_id AND model = NEW.model AND status = 'active' AND id <> NEW.id;
    IF FOUND AND in_force.source = 'owner' AND NEW.source <> 'owner' THEN
      RAISE EXCEPTION 'An Owner''s price of % is never overwritten by a read', NEW.model USING ERRCODE = 'FY333',
        HINT = 'Withdraw the Owner''s price first, if the source''s price should apply.';
    END IF;
    UPDATE fyniq.ai_price SET status = 'superseded'
     WHERE organisation_id = NEW.organisation_id AND connection_id = NEW.connection_id AND model = NEW.model AND id <> NEW.id
       AND (status = 'active' OR (status = 'pending' AND number < NEW.number));
  ELSE
    UPDATE fyniq.ai_price SET status = 'superseded'
     WHERE organisation_id = NEW.organisation_id AND connection_id = NEW.connection_id AND model = NEW.model AND id <> NEW.id
       AND status = 'pending';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

-- An Owner's price is a request of the connection, under "AI providers and model routes".
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
                               ('option_event', 'option_event.record'), ('round_scenario', 'round_scenario.approve'),
                               ('investor_right', 'investor_right.record'),
                               ('controller_entry', 'controller_entry.record'),
                               ('budget', 'budget.record'),
                               ('migration_commit', 'migration.commit'),
                               ('tenant_export', 'tenant_export.prepare'),
                               ('ai_connection', 'ai_connection.create'), ('ai_connection', 'ai_connection.change'),
                               ('ai_connection', 'ai_connection.credential'), ('ai_connection', 'ai_connection.enable'),
                               ('ai_data_policy', 'ai_data_policy.change'),
                               ('ai_connection', 'ai_connection.allow_model'),
                               ('ai_connection', 'ai_connection.price'))) NOT VALID;

-- The API records an Owner's price as its approval executes; the guard keeps it to that.
GRANT INSERT (connection_id, model, number, source, match, currency, status, recorded_by, recorded_at, approval_id) ON fyniq.ai_price TO fyniq_api;
GRANT INSERT (price_id, kind, above, unit, amount) ON fyniq.ai_price_rate TO fyniq_api;
