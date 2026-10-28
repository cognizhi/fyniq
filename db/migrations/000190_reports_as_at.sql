-- Reports as at a ledger revision (PRD-001 FR-RPT-08, ENG-46; rev 0.159; p1-reports tasks 1.1 and 1.3, design D4).
--
-- fyniq.balances_at(period, revision) is a period's balances as at a ledger revision: the lines of its entries with a
-- revision up to it, summed as the cache sums them (the cache holds only the balances as they stand). Revisions follow
-- commit order (journal_entry_assign locks the organisation's counter until commit), so a statement that reads the
-- ledger's revision and then the balances at it reads one snapshot, and a re-run at that revision is unchanged by any
-- later posting, back-dated or not (G4-051). Only the balance service calls it (ENG-04).
--
-- A report re-run as at a ledger revision shows today's account names and groups, labelled "restated presentation"
-- when an account was created or changed after the revision (the owner's decision of 28 Oct 2026, until the chart
-- keeps its history). account.changed_at is when the row last changed, set by the trigger alone: no role writes the
-- column. Accounts loaded before this migration have none, and count from their creation.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.account ADD COLUMN changed_at timestamptz;

-- +goose StatementBegin
CREATE FUNCTION fyniq.account_changed_at() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  NEW.changed_at := now();
  RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER account_changed_at BEFORE UPDATE ON fyniq.account
  FOR EACH ROW WHEN (OLD IS DISTINCT FROM NEW) EXECUTE FUNCTION fyniq.account_changed_at();

-- +goose StatementBegin
CREATE FUNCTION fyniq.balances_at(p_period uuid, p_revision bigint)
RETURNS TABLE (account_id uuid, period_id uuid, currency text, amount numeric, base_amount numeric, line_count bigint)
LANGUAGE sql STABLE
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT l.account_id, e.period_id, l.currency, sum(l.amount), sum(l.base_amount), count(*)
    FROM fyniq.journal_line l
    JOIN fyniq.journal_entry e ON e.organisation_id = l.organisation_id AND e.id = l.entry_id
   WHERE l.organisation_id = fyniq.current_org() AND e.period_id = p_period AND e.ledger_revision <= p_revision
   GROUP BY l.account_id, e.period_id, l.currency
$$;
-- +goose StatementEnd

GRANT EXECUTE ON FUNCTION fyniq.balances_at(uuid, bigint) TO fyniq_api, fyniq_worker, fyniq_readonly;
