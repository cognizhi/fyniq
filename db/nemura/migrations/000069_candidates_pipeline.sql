-- Hiring, the pipeline (PRD-002 NM-REC-04, NM-REC-05): candidates for a requisition with a profile, a stage on a board and every move kept; notes; interview kits with scorecards and the interviewers
-- assigned to each candidate; and the candidate's consent and retention.
--
-- A candidate has no sign-in and is not a person: a person record is made only at hire (design D2). Applying for a role is deemed consent to assessing the application (PDPC); keeping a candidate for
-- future roles is a separate consent that can be withdrawn. A candidate who is not progressing is deleted six months after the decision unless the future-roles consent stands, through the retention
-- engine's report, review and proof (NM-SEC-05). An interviewer reads only the candidates assigned to them; the notes and the scores are opinions formed to evaluate a candidate and are not released on
-- an access request.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE nemura.candidate (
  organisation_id             uuid        NOT NULL DEFAULT nemura.current_org(),
  id                          uuid        NOT NULL DEFAULT uuidv7(),
  requisition_id              uuid        NOT NULL,
  full_name                   text        NOT NULL,
  email                       text        NOT NULL,
  cv_text                     text,
  stage                       text        NOT NULL DEFAULT 'applied',
  stage_reason                text,
  stage_at                    timestamptz NOT NULL,
  applied_via                 text        NOT NULL,
  applied_at                  timestamptz NOT NULL,
  consent_future_at           timestamptz,   -- separate consent to keep the candidate for future roles
  consent_future_withdrawn_at timestamptz,
  created_by                  uuid,          -- null when the candidate applied on the careers page
  CONSTRAINT candidate_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT candidate_requisition_fkey FOREIGN KEY (organisation_id, requisition_id) REFERENCES nemura.requisition (organisation_id, id),
  CONSTRAINT candidate_by_fkey FOREIGN KEY (organisation_id, created_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT candidate_name_length CHECK (length(btrim(full_name)) BETWEEN 1 AND 120),
  CONSTRAINT candidate_email_form CHECK (email ~ '^[^@[:space:]]+@[^@[:space:]]+\.[^@[:space:]]+$' AND length(email) <= 200),
  CONSTRAINT candidate_cv_length CHECK (cv_text IS NULL OR length(cv_text) <= 20000),
  CONSTRAINT candidate_stage_known CHECK (stage IN ('applied', 'screening', 'interview', 'offer', 'hired', 'not_progressing')),
  CONSTRAINT candidate_reason_length CHECK (stage_reason IS NULL OR length(stage_reason) <= 500),
  CONSTRAINT candidate_via_known CHECK (applied_via IN ('careers_page', 'recruiter')),
  CONSTRAINT candidate_consent_whole CHECK (consent_future_withdrawn_at IS NULL OR consent_future_at IS NOT NULL)
);
CREATE INDEX candidate_requisition_idx ON nemura.candidate (organisation_id, requisition_id, stage);

CREATE TABLE nemura.candidate_move (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  candidate_id    uuid        NOT NULL,
  from_stage      text,
  to_stage        text        NOT NULL,
  reason          text,
  by_member       uuid,
  at              timestamptz NOT NULL,
  CONSTRAINT candidate_move_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT candidate_move_candidate_fkey FOREIGN KEY (organisation_id, candidate_id) REFERENCES nemura.candidate (organisation_id, id),
  CONSTRAINT candidate_move_by_fkey FOREIGN KEY (organisation_id, by_member) REFERENCES nemura.member (organisation_id, id)
);

CREATE TABLE nemura.candidate_note (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  candidate_id    uuid        NOT NULL,
  body            text        NOT NULL,
  by_member       uuid        NOT NULL,
  at              timestamptz NOT NULL,
  CONSTRAINT candidate_note_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT candidate_note_candidate_fkey FOREIGN KEY (organisation_id, candidate_id) REFERENCES nemura.candidate (organisation_id, id),
  CONSTRAINT candidate_note_by_fkey FOREIGN KEY (organisation_id, by_member) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT candidate_note_length CHECK (length(btrim(body)) BETWEEN 1 AND 4000)
);

CREATE TABLE nemura.candidate_interviewer (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  candidate_id    uuid        NOT NULL,
  member_id       uuid        NOT NULL,
  assigned_by     uuid        NOT NULL,
  assigned_at     timestamptz NOT NULL,
  CONSTRAINT candidate_interviewer_pkey PRIMARY KEY (organisation_id, candidate_id, member_id),
  CONSTRAINT candidate_interviewer_candidate_fkey FOREIGN KEY (organisation_id, candidate_id) REFERENCES nemura.candidate (organisation_id, id),
  CONSTRAINT candidate_interviewer_member_fkey FOREIGN KEY (organisation_id, member_id) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT candidate_interviewer_by_fkey FOREIGN KEY (organisation_id, assigned_by) REFERENCES nemura.member (organisation_id, id)
);

-- One kit for each requisition: the questions and the scorecard's criteria. A question that names a protected criterion is kept only with the genuine occupational requirement recorded.
CREATE TABLE nemura.interview_kit (
  organisation_id  uuid        NOT NULL DEFAULT nemura.current_org(),
  requisition_id   uuid        NOT NULL,
  questions        text[]      NOT NULL,
  criteria         text[]      NOT NULL,
  requirement_note text,
  updated_by       uuid        NOT NULL,
  updated_at       timestamptz NOT NULL,
  CONSTRAINT interview_kit_pkey PRIMARY KEY (organisation_id, requisition_id),
  CONSTRAINT interview_kit_requisition_fkey FOREIGN KEY (organisation_id, requisition_id) REFERENCES nemura.requisition (organisation_id, id),
  CONSTRAINT interview_kit_by_fkey FOREIGN KEY (organisation_id, updated_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT interview_kit_sizes CHECK (cardinality(questions) BETWEEN 1 AND 30 AND cardinality(criteria) BETWEEN 1 AND 10),
  CONSTRAINT interview_kit_note_length CHECK (requirement_note IS NULL OR length(requirement_note) <= 1000)
);

CREATE TABLE nemura.interview_score (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  candidate_id    uuid        NOT NULL,
  member_id       uuid        NOT NULL,
  criterion       text        NOT NULL,
  score           smallint    NOT NULL,
  comment         text,
  at              timestamptz NOT NULL,
  CONSTRAINT interview_score_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT interview_score_candidate_fkey FOREIGN KEY (organisation_id, candidate_id) REFERENCES nemura.candidate (organisation_id, id),
  CONSTRAINT interview_score_member_fkey FOREIGN KEY (organisation_id, member_id) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT interview_score_range CHECK (score BETWEEN 1 AND 5),
  CONSTRAINT interview_score_comment_length CHECK (comment IS NULL OR length(comment) <= 1000),
  CONSTRAINT interview_score_criterion_length CHECK (length(btrim(criterion)) BETWEEN 1 AND 120)
);

-- The candidate: who and what never change; the stage moves along the board, never backwards from hired or not progressing (a candidate not progressing may be reopened to screening by the recruiter).
-- Only the retention purge, which runs as the migrator's own functions, deletes one, and only when it is not progressing.
-- +goose StatementBegin
CREATE FUNCTION nemura.candidate_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    IF current_user = 'nemura_migrator' AND OLD.stage = 'not_progressing' THEN
      RETURN OLD; -- only purge_candidates runs as its owner, and it checks the retention period, the consent and the holds first
    END IF;
    RAISE EXCEPTION 'A candidate is kept until the retention period has run: only a candidate who is not progressing is deleted, by the retention job' USING ERRCODE = 'NM215';
  END IF;
  IF (NEW.organisation_id, NEW.id, NEW.requisition_id, NEW.full_name, NEW.email, NEW.cv_text, NEW.applied_via, NEW.applied_at, NEW.created_by) IS DISTINCT FROM
     (OLD.organisation_id, OLD.id, OLD.requisition_id, OLD.full_name, OLD.email, OLD.cv_text, OLD.applied_via, OLD.applied_at, OLD.created_by) THEN
    RAISE EXCEPTION 'What a candidate gave never changes' USING ERRCODE = 'NM215';
  END IF;
  IF NEW.stage IS DISTINCT FROM OLD.stage AND NOT (
       (OLD.stage = 'applied' AND NEW.stage IN ('screening', 'interview', 'not_progressing'))
    OR (OLD.stage = 'screening' AND NEW.stage IN ('interview', 'not_progressing'))
    OR (OLD.stage = 'interview' AND NEW.stage IN ('offer', 'not_progressing'))
    OR (OLD.stage = 'offer' AND NEW.stage IN ('hired', 'not_progressing', 'interview'))
    OR (OLD.stage = 'not_progressing' AND NEW.stage = 'screening')) THEN
    RAISE EXCEPTION 'A candidate moves along applied, screening, interview, offer and hired, or is not progressing' USING ERRCODE = 'NM215';
  END IF;
  IF OLD.stage = 'hired' THEN
    RAISE EXCEPTION 'A hired candidate is final: the person record carries on' USING ERRCODE = 'NM215';
  END IF;
  IF OLD.consent_future_at IS NOT NULL AND NEW.consent_future_at IS DISTINCT FROM OLD.consent_future_at THEN
    RAISE EXCEPTION 'The date of a consent never changes; it is withdrawn' USING ERRCODE = 'NM215';
  END IF;
  IF OLD.consent_future_withdrawn_at IS NOT NULL AND NEW.consent_future_withdrawn_at IS DISTINCT FROM OLD.consent_future_withdrawn_at THEN
    RAISE EXCEPTION 'A consent is withdrawn once' USING ERRCODE = 'NM215';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER candidate_guard BEFORE UPDATE OR DELETE ON nemura.candidate FOR EACH ROW EXECUTE FUNCTION nemura.candidate_guard();

-- +goose StatementBegin
CREATE FUNCTION nemura.candidate_append_only() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' AND current_user = 'nemura_migrator' THEN
    RETURN OLD; -- the retention purge deletes a candidate's history with the candidate
  END IF;
  RAISE EXCEPTION 'A candidate''s moves, notes and scores are kept as written' USING ERRCODE = 'NM215';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER append_only BEFORE UPDATE OR DELETE ON nemura.candidate_move FOR EACH ROW EXECUTE FUNCTION nemura.candidate_append_only();
CREATE TRIGGER append_only BEFORE UPDATE OR DELETE ON nemura.candidate_note FOR EACH ROW EXECUTE FUNCTION nemura.candidate_append_only();
CREATE TRIGGER append_only BEFORE UPDATE OR DELETE ON nemura.interview_score FOR EACH ROW EXECUTE FUNCTION nemura.candidate_append_only();
CREATE TRIGGER append_only BEFORE UPDATE OR DELETE ON nemura.candidate_interviewer FOR EACH ROW EXECUTE FUNCTION nemura.candidate_append_only();

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.candidate DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('candidate', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON nemura.candidate_move DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('candidate', 'candidate_id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON nemura.candidate_note DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('candidate', 'candidate_id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON nemura.candidate_interviewer DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('candidate', 'candidate_id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON nemura.interview_score DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('candidate', 'candidate_id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.interview_kit DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('requisition', 'requisition_id');

-- Who reads a candidate: the hiring team (the Recruiter, the HR officer, the HR administrator and the Owner) and an interviewer assigned to the candidate (Appendix B: Hiring). The assignment
-- is a row an interviewer cannot make. A candidate who applies on the careers page is inserted with no acting person, which only the careers application does.
-- +goose StatementBegin
CREATE FUNCTION nemura.hiring_team() RETURNS boolean
LANGUAGE sql STABLE
SET search_path = pg_catalog, nemura, pg_temp
AS $$
  SELECT nemura.has_role('recruiter') OR nemura.has_role('hr_officer') OR nemura.has_role('hr_admin') OR nemura.has_role('owner')
$$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION nemura.is_interviewer_of(p_candidate uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$
  SELECT EXISTS (SELECT 1 FROM nemura.candidate_interviewer i WHERE i.organisation_id = nemura.current_org() AND i.candidate_id = p_candidate AND i.member_id = nemura.current_person())
$$;
-- +goose StatementEnd
GRANT EXECUTE ON FUNCTION nemura.hiring_team(), nemura.is_interviewer_of(uuid) TO nemura_api;

ALTER TABLE nemura.candidate ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.candidate FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.candidate USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
CREATE POLICY hiring_scope ON nemura.candidate AS RESTRICTIVE FOR SELECT TO nemura_api USING (nemura.hiring_team() OR nemura.is_interviewer_of(id));
CREATE POLICY hiring_scope_write ON nemura.candidate AS RESTRICTIVE FOR UPDATE TO nemura_api USING (nemura.hiring_team()) WITH CHECK (nemura.hiring_team());
CREATE POLICY hiring_scope_insert ON nemura.candidate AS RESTRICTIVE FOR INSERT TO nemura_api WITH CHECK (nemura.hiring_team() OR nemura.current_person() IS NULL);

ALTER TABLE nemura.candidate_move ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.candidate_move FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.candidate_move USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
CREATE POLICY hiring_scope ON nemura.candidate_move AS RESTRICTIVE FOR ALL TO nemura_api USING (nemura.hiring_team() OR nemura.current_person() IS NULL) WITH CHECK (nemura.hiring_team() OR nemura.current_person() IS NULL);

-- The notes are the hiring team's alone: an interviewer reads the candidate and writes scores, not the team's notes.
ALTER TABLE nemura.candidate_note ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.candidate_note FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.candidate_note USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
CREATE POLICY hiring_scope ON nemura.candidate_note AS RESTRICTIVE FOR ALL TO nemura_api USING (nemura.hiring_team()) WITH CHECK (nemura.hiring_team());

ALTER TABLE nemura.candidate_interviewer ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.candidate_interviewer FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.candidate_interviewer USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
CREATE POLICY hiring_scope ON nemura.candidate_interviewer AS RESTRICTIVE FOR ALL TO nemura_api USING (nemura.hiring_team() OR member_id = nemura.current_person()) WITH CHECK (nemura.hiring_team());

ALTER TABLE nemura.interview_kit ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.interview_kit FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.interview_kit USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());

-- A score is read by the hiring team and by the interviewer who gave it; only an interviewer assigned to the candidate writes one, and only as themselves.
ALTER TABLE nemura.interview_score ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.interview_score FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.interview_score USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
CREATE POLICY reads ON nemura.interview_score AS RESTRICTIVE FOR SELECT TO nemura_api USING (nemura.hiring_team() OR member_id = nemura.current_person());
CREATE POLICY writes ON nemura.interview_score AS RESTRICTIVE FOR INSERT TO nemura_api WITH CHECK (member_id = nemura.current_person() AND nemura.is_interviewer_of(candidate_id));

REVOKE ALL ON nemura.candidate, nemura.candidate_move, nemura.candidate_note, nemura.candidate_interviewer, nemura.interview_kit, nemura.interview_score FROM PUBLIC;
GRANT SELECT ON nemura.candidate, nemura.candidate_move, nemura.candidate_note, nemura.candidate_interviewer, nemura.interview_kit, nemura.interview_score TO nemura_api, nemura_readonly;
GRANT INSERT (id, requisition_id, full_name, email, cv_text, stage_at, applied_via, applied_at, consent_future_at, created_by), UPDATE (stage, stage_reason, stage_at, consent_future_at, consent_future_withdrawn_at) ON nemura.candidate TO nemura_api;
GRANT INSERT (id, candidate_id, from_stage, to_stage, reason, by_member, at) ON nemura.candidate_move TO nemura_api;
GRANT INSERT (id, candidate_id, body, by_member, at) ON nemura.candidate_note TO nemura_api;
GRANT INSERT (candidate_id, member_id, assigned_by, assigned_at) ON nemura.candidate_interviewer TO nemura_api;
GRANT INSERT (requisition_id, questions, criteria, requirement_note, updated_by, updated_at), UPDATE (questions, criteria, requirement_note, updated_by, updated_at) ON nemura.interview_kit TO nemura_api;
GRANT INSERT (id, candidate_id, member_id, criterion, score, comment, at) ON nemura.interview_score TO nemura_api;

-- The retention: a candidate who is not progressing, six months from the day the decision was made, unless the future-roles consent stands. What the retention engine reads is the identity, the day
-- and a fingerprint; the purge deletes only a candidate that is due, and refuses the whole call otherwise.
-- +goose StatementBegin
CREATE FUNCTION nemura.candidates_for_retention() RETURNS TABLE (id uuid, decided_on date, fingerprint bytea)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$
  SELECT c.id, greatest(c.stage_at::date, coalesce(c.consent_future_withdrawn_at::date, c.stage_at::date)), sha256(convert_to(c.full_name || '|' || c.email, 'UTF8'))
    FROM nemura.candidate c
   WHERE c.organisation_id = nemura.current_org() AND c.stage = 'not_progressing'
     AND NOT (c.consent_future_at IS NOT NULL AND c.consent_future_withdrawn_at IS NULL)
$$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION nemura.purge_candidates(p_ids uuid[], p_on date) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$
DECLARE
  n integer := 0;
  c nemura.candidate;
BEGIN
  FOR c IN SELECT * FROM nemura.candidate WHERE organisation_id = nemura.current_org() AND id = ANY (p_ids) FOR UPDATE LOOP
    IF c.stage <> 'not_progressing' OR (c.consent_future_at IS NOT NULL AND c.consent_future_withdrawn_at IS NULL) OR greatest(c.stage_at::date, coalesce(c.consent_future_withdrawn_at::date, c.stage_at::date)) > p_on - 180 THEN
      RAISE EXCEPTION 'A candidate is deleted only six months after the decision, when not progressing and with no consent to keep them' USING ERRCODE = 'NM215';
    END IF;
    DELETE FROM nemura.interview_score WHERE organisation_id = c.organisation_id AND candidate_id = c.id;
    DELETE FROM nemura.candidate_interviewer WHERE organisation_id = c.organisation_id AND candidate_id = c.id;
    DELETE FROM nemura.candidate_note WHERE organisation_id = c.organisation_id AND candidate_id = c.id;
    DELETE FROM nemura.candidate_move WHERE organisation_id = c.organisation_id AND candidate_id = c.id;
    DELETE FROM nemura.candidate WHERE organisation_id = c.organisation_id AND id = c.id;
    n := n + 1;
  END LOOP;
  RETURN n;
END
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION nemura.candidates_for_retention(), nemura.purge_candidates(uuid[], date) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION nemura.candidates_for_retention(), nemura.purge_candidates(uuid[], date) TO nemura_worker;

-- The careers application: which organisation an open, published post belongs to, for a visitor who has none in hand. It names the organisation only for a page that is switched on and a post that is
-- published and approved there; any other pair answers no row.
-- +goose StatementBegin
CREATE FUNCTION nemura.careers_application_target(p_slug text, p_post uuid) RETURNS TABLE (organisation_id uuid, requisition_id uuid)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$
DECLARE o uuid;
BEGIN
  SELECT d.organisation_id INTO o FROM nemura.careers_directory d WHERE d.slug = p_slug AND d.enabled;
  IF o IS NULL THEN
    RETURN;
  END IF;
  PERFORM pg_catalog.set_config('app.org_id', o::text, true);
  RETURN QUERY SELECT j.organisation_id, j.requisition_id FROM nemura.job_post j JOIN nemura.requisition r ON r.organisation_id = j.organisation_id AND r.id = j.requisition_id
     WHERE j.organisation_id = o AND j.id = p_post AND j.state = 'published' AND r.state = 'approved';
  PERFORM pg_catalog.set_config('app.org_id', '', true);
END
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION nemura.careers_application_target(text, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION nemura.careers_application_target(text, uuid) TO nemura_api;
