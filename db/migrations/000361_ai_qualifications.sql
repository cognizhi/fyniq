-- Qualification before use (PRD-001 AI-78, AI-87, CMP-AI-03; p1-ai-providers task 5.4, design D10).
--
-- ai_qualification is each run of an agent's eval suite against a connection's model: the suite and its version, the
-- threshold it needed, how many cases passed, the result, and the model version the provider reported while it ran. A
-- model serves an agent only on its latest passing qualification for that agent; a later answer reporting another
-- version, or, for a moving alias, a week passing, puts it out of date: then it carries no personal data or payroll
-- until it is qualified again. A run is kept as it was made; only its going out of date is added, once, with why
-- (FY339). The API and the worker run qualifications; the AI's role writes none of it (CMP-AI-01). The eval runner's
-- calls are recorded outside any route, as the probe's are.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.ai_qualification (
  organisation_id  uuid         NOT NULL DEFAULT fyniq.current_org(),
  id               uuid         NOT NULL DEFAULT uuidv7(),
  agent            text         NOT NULL,
  connection_id    uuid         NOT NULL,
  model            text         NOT NULL,
  version          text         NOT NULL DEFAULT '', -- the version the provider reported while it ran; '' when none
  suite            text         NOT NULL,
  suite_version    integer      NOT NULL,
  threshold        numeric(5,4) NOT NULL,           -- the share of cases that must pass
  cases            integer      NOT NULL,
  passed           integer      NOT NULL,
  result           text         NOT NULL,
  run_id           uuid         NOT NULL,           -- the eval run: its calls are in ai_call under this run
  ran_by           text         NOT NULL,
  ran_at           timestamptz  NOT NULL,
  out_of_date_at   timestamptz,
  out_of_date_why  text,
  CONSTRAINT ai_qualification_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT ai_qualification_agent_fkey FOREIGN KEY (agent) REFERENCES fyniq.ai_agent (id),
  CONSTRAINT ai_qualification_model_fkey FOREIGN KEY (organisation_id, connection_id, model)
    REFERENCES fyniq.ai_model (organisation_id, connection_id, model),
  CONSTRAINT ai_qualification_suite_format CHECK (suite ~ '^[a-z][a-z0-9_.-]{1,59}$' AND suite_version >= 1),
  CONSTRAINT ai_qualification_version_length CHECK (length(version) <= 200),
  CONSTRAINT ai_qualification_counts CHECK (cases >= 1 AND passed BETWEEN 0 AND cases),
  CONSTRAINT ai_qualification_threshold CHECK (threshold <> 'NaN' AND threshold > 0 AND threshold <= 1),
  CONSTRAINT ai_qualification_result CHECK (result IN ('pass', 'fail') AND (result = 'pass') = (passed >= threshold * cases)),
  CONSTRAINT ai_qualification_out_of_date CHECK ((out_of_date_at IS NULL) = (out_of_date_why IS NULL) AND length(out_of_date_why) <= 500),
  CONSTRAINT ai_qualification_ran_by_format CHECK (ran_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);
CREATE INDEX ai_qualification_latest ON fyniq.ai_qualification (organisation_id, connection_id, model, agent, ran_at DESC);

-- +goose StatementBegin
CREATE FUNCTION fyniq.ai_qualification_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR (NEW.organisation_id, NEW.id, NEW.agent, NEW.connection_id, NEW.model, NEW.version, NEW.suite, NEW.suite_version,
       NEW.threshold, NEW.cases, NEW.passed, NEW.result, NEW.run_id, NEW.ran_by, NEW.ran_at)
     IS DISTINCT FROM (OLD.organisation_id, OLD.id, OLD.agent, OLD.connection_id, OLD.model, OLD.version, OLD.suite, OLD.suite_version,
       OLD.threshold, OLD.cases, OLD.passed, OLD.result, OLD.run_id, OLD.ran_by, OLD.ran_at)
     OR OLD.out_of_date_at IS NOT NULL THEN
    RAISE EXCEPTION 'A qualification is kept as it ran: only its going out of date is added, once' USING ERRCODE = 'FY339';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER ai_qualification_guard BEFORE UPDATE OR DELETE ON fyniq.ai_qualification
  FOR EACH ROW EXECUTE FUNCTION fyniq.ai_qualification_guard();
CREATE TRIGGER ai_qualification_no_truncate BEFORE TRUNCATE ON fyniq.ai_qualification
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.ai_qualification
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('ai_qualification', 'id');

ALTER TABLE fyniq.ai_qualification ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.ai_qualification FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.ai_qualification
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

REVOKE ALL ON fyniq.ai_qualification FROM PUBLIC;
GRANT SELECT ON fyniq.ai_qualification TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (agent, connection_id, model, version, suite, suite_version, threshold, cases, passed, result, run_id, ran_by, ran_at),
  UPDATE (out_of_date_at, out_of_date_why) ON fyniq.ai_qualification TO fyniq_api, fyniq_worker;

-- The eval runner's calls are outside any route too.
ALTER TABLE fyniq.ai_call DROP CONSTRAINT ai_call_steps;
ALTER TABLE fyniq.ai_call ADD CONSTRAINT ai_call_steps CHECK (route_version >= 0 AND step BETWEEN 1 AND 5
  AND (fell_back_from IS NULL OR fell_back_from BETWEEN 1 AND step - 1)
  AND (route_version > 0 OR (agent IN ('probe', 'eval_runner') AND step = 1))) NOT VALID;
