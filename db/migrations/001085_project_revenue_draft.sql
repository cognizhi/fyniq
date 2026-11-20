-- The month-end proposal of revenue over time, made by the platform (PRD-001 FR-PRJ-11, rev 0.587; p2-customer-portal-subscriptions task 4.1 leftovers).
--
-- A job that has no right to create documents makes a month's draft journal for a project elected for revenue over time through this one function, which makes a draft journal and its run
-- and nothing else: it posts nothing, and the draft waits for a person to approve and post it like any journal. It refuses a project with no over-time election, a month that is not a first day
-- or that has a run already, and a draft that is not what the proposal makes: two or three lines, each on an active account that takes postings and is an asset, a liability or revenue,
-- each carrying the project and no other dimension value, balanced to nothing, dated the month's last day. (The pattern is create_scheduled_draft, 000064.)

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- +goose StatementBegin
CREATE FUNCTION fyniq.create_project_revenue_draft(p_project uuid, p_month date, p_payload jsonb, p_adjustment numeric, p_earned numeric) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  org  uuid := fyniq.require_org();
  l    jsonb;
  acct fyniq.account;
  sum  numeric := 0;
  n    integer;
  doc  uuid;
BEGIN
  IF p_month <> date_trunc('month', p_month)::date THEN
    RAISE EXCEPTION 'The month % is not the first day of a month', p_month USING ERRCODE = 'FYP25';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM fyniq.project_revenue_election e WHERE e.organisation_id = org AND e.project_id = p_project AND e.method = 'over_time') THEN
    RAISE EXCEPTION 'Project % is not elected for revenue over time', p_project USING ERRCODE = 'FYP25', HINT = 'An accountant elects a fixed-fee project first (FR-PRJ-11).';
  END IF;
  IF EXISTS (SELECT 1 FROM fyniq.project_revenue_run r WHERE r.organisation_id = org AND r.project_id = p_project AND r.month = p_month) THEN
    RAISE EXCEPTION 'A journal is proposed for project % and % already', p_project, p_month USING ERRCODE = 'FYP25';
  END IF;
  IF p_payload->>'date' IS DISTINCT FROM to_char((p_month + interval '1 month' - interval '1 day')::date, 'YYYY-MM-DD')
     OR jsonb_typeof(p_payload->'lines') <> 'array' OR p_payload ? 'reverse_on' OR p_payload ? 'recurring_journal' THEN
    RAISE EXCEPTION 'The draft is not the month''s revenue journal' USING ERRCODE = 'FYP25';
  END IF;
  n := jsonb_array_length(p_payload->'lines');
  IF n NOT BETWEEN 2 AND 3 THEN
    RAISE EXCEPTION 'The month''s revenue journal has two or three lines, not %', n USING ERRCODE = 'FYP25';
  END IF;
  FOR l IN SELECT * FROM jsonb_array_elements(p_payload->'lines') LOOP
    SELECT * INTO acct FROM fyniq.account a WHERE a.organisation_id = org AND a.id = (l->>'account_id')::uuid;
    IF NOT FOUND OR NOT acct.is_active OR acct.is_group OR acct.type NOT IN ('asset', 'liability', 'revenue') THEN
      RAISE EXCEPTION 'A line is on an account that does not take the revenue journal' USING ERRCODE = 'FYP25';
    END IF;
    IF l->'dimension_value_ids' IS DISTINCT FROM jsonb_build_array(p_project::text) THEN
      RAISE EXCEPTION 'Each line carries the project and nothing else' USING ERRCODE = 'FYP25';
    END IF;
    sum := sum + (l->'amount'->>'amount')::numeric;
  END LOOP;
  IF sum <> 0 THEN
    RAISE EXCEPTION 'The month''s revenue journal does not balance (%)', sum USING ERRCODE = 'FYP25';
  END IF;
  INSERT INTO fyniq.document (organisation_id, type) VALUES (org, 'journal') RETURNING id INTO doc;
  INSERT INTO fyniq.document_version (organisation_id, document_id, version, payload, created_by)
    VALUES (org, doc, 1, p_payload, 'scheduler:projects.revenue');
  INSERT INTO fyniq.project_revenue_run (organisation_id, project_id, month, journal_id, adjustment, earned, created_by, created_at)
    VALUES (org, p_project, p_month, doc, p_adjustment, p_earned, 'scheduler:projects.revenue', now());
  RETURN doc;
END
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION fyniq.create_project_revenue_draft(uuid, date, jsonb, numeric, numeric) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.create_project_revenue_draft(uuid, date, jsonb, numeric, numeric) TO fyniq_worker;
