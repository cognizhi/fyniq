-- The scheduler's month-end revaluation (PRD-001 FR-GL-04, FR-AUT-01, FR-AUT-02; rev 0.75; p1-multi-currency-fx task 3.7).
--
-- The platform job fx.revalue revalues each ended, open period whose foreign-currency balances are due once its last
-- day's rates are in, as the principal "scheduler:fx.revalue" with the worker's role. The worker still holds no INSERT
-- on documents and cannot name who posted one (FY053): its one way to make a revaluation is
-- create_scheduled_revaluation, which saves the run the application worked out as a posted fx_revaluation document of
-- that period, saved and posted by the scheduler, never by a person. It makes no other type, and only for an open
-- period whose last day and the day after are the payload's (FY169). The entry is the engine's, appended as every
-- posting is (the worker has held INSERT on journal_entry and journal_line since 000004), and the database's own checks
-- still apply to it: balanced, base-only lines only on a revaluation (FY008), nothing in a closed period.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- +goose StatementBegin
CREATE FUNCTION fyniq.create_scheduled_revaluation(p_period uuid, p_payload jsonb, p_at timestamptz) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  org uuid := fyniq.require_org();
  p   fyniq.period;
  doc uuid;
BEGIN
  -- FOR SHARE, so the run and the period's close never interleave.
  SELECT * INTO p FROM fyniq.period WHERE organisation_id = org AND id = p_period FOR SHARE;
  IF NOT FOUND OR p.status <> 'open' OR p_at IS NULL OR jsonb_typeof(p_payload) IS DISTINCT FROM 'object'
     OR p_payload->>'period_id' IS DISTINCT FROM p.id::text
     OR p_payload->>'date' IS DISTINCT FROM to_char(p.ends_on, 'YYYY-MM-DD')
     OR p_payload->>'reverse_on' IS DISTINCT FROM to_char(p.ends_on + 1, 'YYYY-MM-DD') THEN
    RAISE EXCEPTION 'Period % has no revaluation the scheduler can save', p_period
      USING ERRCODE = 'FY169', HINT = 'The scheduler revalues an open period at its last day, as the application works it out (FR-GL-04).';
  END IF;
  INSERT INTO fyniq.document (organisation_id, type) VALUES (org, 'fx_revaluation') RETURNING id INTO doc;
  INSERT INTO fyniq.document_version (organisation_id, document_id, version, payload, created_by)
    VALUES (org, doc, 1, p_payload, 'scheduler:fx.revalue');
  UPDATE fyniq.document SET status = 'posted', posted_by = 'scheduler:fx.revalue', posted_at = p_at
   WHERE organisation_id = org AND id = doc;
  RETURN doc;
END
$$;
-- +goose StatementEnd

GRANT EXECUTE ON FUNCTION fyniq.create_scheduled_revaluation(uuid, jsonb, timestamptz) TO fyniq_worker;
