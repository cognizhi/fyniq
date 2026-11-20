-- The executor creates and changes a bank rule (PRD-001 Appendix E bank.rule_upsert; p1-ai-proposals task 3.2, design D8).
--
-- An approved bank.rule_upsert proposal creates or changes a rule through the functions a person's create and change run, as the approving person.
-- The grants are the API's own columns MINUS auto_apply and auto_apply_approval_id: the executor can never set a rule to apply by itself (that
-- is another person's approval after its back-test, FR-BNK-14, and the rule's own guard refuses it without one), and changing a rule's conditions
-- already clears it in the guard. It reads the rules (to find the one it changes) and nothing of the lines: the back-test is the dry run's, under the
-- API's role. A rule the executor creates says it is an AI proposal's, by a trigger, so a rule made by the AI cannot pass for a person's. The
-- executor never deletes a rule (the guard refuses that for everyone). Needs human review (grants).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

GRANT SELECT ON fyniq.bank_rule TO fyniq_executor;
GRANT INSERT (name, position, conditions, action, active, from_ai_proposal, created_by, created_at, changed_by, changed_at) ON fyniq.bank_rule TO fyniq_executor;
GRANT UPDATE (name, position, conditions, action, active, changed_by, changed_at, version) ON fyniq.bank_rule TO fyniq_executor;

-- +goose StatementBegin
CREATE FUNCTION fyniq.bank_rule_executor_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF current_user = 'fyniq_executor' AND NOT NEW.from_ai_proposal THEN
    RAISE EXCEPTION 'A bank rule the executor makes says it is an AI proposal''s' USING ERRCODE = '42501';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER bank_rule_executor_guard BEFORE INSERT ON fyniq.bank_rule
  FOR EACH ROW EXECUTE FUNCTION fyniq.bank_rule_executor_guard();

-- +goose Down
DROP TRIGGER bank_rule_executor_guard ON fyniq.bank_rule;
DROP FUNCTION fyniq.bank_rule_executor_guard();
REVOKE UPDATE (name, position, conditions, action, active, changed_by, changed_at, version) ON fyniq.bank_rule FROM fyniq_executor;
REVOKE INSERT (name, position, conditions, action, active, from_ai_proposal, created_by, created_at, changed_by, changed_at) ON fyniq.bank_rule FROM fyniq_executor;
REVOKE SELECT ON fyniq.bank_rule FROM fyniq_executor;
