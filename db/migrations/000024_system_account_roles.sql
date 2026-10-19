-- System accounts by role (PRD-001 FR-COA-03, rev 0.32; p1-organisation-chart task 3.2).
--
-- 000014 gave accounts a role for share capital and said the system accounts would extend it. Every system
-- account of B.5 now has its role, the key the engine finds it by whatever an organisation's codes are. The
-- standard chart (internal/domain/chart) sets them. The guard adds two rules: each role is on one account
-- (share capital on two or more), and a system account cannot be deactivated. A role is still fixed once the
-- account exists. The API may now change an account's name, place in the hierarchy, active flag and the
-- FR-COA-02 attributes; its code, type and role never change (account_guard).
--
-- The widened check is added NOT VALID and validated by 000025 in a transaction of its own (squawk:
-- constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.account DROP CONSTRAINT account_role_known;
ALTER TABLE fyniq.account ADD CONSTRAINT account_role_known CHECK (role IN ('accounts-receivable', 'accounts-payable', 'gst-control', 'fx-rounding', 'unrealised-fx', 'deferred-revenue', 'prepayments', 'salaries-payable', 'cpf-sdl-payable', 'employer-cpf', 'sdl', 'depreciation', 'accumulated-depreciation', 'suspense', 'retained-earnings', 'expense-claims-payable', 'income-tax-payable', 'income-tax-expense', 'director-loan', 'deferred-remuneration', 'share-capital', 'capital-contribution')) NOT VALID;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.account_guard() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'UPDATE' AND (NEW.code IS DISTINCT FROM OLD.code
     OR NEW.type IS DISTINCT FROM OLD.type OR NEW.created_at IS DISTINCT FROM OLD.created_at) THEN
    RAISE EXCEPTION 'The code and type of account % cannot change', OLD.code
      USING ERRCODE = 'FY040', HINT = 'Create a new account and deactivate this one (FR-COA-04).';
  END IF;
  -- (000014) The role is fixed when the account is created: a share-capital account stays one (INV-13).
  IF TG_OP = 'UPDATE' AND NEW.role IS DISTINCT FROM OLD.role THEN
    RAISE EXCEPTION 'The role of account % cannot change', OLD.code
      USING ERRCODE = 'FY040', HINT = 'Create a new account with the role and deactivate this one (FR-COA-04, INV-13).';
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
  -- (000024) Each system role is on one account, share capital on two or more (FR-COA-03, rev 0.32), so the
  -- engine finds a system account by its role.
  IF NEW.role IS NOT NULL AND NEW.role <> 'share-capital' AND EXISTS (
       SELECT 1 FROM fyniq.account a
        WHERE a.organisation_id = NEW.organisation_id AND a.role = NEW.role AND a.id <> NEW.id) THEN
    RAISE EXCEPTION 'Another account already has the role %', NEW.role
      USING ERRCODE = 'FY042', HINT = 'A system role is on one account (FR-COA-03).';
  END IF;
  -- (000024) A system account stays active: the engine posts to it (FR-COA-03, rev 0.32).
  IF TG_OP = 'UPDATE' AND OLD.is_active AND NOT NEW.is_active AND NEW.role IS NOT NULL THEN
    RAISE EXCEPTION 'Account % is a system account (%), so it cannot be deactivated', OLD.code, OLD.role
      USING ERRCODE = 'FY040', HINT = 'The engine posts to it (FR-COA-03).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

GRANT UPDATE (report_group, cash_flow_line, function, is_money_account, default_gst_code, currency, is_control, is_payroll_cost, guidance)
  ON fyniq.account TO fyniq_api;
