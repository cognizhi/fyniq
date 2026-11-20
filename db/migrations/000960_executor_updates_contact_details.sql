-- The executor changes six details of a contact (PRD-001 Appendix E ct.update_contact; p1-ai-proposals task 4.1, design D8).
--
-- An approved ct.update_contact proposal changes a contact through its one narrow query, as the approving person. The grant is the
-- tightest the command needs and no more: the billing email, the phone, the address, the payment terms, the default category and the
-- default GST code, with `version` (the query bumps it). It holds NOTHING on the legal name, the UEN or tax ID, the Peppol ID, the
-- remittance email, the country, the tax residence, the currency, the discount terms, the customer group, the activity flag, the
-- director, shareholder, investor and individual flags or the linked member: the API's own all-column grant stays the API's.
-- Needs human review (grants).
--
-- Two more guards sit in the database, whatever the command does: the executor never changes the contact of a person (an individual,
-- a director, a shareholder, an investor or a linked member: their values are sealed in the audit trail and their records are on
-- the AI's never-list for equity holders), and never a contact that is not active.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

GRANT UPDATE (billing_email, phone, address, payment_terms_days, default_account_id, default_gst_code, version) ON fyniq.contact TO fyniq_executor;

-- +goose StatementBegin
CREATE FUNCTION fyniq.contact_executor_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF current_user = 'fyniq_executor' AND (OLD.individual OR OLD.is_director OR OLD.is_shareholder OR OLD.is_investor
      OR OLD.member_user_id IS NOT NULL OR NOT OLD.is_active) THEN
    RAISE EXCEPTION 'The executor changes the details of an active company contact, never a person''s or an inactive one' USING ERRCODE = '42501';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER contact_executor_guard BEFORE UPDATE ON fyniq.contact
  FOR EACH ROW EXECUTE FUNCTION fyniq.contact_executor_guard();

-- +goose Down
DROP TRIGGER contact_executor_guard ON fyniq.contact;
DROP FUNCTION fyniq.contact_executor_guard();
REVOKE UPDATE (billing_email, phone, address, payment_terms_days, default_account_id, default_gst_code, version) ON fyniq.contact FROM fyniq_executor;
