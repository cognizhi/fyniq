-- Explanations of the close's month-on-month variances (PRD-001 FR-GL-14; the founders' decisions of rev 0.63, built in
-- rev 0.65; p1-general-ledger-close task 6.3).
--
-- The close compares each income or expense account's total for the month with the previous month's. A variance above
-- the organisation's thresholds needs a short explanation before the close is signed off. An explanation records the two
-- totals it explained, so it stays the explanation of those figures: once either total changes, the close asks for a new
-- one (the explanation is stale; a provisional rule of rev 0.65). Explanations are append-only: a new one replaces the
-- account's earlier one for the period, and none ever changes or goes (FY163). The database refuses an explanation in a
-- period that is not open, or of an account that is not an income or expense account (FY162); the period is read FOR
-- SHARE, so an explanation and the period's close never interleave.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.period_variance_explanation (
  organisation_id uuid           NOT NULL DEFAULT fyniq.current_org(),
  id              uuid           NOT NULL DEFAULT uuidv7(),
  period_id       uuid           NOT NULL,
  account_id      uuid           NOT NULL,
  current_amount  numeric(20, 4) NOT NULL,  -- the account's total for the period, in the base currency: debit +, credit −
  previous_amount numeric(20, 4) NOT NULL,  -- its total for the previous period
  explanation     text           NOT NULL,
  written_by      text           NOT NULL,  -- "type:id", as the audit trail names the actor
  written_at      timestamptz    NOT NULL,
  CONSTRAINT period_variance_explanation_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT period_variance_explanation_period_fkey FOREIGN KEY (organisation_id, period_id)
    REFERENCES fyniq.period (organisation_id, id),
  CONSTRAINT period_variance_explanation_account_fkey FOREIGN KEY (organisation_id, account_id)
    REFERENCES fyniq.account (organisation_id, id),
  CONSTRAINT period_variance_explanation_amounts CHECK (current_amount <> 'NaN' AND previous_amount <> 'NaN'),
  CONSTRAINT period_variance_explanation_length CHECK (length(btrim(explanation)) BETWEEN 1 AND 1000),
  CONSTRAINT period_variance_explanation_actor_format CHECK (written_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);
CREATE INDEX period_variance_explanation_period
  ON fyniq.period_variance_explanation (organisation_id, period_id, account_id, written_at);

-- An explanation is of an income or expense account, in an open period; once written, it never changes or goes.
-- +goose StatementBegin
CREATE FUNCTION fyniq.period_variance_explanation_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  p record;
  kind text;
BEGIN
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'An explanation of a variance never changes or goes' USING ERRCODE = 'FY163',
      HINT = 'Write a new explanation: it replaces the earlier one for the account (FR-GL-14).';
  END IF;
  SELECT status, starts_on, ends_on INTO p FROM fyniq.period
   WHERE organisation_id = NEW.organisation_id AND id = NEW.period_id
   FOR SHARE;
  IF p.status IS DISTINCT FROM 'open' THEN
    RAISE EXCEPTION 'The period % to % is %, so its variances are not explained now', p.starts_on, p.ends_on, p.status
      USING ERRCODE = 'FY162', HINT = 'Explain variances while the period is open; reopen it first (FR-GL-08, FR-GL-14).';
  END IF;
  SELECT type INTO kind FROM fyniq.account WHERE organisation_id = NEW.organisation_id AND id = NEW.account_id;
  IF kind IS DISTINCT FROM 'revenue' AND kind IS DISTINCT FROM 'expense' THEN
    RAISE EXCEPTION 'Only an income or expense account''s variance is explained, not a % account''s', kind
      USING ERRCODE = 'FY162', HINT = 'Balance-sheet accounts rely on the support the close requires (FR-GL-14, INV-07).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER period_variance_explanation_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.period_variance_explanation
  FOR EACH ROW EXECUTE FUNCTION fyniq.period_variance_explanation_guard();
CREATE TRIGGER period_variance_explanation_no_truncate BEFORE TRUNCATE ON fyniq.period_variance_explanation
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();

ALTER TABLE fyniq.period_variance_explanation ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.period_variance_explanation FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.period_variance_explanation
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.period_variance_explanation
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('period_variance_explanation', 'id');

GRANT SELECT ON fyniq.period_variance_explanation TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
GRANT INSERT (period_id, account_id, current_amount, previous_amount, explanation, written_by, written_at)
  ON fyniq.period_variance_explanation TO fyniq_api;
