-- Access and correction requests (PRD-001 CMP-PD-04, rev 0.244; p1-pdpa-retention slice 6, design D5). A member asks
-- about their own personal data, or the Compliance officer records a request from anyone else. Each is answered within
-- 30 days of receipt, or the requester is told in writing within those 30 days when it will be; its steps (acknowledged,
-- extended, answered, closed) are added in order. Requests and steps are only ever added (FY314), each audited in the
-- same transaction (AUD-01), under the "Data protection (PDPA)" and "Own personal-data requests" rows of Appendix D.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.pd_request (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  kind            text        NOT NULL,  -- access or correction
  subject         text        NOT NULL,  -- member:<user id>, contact:<contact id>, or person: someone else
  requester       text,                  -- who asked and how to reach them, when not a member asking for themselves
  details         text        NOT NULL,  -- what they ask for
  received_on     date        NOT NULL,
  due_on          date        NOT NULL,  -- 30 days after receipt
  created_by      text        NOT NULL,
  created_at      timestamptz NOT NULL,
  CONSTRAINT pd_request_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT pd_request_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT pd_request_kind_known CHECK (kind IN ('access', 'correction')),
  CONSTRAINT pd_request_subject_format CHECK (subject ~ '^((member|contact):[0-9a-f-]{36}|person)$'),
  CONSTRAINT pd_request_requester_length CHECK (length(btrim(requester)) BETWEEN 1 AND 500),
  CONSTRAINT pd_request_someone_asks CHECK (subject LIKE 'member:%' OR requester IS NOT NULL),
  CONSTRAINT pd_request_details_length CHECK (length(btrim(details)) BETWEEN 1 AND 4000),
  CONSTRAINT pd_request_due CHECK (due_on = received_on + 30),
  CONSTRAINT pd_request_created_by_format CHECK (created_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);

CREATE TABLE fyniq.pd_request_step (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  request_id      uuid        NOT NULL,
  step            text        NOT NULL,  -- acknowledged, extended, answered or closed
  at              timestamptz NOT NULL,
  by_actor        text        NOT NULL,
  note            text,
  new_due_on      date,                  -- an extension's new date, told to the requester in writing
  outcome         text,                  -- an answer's: provided, corrected, partly or refused
  CONSTRAINT pd_request_step_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT pd_request_step_request_fkey FOREIGN KEY (organisation_id, request_id) REFERENCES fyniq.pd_request (organisation_id, id),
  CONSTRAINT pd_request_step_known CHECK (step IN ('acknowledged', 'extended', 'answered', 'closed')),
  CONSTRAINT pd_request_step_extension CHECK ((step = 'extended') = (new_due_on IS NOT NULL)),
  CONSTRAINT pd_request_step_outcome CHECK ((step = 'answered') = (outcome IS NOT NULL)
    AND (outcome IS NULL OR outcome IN ('provided', 'corrected', 'partly', 'refused'))),
  CONSTRAINT pd_request_step_note_length CHECK (length(note) BETWEEN 1 AND 4000),
  CONSTRAINT pd_request_step_by_format CHECK (by_actor ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);
CREATE INDEX pd_request_step_request ON fyniq.pd_request_step (organisation_id, request_id, at);

-- Steps come in order: nothing after closing; an answer once, and closing only after it; one extension, written within
-- the first 30 days, to a later day; an acknowledgement before any other. Nothing changes or goes.
-- +goose StatementBegin
CREATE FUNCTION fyniq.pd_request_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  r     fyniq.pd_request;
  steps text[];
BEGIN
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'Personal-data requests and their steps never change or go' USING ERRCODE = 'FY314',
      HINT = 'Add the request''s next step (CMP-PD-04).';
  END IF;
  IF TG_TABLE_NAME = 'pd_request_step' THEN
    SELECT * INTO r FROM fyniq.pd_request q WHERE q.organisation_id = NEW.organisation_id AND q.id = NEW.request_id;
    SELECT coalesce(array_agg(s.step ORDER BY s.at, s.id), '{}') INTO steps FROM fyniq.pd_request_step s
      WHERE s.organisation_id = NEW.organisation_id AND s.request_id = NEW.request_id;
    IF 'closed' = ANY(steps) THEN
      RAISE EXCEPTION 'The request is closed' USING ERRCODE = 'FY314';
    END IF;
    IF NEW.step = ANY(steps) THEN
      RAISE EXCEPTION 'The request was % already', NEW.step USING ERRCODE = 'FY314';
    END IF;
    IF NEW.step = 'closed' AND NOT 'answered' = ANY(steps) THEN
      RAISE EXCEPTION 'A request is closed only after it is answered' USING ERRCODE = 'FY314';
    END IF;
    IF NEW.step = 'extended' AND ('answered' = ANY(steps) OR (NEW.at AT TIME ZONE 'Asia/Singapore')::date > r.due_on OR NEW.new_due_on <= r.due_on) THEN
      RAISE EXCEPTION 'An extension is told in writing within the first 30 days, before the answer, and names a later day' USING ERRCODE = 'FY314';
    END IF;
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER pd_request_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.pd_request
  FOR EACH ROW EXECUTE FUNCTION fyniq.pd_request_guard();
CREATE TRIGGER pd_request_step_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.pd_request_step
  FOR EACH ROW EXECUTE FUNCTION fyniq.pd_request_guard();
CREATE TRIGGER pd_request_no_truncate BEFORE TRUNCATE ON fyniq.pd_request
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE TRIGGER pd_request_step_no_truncate BEFORE TRUNCATE ON fyniq.pd_request_step
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.pd_request
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('pd_request', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.pd_request_step
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('pd_request', 'request_id');

ALTER TABLE fyniq.pd_request ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.pd_request FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.pd_request
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.pd_request_step ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.pd_request_step FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.pd_request_step
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

GRANT INSERT (kind, subject, requester, details, received_on, due_on, created_by, created_at) ON fyniq.pd_request TO fyniq_api;
GRANT INSERT (request_id, step, at, by_actor, note, new_due_on, outcome) ON fyniq.pd_request_step TO fyniq_api;
GRANT SELECT ON fyniq.pd_request, fyniq.pd_request_step TO fyniq_api, fyniq_worker, fyniq_readonly;
