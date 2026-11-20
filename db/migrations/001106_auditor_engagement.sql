-- The Auditor's engagement (PRD-001 FR-UX-42, owner's decision of 23 Nov 2026; p2-reporting-planning task 7.1, design D13): a named, time-boxed engagement
-- with an auditor (a member holding the Auditor role), the auditor's requests and the samples they draw. An Owner opens an engagement and a second Owner
-- (in Founder mode the same one) confirms it; requests and samples can be added only while it is active. A sample is insert-only: it is the evidence of
-- what the auditor drew, and its seed makes the draw reproducible. Needs human review (a new right for the Auditor role, Appendix D).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.auditor_engagement (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  name            text        NOT NULL,
  firm            text        NOT NULL DEFAULT '',
  auditor_user_id uuid        NOT NULL,
  period_from     date        NOT NULL,
  period_to       date        NOT NULL,
  opens_on        date        NOT NULL,
  closes_on       date        NOT NULL,
  status          text        NOT NULL DEFAULT 'pending',
  opened_by       text        NOT NULL,
  opened_at       timestamptz NOT NULL,
  confirmed_by    text,
  confirmed_at    timestamptz,
  closed_by       text,
  closed_at       timestamptz,
  CONSTRAINT auditor_engagement_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT auditor_engagement_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT auditor_engagement_name CHECK (length(btrim(name)) BETWEEN 2 AND 120 AND length(firm) <= 120),
  CONSTRAINT auditor_engagement_period CHECK (period_from <= period_to AND opens_on <= closes_on AND closes_on - opens_on <= 366),
  CONSTRAINT auditor_engagement_status CHECK (status IN ('pending', 'active', 'closed')),
  CONSTRAINT auditor_engagement_by_form CHECK (opened_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$' AND (confirmed_by IS NULL OR confirmed_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
    AND (closed_by IS NULL OR closed_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')),
  CONSTRAINT auditor_engagement_state CHECK ((status = 'pending' AND confirmed_at IS NULL AND closed_at IS NULL) OR (status = 'active' AND confirmed_at IS NOT NULL AND confirmed_by IS NOT NULL AND closed_at IS NULL)
    OR (status = 'closed' AND closed_at IS NOT NULL AND closed_by IS NOT NULL))
);

CREATE TABLE fyniq.auditor_request (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  engagement_id   uuid        NOT NULL,
  number          integer     NOT NULL,
  title           text        NOT NULL,
  detail          text        NOT NULL DEFAULT '',
  due_on          date,
  assignee_user_id uuid       NOT NULL,
  status          text        NOT NULL DEFAULT 'open',
  requested_by    text        NOT NULL,
  requested_at    timestamptz NOT NULL,
  answer          text,
  evidence_ids    uuid[]      NOT NULL DEFAULT '{}',
  answered_by     text,
  answered_at     timestamptz,
  closed_by       text,
  closed_at       timestamptz,
  CONSTRAINT auditor_request_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT auditor_request_engagement_fkey FOREIGN KEY (organisation_id, engagement_id) REFERENCES fyniq.auditor_engagement (organisation_id, id),
  CONSTRAINT auditor_request_number UNIQUE (organisation_id, engagement_id, number),
  CONSTRAINT auditor_request_text CHECK (length(btrim(title)) BETWEEN 2 AND 200 AND length(detail) <= 2000 AND (answer IS NULL OR length(btrim(answer)) BETWEEN 1 AND 4000)),
  CONSTRAINT auditor_request_status CHECK (status IN ('open', 'answered', 'closed')),
  CONSTRAINT auditor_request_evidence CHECK (cardinality(evidence_ids) <= 20),
  CONSTRAINT auditor_request_state CHECK ((status = 'open' AND answered_at IS NULL AND closed_at IS NULL) OR (status = 'answered' AND answered_at IS NOT NULL AND answered_by IS NOT NULL AND answer IS NOT NULL AND closed_at IS NULL)
    OR (status = 'closed' AND closed_at IS NOT NULL AND closed_by IS NOT NULL))
);

CREATE TABLE fyniq.auditor_sample (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  engagement_id   uuid        NOT NULL,
  rule            jsonb       NOT NULL,
  seed            text        NOT NULL,
  population      integer     NOT NULL,
  items           jsonb       NOT NULL,
  drawn_by        text        NOT NULL,
  drawn_at        timestamptz NOT NULL,
  CONSTRAINT auditor_sample_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT auditor_sample_engagement_fkey FOREIGN KEY (organisation_id, engagement_id) REFERENCES fyniq.auditor_engagement (organisation_id, id),
  CONSTRAINT auditor_sample_form CHECK (jsonb_typeof(rule) = 'object' AND jsonb_typeof(items) = 'array' AND jsonb_array_length(items) <= 200 AND population >= jsonb_array_length(items)
    AND length(seed) BETWEEN 1 AND 64 AND drawn_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);

-- +goose StatementBegin
CREATE FUNCTION fyniq.auditor_engagement_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF NEW.status <> 'pending' THEN
      RAISE EXCEPTION 'An engagement starts pending' USING ERRCODE = 'FYR10', HINT = 'Open it, then have an Owner confirm it (FR-UX-42).';
    END IF;
    RETURN NEW;
  END IF;
  IF TG_OP <> 'UPDATE' THEN
    RAISE EXCEPTION 'An engagement is never deleted or truncated' USING ERRCODE = 'FYR10', HINT = 'Close it instead (FR-UX-42).';
  END IF;
  IF (NEW.id, NEW.organisation_id, NEW.name, NEW.firm, NEW.auditor_user_id, NEW.period_from, NEW.period_to, NEW.opens_on, NEW.closes_on, NEW.opened_by, NEW.opened_at)
       IS DISTINCT FROM (OLD.id, OLD.organisation_id, OLD.name, OLD.firm, OLD.auditor_user_id, OLD.period_from, OLD.period_to, OLD.opens_on, OLD.closes_on, OLD.opened_by, OLD.opened_at)
     OR NOT ((OLD.status = 'pending' AND NEW.status IN ('active', 'closed')) OR (OLD.status = 'active' AND NEW.status = 'closed')) THEN
    RAISE EXCEPTION 'An engagement only moves forward: pending, active, closed' USING ERRCODE = 'FYR10', HINT = 'Open a new engagement instead (FR-UX-42).';
  END IF;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER auditor_engagement_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.auditor_engagement FOR EACH ROW EXECUTE FUNCTION fyniq.auditor_engagement_guard();
CREATE TRIGGER auditor_engagement_no_truncate BEFORE TRUNCATE ON fyniq.auditor_engagement FOR EACH STATEMENT EXECUTE FUNCTION fyniq.auditor_engagement_guard();

-- Requests and samples are added only while the engagement is active; a request moves open, answered, closed (a re-answer before it is closed is allowed); a sample never changes.
-- +goose StatementBegin
CREATE FUNCTION fyniq.auditor_child_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE st text;
BEGIN
  IF TG_OP IN ('DELETE', 'TRUNCATE') THEN
    RAISE EXCEPTION 'An audit request or sample is never deleted' USING ERRCODE = 'FYR10', HINT = 'Close the request instead (FR-UX-42).';
  END IF;
  IF TG_OP = 'UPDATE' AND TG_TABLE_NAME = 'auditor_sample' THEN
    RAISE EXCEPTION 'A drawn sample is never changed' USING ERRCODE = 'FYR10', HINT = 'Draw another (FR-UX-42).';
  END IF;
  SELECT status INTO st FROM fyniq.auditor_engagement WHERE organisation_id = NEW.organisation_id AND id = NEW.engagement_id;
  IF st IS DISTINCT FROM 'active' THEN
    RAISE EXCEPTION 'The engagement is not active' USING ERRCODE = 'FYR10', HINT = 'Requests and samples are added while an engagement is active (FR-UX-42).';
  END IF;
  IF TG_TABLE_NAME = 'auditor_request' THEN
    IF TG_OP = 'INSERT' AND NEW.status <> 'open' THEN
      RAISE EXCEPTION 'A request starts open' USING ERRCODE = 'FYR10';
    END IF;
    IF TG_OP = 'UPDATE' AND ((NEW.id, NEW.organisation_id, NEW.engagement_id, NEW.number, NEW.title, NEW.detail, NEW.due_on, NEW.assignee_user_id, NEW.requested_by, NEW.requested_at)
         IS DISTINCT FROM (OLD.id, OLD.organisation_id, OLD.engagement_id, OLD.number, OLD.title, OLD.detail, OLD.due_on, OLD.assignee_user_id, OLD.requested_by, OLD.requested_at)
         OR OLD.status = 'closed' OR NOT ((OLD.status IN ('open', 'answered') AND NEW.status = 'answered') OR (OLD.status = 'answered' AND NEW.status = 'closed'))) THEN
      RAISE EXCEPTION 'A request changes only by being answered, then closed' USING ERRCODE = 'FYR10';
    END IF;
  END IF;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER auditor_request_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.auditor_request FOR EACH ROW EXECUTE FUNCTION fyniq.auditor_child_guard();
CREATE TRIGGER auditor_request_no_truncate BEFORE TRUNCATE ON fyniq.auditor_request FOR EACH STATEMENT EXECUTE FUNCTION fyniq.auditor_child_guard();
CREATE TRIGGER auditor_sample_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.auditor_sample FOR EACH ROW EXECUTE FUNCTION fyniq.auditor_child_guard();
CREATE TRIGGER auditor_sample_no_truncate BEFORE TRUNCATE ON fyniq.auditor_sample FOR EACH STATEMENT EXECUTE FUNCTION fyniq.auditor_child_guard();

-- +goose StatementBegin
DO $$
DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY['auditor_engagement', 'auditor_request', 'auditor_sample'] LOOP
    EXECUTE format('ALTER TABLE fyniq.%I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE fyniq.%I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY tenant_isolation ON fyniq.%I USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org())', t);
    EXECUTE format('CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.%I DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit(%L, ''id'')', t, t);
    EXECUTE format('REVOKE ALL ON fyniq.%I FROM PUBLIC', t);
    EXECUTE format('GRANT SELECT ON fyniq.%I TO fyniq_api, fyniq_worker, fyniq_readonly', t);
  END LOOP;
END
$$;
-- +goose StatementEnd
GRANT INSERT (name, firm, auditor_user_id, period_from, period_to, opens_on, closes_on, opened_by, opened_at) ON fyniq.auditor_engagement TO fyniq_api;
GRANT UPDATE (status, confirmed_by, confirmed_at, closed_by, closed_at) ON fyniq.auditor_engagement TO fyniq_api;
GRANT INSERT (engagement_id, number, title, detail, due_on, assignee_user_id, requested_by, requested_at) ON fyniq.auditor_request TO fyniq_api;
GRANT UPDATE (status, answer, evidence_ids, answered_by, answered_at, closed_by, closed_at) ON fyniq.auditor_request TO fyniq_api;
GRANT INSERT (engagement_id, rule, seed, population, items, drawn_by, drawn_at) ON fyniq.auditor_sample TO fyniq_api;

-- +goose Down
DROP TABLE fyniq.auditor_sample;
DROP TABLE fyniq.auditor_request;
DROP TABLE fyniq.auditor_engagement;
DROP FUNCTION fyniq.auditor_child_guard();
DROP FUNCTION fyniq.auditor_engagement_guard();
