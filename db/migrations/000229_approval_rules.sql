-- The conditions of approval policies (PRD-001 FR-APR-01; rev 0.178, the owner's decision of 6 Nov 2026;
-- p1-approval-engine task 2.1).
--
-- A policy is an ordered list of rules, each with conditions (an amount at or above, departments, accounts, vendors)
-- and its own steps, and its default steps: the first rule whose conditions all hold decides a request's steps, else
-- the default steps do. The database keeps the rules as it keeps the steps:
--   - a version's rules are at most 10, each with at least one condition, its named records UUIDs, its amount a decimal
--     at or above zero, and its steps signable as a policy's steps are (approval_rules_valid);
--   - a version exists only with the approved request that holds exactly its steps and its rules (FY105).
-- The new check is added NOT VALID and validated by 000230 (squawk: constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- +goose StatementBegin
CREATE FUNCTION fyniq.approval_rules_valid(p_rules jsonb) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  r jsonb;
  c jsonb;
  k text;
BEGIN
  IF p_rules IS NULL OR jsonb_typeof(p_rules) <> 'array' OR jsonb_array_length(p_rules) > 10 THEN
    RETURN false;
  END IF;
  FOR r IN SELECT value FROM jsonb_array_elements(p_rules) LOOP
    IF jsonb_typeof(r) <> 'object' OR EXISTS (SELECT 1 FROM jsonb_object_keys(r) x WHERE x NOT IN ('conditions', 'steps'))
       OR NOT fyniq.approval_steps_valid(r -> 'steps') THEN
      RETURN false;
    END IF;
    c := r -> 'conditions';
    IF jsonb_typeof(c) IS DISTINCT FROM 'object'
       OR EXISTS (SELECT 1 FROM jsonb_object_keys(c) x WHERE x NOT IN ('amount_at_least', 'departments', 'accounts', 'vendors'))
       OR jsonb_typeof(c -> 'amount_at_least') IS DISTINCT FROM 'string'
       OR NOT ((c ->> 'amount_at_least') = '' OR (c ->> 'amount_at_least') ~ '^(0|[1-9][0-9]{0,15})(\.[0-9]{1,4})?$') THEN
      RETURN false;
    END IF;
    FOREACH k IN ARRAY ARRAY['departments', 'accounts', 'vendors'] LOOP
      IF jsonb_typeof(c -> k) IS DISTINCT FROM 'array' OR jsonb_array_length(c -> k) > 50
         OR EXISTS (SELECT 1 FROM jsonb_array_elements(c -> k) u
                     WHERE jsonb_typeof(u) <> 'string' OR (u #>> '{}') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$') THEN
        RETURN false;
      END IF;
    END LOOP;
    IF (c ->> 'amount_at_least') = '' AND jsonb_array_length(c -> 'departments') = 0 AND jsonb_array_length(c -> 'accounts') = 0
       AND jsonb_array_length(c -> 'vendors') = 0 THEN
      RETURN false; -- a rule with no condition would match nothing
    END IF;
  END LOOP;
  RETURN true;
END
$$;
-- +goose StatementEnd

ALTER TABLE fyniq.approval_policy ADD COLUMN rules jsonb NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE fyniq.approval_policy ADD CONSTRAINT approval_policy_rules_valid CHECK (fyniq.approval_rules_valid(rules)) NOT VALID;

-- +goose StatementBegin
-- approval_policy_guard as in 000039, and: the approved request holds exactly the version's rules too (FY105; FR-APR-01).
CREATE OR REPLACE FUNCTION fyniq.approval_policy_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  r      fyniq.approval_request;
  latest integer;
BEGIN
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'A version of an approval policy is never changed or deleted' USING ERRCODE = 'FY104',
      HINT = 'Change the policy: its next version is approved like any change (FR-APR-09).';
  END IF;
  SELECT * INTO r FROM fyniq.approval_request q WHERE q.organisation_id = NEW.organisation_id AND q.id = NEW.request_id;
  IF r.status IS DISTINCT FROM 'approved' OR r.object_type <> 'approval_policy' OR r.operation <> 'approval_policy.change'
     OR r.object_key <> NEW.object_type OR r.requested_by <> NEW.created_by
     OR (r.change -> 'execute' -> 'steps') IS DISTINCT FROM NEW.steps
     OR coalesce(r.change -> 'execute' -> 'rules', '[]'::jsonb) IS DISTINCT FROM NEW.rules
     OR (r.change -> 'execute' ->> 'version') IS DISTINCT FROM NEW.version::text THEN
    RAISE EXCEPTION 'A version of an approval policy exists only with the approved request that holds exactly its steps and rules'
      USING ERRCODE = 'FY105', HINT = 'Ask for the policy change and have another member approve it (FR-APR-09, SOD-8).';
  END IF;
  SELECT max(p.version) INTO latest FROM fyniq.approval_policy p
   WHERE p.organisation_id = NEW.organisation_id AND p.object_type = NEW.object_type;
  IF NEW.version <> coalesce(latest, 0) + 1 THEN
    RAISE EXCEPTION 'The next version of the % policy is %, not %', NEW.object_type, coalesce(latest, 0) + 1, NEW.version
      USING ERRCODE = 'FY105', HINT = 'Read the policy again and ask for the change from its latest version.';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

GRANT INSERT (rules) ON fyniq.approval_policy TO fyniq_api;
GRANT EXECUTE ON FUNCTION fyniq.approval_rules_valid(jsonb) TO fyniq_api;
