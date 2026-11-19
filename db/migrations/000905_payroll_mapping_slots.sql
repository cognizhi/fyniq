-- Which accounts a pay-run mapping slot may point at (p1-payroll-link design D8; FR-PPL-09): a department's salary posts to an active, postable expense
-- account that is not a system payroll account, and a deduction is recovered through an active, postable asset or liability account that is not a money
-- account, a director's account, share capital or the salaries payable account itself. The database refuses any other, whatever code writes the mapping.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- +goose StatementBegin
CREATE FUNCTION fyniq.payroll_mapping_slot_account() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  a fyniq.account%ROWTYPE;
BEGIN
  SELECT * INTO a FROM fyniq.account WHERE organisation_id = NEW.organisation_id AND id = NEW.account_id;
  IF a.is_group OR NOT a.is_active THEN
    RAISE EXCEPTION 'A mapping slot points at an active account that takes postings' USING ERRCODE = 'FY901';
  END IF;
  IF NEW.key LIKE 'department:%' AND (a.type <> 'expense' OR a.role IN ('employer-cpf', 'sdl')) THEN
    RAISE EXCEPTION 'A department''s salary posts to an expense account that is not employer CPF or SDL' USING ERRCODE = 'FY901';
  END IF;
  IF NEW.key LIKE 'deduction:%' AND (a.type NOT IN ('asset', 'liability') OR a.is_money_account OR a.role = 'share-capital'
       OR a.role IN ('salaries-payable', 'cpf-sdl-payable', 'director-loan', 'deferred-remuneration', 'gst-control')) THEN
    RAISE EXCEPTION 'A deduction is recovered through an asset or liability account that is not money, share capital or a payroll or director account' USING ERRCODE = 'FY901';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER payroll_mapping_slot_account BEFORE INSERT ON fyniq.payroll_mapping_entry
  FOR EACH ROW EXECUTE FUNCTION fyniq.payroll_mapping_slot_account();
