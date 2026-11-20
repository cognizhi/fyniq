-- The executor drafts a budget version (PRD-001 Appendix E plan.propose_budget; p1-ai-proposals task 4.1, design D8).
--
-- An approved plan.propose_budget proposal drafts a budget through the function a person's Draft a budget runs, which saves a budget document: the
-- executor already holds the document's and its version's insert columns (000005, 000052). The budget's own guard (budget_guard, FY301) runs as the
-- writer and calls two pure validators of the forecast keys and decimals, which the executor lacked: this grants EXECUTE on those two functions and on
-- nothing else. They read no table. The budget stays a draft; approving or recording one is a person's. Needs human review (grants).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

GRANT EXECUTE ON FUNCTION fyniq.forecast_keys(jsonb, text[], text[]), fyniq.forecast_decimal(jsonb, boolean) TO fyniq_executor;

-- The executor holds UPDATE on a document's status and current version for the documents it posts (000005), which for a budget would be recording
-- or revising it: approving a budget is on the AI's never-list, so the database refuses the executor any change of a budget document once drafted.
-- +goose StatementBegin
CREATE FUNCTION fyniq.budget_executor_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF current_user = 'fyniq_executor' AND OLD.type = 'budget' THEN
    RAISE EXCEPTION 'The executor drafts a budget and never revises, approves or records one' USING ERRCODE = '42501';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER budget_executor_guard BEFORE UPDATE ON fyniq.document
  FOR EACH ROW EXECUTE FUNCTION fyniq.budget_executor_guard();

-- +goose Down
DROP TRIGGER budget_executor_guard ON fyniq.document;
DROP FUNCTION fyniq.budget_executor_guard();
REVOKE EXECUTE ON FUNCTION fyniq.forecast_keys(jsonb, text[], text[]), fyniq.forecast_decimal(jsonb, boolean) FROM fyniq_executor;
