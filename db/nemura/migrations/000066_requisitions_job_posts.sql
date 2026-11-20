-- Hiring, the first slice (PRD-002 NM-REC-01, NM-REC-02, NM-REC-03): requisitions (a role, the team, the reason, the salary range, the start date) approved by the Owner, and one job post for each,
-- checked for fair wording, with the dates of the Fair Consideration Framework's advert kept as evidence.
--
-- A requisition is decided once by an Owner; a job post is a draft until published and its published words never change (a changed post is a new one on a new requisition or a correction in the
-- record). Candidates, the pipeline and consent are a later migration. Money is NUMERIC(20,4), a decimal string in JSON, never a float.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE nemura.requisition (
  organisation_id uuid          NOT NULL DEFAULT nemura.current_org(),
  id              uuid          NOT NULL DEFAULT uuidv7(),
  title           text          NOT NULL,
  department      text          NOT NULL,
  reason          text          NOT NULL,
  replaces_person uuid,
  salary_min      numeric(20,4) NOT NULL,
  salary_max      numeric(20,4) NOT NULL,
  currency        text          NOT NULL DEFAULT 'SGD',
  target_start    date          NOT NULL,
  pass_needed     boolean       NOT NULL DEFAULT false,   -- an Employment Pass or S Pass application is expected: the Fair Consideration Framework may apply
  plan_status     text          NOT NULL DEFAULT 'unknown', -- whether the role is in Fyniq's plan: unknown until the link tells Nemura [live-verify]
  state           text          NOT NULL DEFAULT 'requested',
  requested_by    uuid          NOT NULL,
  requested_at    timestamptz   NOT NULL,
  decided_by      uuid,
  decided_at      timestamptz,
  decision_reason text,
  CONSTRAINT requisition_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT requisition_replaces_fkey FOREIGN KEY (organisation_id, replaces_person) REFERENCES nemura.person (organisation_id, id),
  CONSTRAINT requisition_requester_fkey FOREIGN KEY (organisation_id, requested_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT requisition_decider_fkey FOREIGN KEY (organisation_id, decided_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT requisition_title_length CHECK (length(btrim(title)) BETWEEN 1 AND 120),
  CONSTRAINT requisition_department_length CHECK (length(btrim(department)) BETWEEN 1 AND 120),
  CONSTRAINT requisition_reason_known CHECK (reason IN ('new', 'replacement')),
  CONSTRAINT requisition_replacement_whole CHECK ((reason = 'replacement') = (replaces_person IS NOT NULL)),
  CONSTRAINT requisition_salary_range CHECK (salary_min >= 0 AND salary_max >= salary_min AND salary_max < 100000000),
  CONSTRAINT requisition_currency_known CHECK (currency = 'SGD'),
  CONSTRAINT requisition_plan_known CHECK (plan_status IN ('unknown', 'in_plan', 'not_in_plan')),
  CONSTRAINT requisition_state_known CHECK (state IN ('requested', 'approved', 'rejected', 'filled', 'cancelled')),
  CONSTRAINT requisition_decided_whole CHECK ((state IN ('requested', 'cancelled')) = (decided_at IS NULL) OR state = 'cancelled'),
  CONSTRAINT requisition_reason_text CHECK (decision_reason IS NULL OR length(decision_reason) <= 500)
);

-- +goose StatementBegin
CREATE FUNCTION nemura.requisition_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR (NEW.title, NEW.department, NEW.reason, NEW.replaces_person, NEW.salary_min, NEW.salary_max, NEW.currency, NEW.target_start, NEW.pass_needed, NEW.requested_by, NEW.requested_at)
       IS DISTINCT FROM (OLD.title, OLD.department, OLD.reason, OLD.replaces_person, OLD.salary_min, OLD.salary_max, OLD.currency, OLD.target_start, OLD.pass_needed, OLD.requested_by, OLD.requested_at) THEN
    RAISE EXCEPTION 'A requisition is never deleted, and what was asked for never changes' USING ERRCODE = 'NM213';
  END IF;
  IF NEW.state IS DISTINCT FROM OLD.state AND NOT ((OLD.state = 'requested' AND NEW.state IN ('approved', 'rejected', 'cancelled')) OR (OLD.state = 'approved' AND NEW.state IN ('filled', 'cancelled'))) THEN
    RAISE EXCEPTION 'A requisition moves forward only: requested, then approved or rejected, then filled or cancelled' USING ERRCODE = 'NM213';
  END IF;
  IF OLD.state IN ('rejected', 'filled', 'cancelled') THEN
    RAISE EXCEPTION 'A requisition that is rejected, filled or cancelled is final' USING ERRCODE = 'NM213';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER requisition_guard BEFORE UPDATE OR DELETE ON nemura.requisition FOR EACH ROW EXECUTE FUNCTION nemura.requisition_guard();

CREATE TABLE nemura.job_post (
  organisation_id    uuid        NOT NULL DEFAULT nemura.current_org(),
  id                 uuid        NOT NULL DEFAULT uuidv7(),
  requisition_id     uuid        NOT NULL,
  title              text        NOT NULL,
  body               text        NOT NULL,
  state              text        NOT NULL DEFAULT 'draft',
  requirement_note   text,                         -- the genuine occupational requirement recorded when a flagged criterion is kept
  published_on       date,
  closed_on          date,
  advert_posted_on   date,                         -- the day the advert went up on MyCareersFuture (done by a person; no posting API): the Fair Consideration Framework's 14 days run from it
  advert_reference   text,
  exemption          text,                         -- a recorded exemption from the advert, with its note
  exemption_note     text,
  created_by         uuid        NOT NULL,
  created_at         timestamptz NOT NULL,
  CONSTRAINT job_post_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT job_post_requisition_fkey FOREIGN KEY (organisation_id, requisition_id) REFERENCES nemura.requisition (organisation_id, id),
  CONSTRAINT job_post_by_fkey FOREIGN KEY (organisation_id, created_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT job_post_one_each UNIQUE (organisation_id, requisition_id),
  CONSTRAINT job_post_title_length CHECK (length(btrim(title)) BETWEEN 1 AND 120),
  CONSTRAINT job_post_body_length CHECK (length(btrim(body)) BETWEEN 1 AND 8000),
  CONSTRAINT job_post_state_known CHECK (state IN ('draft', 'published', 'closed')),
  CONSTRAINT job_post_published_whole CHECK ((state = 'draft') = (published_on IS NULL)),
  CONSTRAINT job_post_closed_whole CHECK ((state = 'closed') = (closed_on IS NOT NULL)),
  CONSTRAINT job_post_exemption_known CHECK (exemption IS NULL OR exemption IN ('fewer_than_10_employees', 'ep_salary_22500', 'role_one_month_or_less', 'transferee', 's_pass_exempt')),
  CONSTRAINT job_post_exemption_whole CHECK ((exemption IS NULL) = (exemption_note IS NULL)),
  CONSTRAINT job_post_note_length CHECK ((requirement_note IS NULL OR length(requirement_note) <= 1000) AND (exemption_note IS NULL OR length(exemption_note) <= 500) AND (advert_reference IS NULL OR length(advert_reference) <= 100))
);

-- A post's words change only while it is a draft; once published they never do. The evidence dates (the advert, an exemption) are recorded once each, and a post is closed once.
-- +goose StatementBegin
CREATE FUNCTION nemura.job_post_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A job post is never deleted: it is closed' USING ERRCODE = 'NM213';
  END IF;
  IF NEW.requisition_id IS DISTINCT FROM OLD.requisition_id OR NEW.created_by IS DISTINCT FROM OLD.created_by OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
    RAISE EXCEPTION 'A job post stays with its requisition' USING ERRCODE = 'NM213';
  END IF;
  IF OLD.state <> 'draft' AND (NEW.title, NEW.body, NEW.requirement_note, NEW.published_on) IS DISTINCT FROM (OLD.title, OLD.body, OLD.requirement_note, OLD.published_on) THEN
    RAISE EXCEPTION 'A published job post never changes' USING ERRCODE = 'NM213';
  END IF;
  IF OLD.state = 'closed' THEN
    RAISE EXCEPTION 'A closed job post is final' USING ERRCODE = 'NM213';
  END IF;
  IF (OLD.advert_posted_on IS NOT NULL AND NEW.advert_posted_on IS DISTINCT FROM OLD.advert_posted_on) OR (OLD.exemption IS NOT NULL AND NEW.exemption IS DISTINCT FROM OLD.exemption) THEN
    RAISE EXCEPTION 'The advert date and an exemption are recorded once' USING ERRCODE = 'NM213';
  END IF;
  IF NEW.state IS DISTINCT FROM OLD.state AND NOT ((OLD.state = 'draft' AND NEW.state = 'published') OR (OLD.state = 'published' AND NEW.state = 'closed')) THEN
    RAISE EXCEPTION 'A job post is a draft, then published, then closed' USING ERRCODE = 'NM213';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER job_post_guard BEFORE UPDATE OR DELETE ON nemura.job_post FOR EACH ROW EXECUTE FUNCTION nemura.job_post_guard();

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.requisition DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('requisition', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.job_post DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('job_post', 'id');

ALTER TABLE nemura.requisition ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.requisition FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.requisition USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.job_post ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.job_post FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.job_post USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());

REVOKE ALL ON nemura.requisition, nemura.job_post FROM PUBLIC;
GRANT SELECT ON nemura.requisition, nemura.job_post TO nemura_api, nemura_readonly;
GRANT INSERT (id, title, department, reason, replaces_person, salary_min, salary_max, currency, target_start, pass_needed, requested_by, requested_at), UPDATE (state, decided_by, decided_at, decision_reason) ON nemura.requisition TO nemura_api;
GRANT INSERT (id, requisition_id, title, body, created_by, created_at), UPDATE (title, body, state, requirement_note, published_on, closed_on, advert_posted_on, advert_reference, exemption, exemption_note) ON nemura.job_post TO nemura_api;
