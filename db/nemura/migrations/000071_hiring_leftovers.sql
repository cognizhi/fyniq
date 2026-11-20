-- Hiring, what NM-REC-04 and NM-REC-05 left (PRD-002 rev 0.74): the line manager reads the roles they are hiring for; a candidate looks at their own data through a link the hiring team makes; and
-- a hired candidate's record (CV, notes, scores, interviewers) is deleted six months after the start date through the retention engine, as an unsuccessful candidate's is [owner to confirm].
--
-- The line manager: a requisition may name a hiring manager, a member, once, when it is asked for. The line manager reads only the requisitions that name them, through one narrow function that
-- returns the role, its state, its post's dates and how many candidates stand at each stage: no salary, no name, no word of a candidate.
--
-- The candidate: a candidate has no sign-in and is not a person. The hiring team makes a link for one candidate (an access request under the PDPA); the token is kept as a hash, expires (30 days
-- at most) and is revoked once. With the token and no sign-in, two functions answer: what the candidate gave and where they stand, never the team's notes or scores (opinions formed to evaluate a
-- candidate are not released on an access request), and the withdrawal of the consent to be kept for future roles.
--
-- A hired candidate: the person record carries on; the hire row stays as the tie between the person and the day they were hired, so its foreign key to the candidate goes (the id is kept; the
-- candidate is not).

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- The hiring manager.
ALTER TABLE nemura.requisition ADD COLUMN hiring_manager uuid;
ALTER TABLE nemura.requisition ADD CONSTRAINT requisition_manager_fkey FOREIGN KEY (organisation_id, hiring_manager) REFERENCES nemura.member (organisation_id, id) NOT VALID;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION nemura.requisition_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR (NEW.title, NEW.department, NEW.reason, NEW.replaces_person, NEW.salary_min, NEW.salary_max, NEW.currency, NEW.target_start, NEW.pass_needed, NEW.requested_by, NEW.requested_at, NEW.hiring_manager)
       IS DISTINCT FROM (OLD.title, OLD.department, OLD.reason, OLD.replaces_person, OLD.salary_min, OLD.salary_max, OLD.currency, OLD.target_start, OLD.pass_needed, OLD.requested_by, OLD.requested_at, OLD.hiring_manager) THEN
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
GRANT INSERT (hiring_manager) ON nemura.requisition TO nemura_api;

-- The line manager's own roles: only the requisitions that name the caller, with counts by stage.
-- +goose StatementBegin
CREATE FUNCTION nemura.my_hiring_roles() RETURNS TABLE (id uuid, title text, department text, state text, target_start date, pass_needed boolean, post_state text, published_on date, closed_on date, applied integer, screening integer, interview integer, offer integer, hired integer, not_progressing integer)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$
  SELECT r.id, r.title, r.department, r.state, r.target_start, r.pass_needed, j.state, j.published_on, j.closed_on,
         (SELECT count(*)::integer FROM nemura.candidate c WHERE c.organisation_id = r.organisation_id AND c.requisition_id = r.id AND c.stage = 'applied'),
         (SELECT count(*)::integer FROM nemura.candidate c WHERE c.organisation_id = r.organisation_id AND c.requisition_id = r.id AND c.stage = 'screening'),
         (SELECT count(*)::integer FROM nemura.candidate c WHERE c.organisation_id = r.organisation_id AND c.requisition_id = r.id AND c.stage = 'interview'),
         (SELECT count(*)::integer FROM nemura.candidate c WHERE c.organisation_id = r.organisation_id AND c.requisition_id = r.id AND c.stage = 'offer'),
         (SELECT count(*)::integer FROM nemura.candidate c WHERE c.organisation_id = r.organisation_id AND c.requisition_id = r.id AND c.stage = 'hired'),
         (SELECT count(*)::integer FROM nemura.candidate c WHERE c.organisation_id = r.organisation_id AND c.requisition_id = r.id AND c.stage = 'not_progressing')
    FROM nemura.requisition r LEFT JOIN nemura.job_post j ON j.organisation_id = r.organisation_id AND j.requisition_id = r.id
   WHERE r.organisation_id = nemura.current_org() AND nemura.current_person() IS NOT NULL AND r.hiring_manager = nemura.current_person()
   ORDER BY r.requested_at DESC, r.id
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION nemura.my_hiring_roles() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION nemura.my_hiring_roles() TO nemura_api;

-- A hired candidate's record is deleted after the retention period like any other: the hire row stays, without a key to a record that goes.
ALTER TABLE nemura.hire DROP CONSTRAINT hire_candidate_fkey;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION nemura.candidate_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    IF current_user = 'nemura_migrator' AND OLD.stage IN ('not_progressing', 'hired') THEN
      RETURN OLD; -- only purge_candidates runs as its owner, and it checks the retention period, the consent and the start date first
    END IF;
    RAISE EXCEPTION 'A candidate is kept until the retention period has run: only a candidate who is not progressing or was hired is deleted, by the retention job' USING ERRCODE = 'NM215';
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

-- A candidate's access link.
CREATE TABLE nemura.candidate_access (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  candidate_id    uuid        NOT NULL,
  token_hash      bytea       NOT NULL,
  expires_at      timestamptz NOT NULL,
  created_by      uuid        NOT NULL,
  created_at      timestamptz NOT NULL,
  revoked_at      timestamptz,
  CONSTRAINT candidate_access_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT candidate_access_candidate_fkey FOREIGN KEY (organisation_id, candidate_id) REFERENCES nemura.candidate (organisation_id, id),
  CONSTRAINT candidate_access_by_fkey FOREIGN KEY (organisation_id, created_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT candidate_access_token CHECK (octet_length(token_hash) = 32),
  CONSTRAINT candidate_access_token_once UNIQUE (token_hash),
  CONSTRAINT candidate_access_expiry CHECK (expires_at > created_at AND expires_at <= created_at + interval '30 days')
);
CREATE INDEX candidate_access_candidate_idx ON nemura.candidate_access (organisation_id, candidate_id);
-- +goose StatementBegin
CREATE FUNCTION nemura.candidate_access_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    IF current_user = 'nemura_migrator' THEN
      RETURN OLD; -- the retention purge deletes a candidate's links with the candidate
    END IF;
    RAISE EXCEPTION 'An access link is kept: it is revoked, never deleted' USING ERRCODE = 'NM217';
  END IF;
  IF OLD.revoked_at IS NOT NULL OR NEW.revoked_at IS NULL
     OR (NEW.organisation_id, NEW.id, NEW.candidate_id, NEW.token_hash, NEW.expires_at, NEW.created_by, NEW.created_at) IS DISTINCT FROM
        (OLD.organisation_id, OLD.id, OLD.candidate_id, OLD.token_hash, OLD.expires_at, OLD.created_by, OLD.created_at) THEN
    RAISE EXCEPTION 'An access link changes only by being revoked, once' USING ERRCODE = 'NM217';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER candidate_access_guard BEFORE UPDATE OR DELETE ON nemura.candidate_access FOR EACH ROW EXECUTE FUNCTION nemura.candidate_access_guard();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.candidate_access DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('candidate', 'candidate_id');
ALTER TABLE nemura.candidate_access ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.candidate_access FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.candidate_access USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
CREATE POLICY hiring_scope ON nemura.candidate_access AS RESTRICTIVE FOR ALL TO nemura_api USING (nemura.hiring_team()) WITH CHECK (nemura.hiring_team());
REVOKE ALL ON nemura.candidate_access FROM PUBLIC;
GRANT SELECT ON nemura.candidate_access TO nemura_api;
GRANT INSERT (id, candidate_id, token_hash, expires_at, created_by, created_at), UPDATE (revoked_at) ON nemura.candidate_access TO nemura_api;

-- The functions take the token and hash it themselves, so the stored hash (which the hiring team can read) opens nothing.
-- What a live token shows with no sign-in: what the candidate gave and where they stand; nothing the team wrote. A token that is unknown, expired or revoked answers no row, all alike, and so does
-- one made for another organisation than the one the caller set.
-- +goose StatementBegin
CREATE FUNCTION nemura.candidate_access_view(p_token text, p_now timestamptz) RETURNS TABLE (access_id uuid, candidate_id uuid, full_name text, email text, cv_text text, stage text, stage_at timestamptz, applied_at timestamptz, applied_via text, consent_future_at timestamptz, consent_future_withdrawn_at timestamptz, role_title text, hire_start date, expires_at timestamptz)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$
  SELECT a.id, c.id, c.full_name, c.email, c.cv_text, c.stage, c.stage_at, c.applied_at, c.applied_via, c.consent_future_at, c.consent_future_withdrawn_at, r.title, h.start_date, a.expires_at
    FROM nemura.candidate_access a
    JOIN nemura.candidate c ON c.organisation_id = a.organisation_id AND c.id = a.candidate_id
    JOIN nemura.requisition r ON r.organisation_id = c.organisation_id AND r.id = c.requisition_id
    LEFT JOIN nemura.hire h ON h.organisation_id = c.organisation_id AND h.candidate_id = c.id
   WHERE a.organisation_id = nemura.current_org() AND a.token_hash = sha256(convert_to('nemura-candidate-access|' || p_token, 'UTF8')) AND a.revoked_at IS NULL AND a.expires_at > p_now
$$;
-- +goose StatementEnd
-- The withdrawal of the consent to be kept for future roles, by the candidate: once, and only after it was given. It answers the candidate's ID, or no row.
-- +goose StatementBegin
CREATE FUNCTION nemura.candidate_access_withdraw(p_token text, p_now timestamptz) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$
DECLARE
  cid uuid;
BEGIN
  SELECT a.candidate_id INTO cid FROM nemura.candidate_access a
   WHERE a.organisation_id = nemura.current_org() AND a.token_hash = sha256(convert_to('nemura-candidate-access|' || p_token, 'UTF8')) AND a.revoked_at IS NULL AND a.expires_at > p_now;
  IF cid IS NULL THEN
    RETURN NULL;
  END IF;
  UPDATE nemura.candidate SET consent_future_withdrawn_at = p_now
   WHERE organisation_id = nemura.current_org() AND id = cid AND consent_future_at IS NOT NULL AND consent_future_withdrawn_at IS NULL;
  IF NOT FOUND THEN
    RETURN NULL;
  END IF;
  RETURN cid;
END
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION nemura.candidate_access_view(text, timestamptz), nemura.candidate_access_withdraw(text, timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION nemura.candidate_access_view(text, timestamptz), nemura.candidate_access_withdraw(text, timestamptz) TO nemura_api;

-- The retention: those not progressing, six months from the decision, unless the future-roles consent stands; and those hired, six months from the start date.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION nemura.candidates_for_retention() RETURNS TABLE (id uuid, decided_on date, fingerprint bytea)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$
  SELECT c.id, greatest(c.stage_at::date, coalesce(c.consent_future_withdrawn_at::date, c.stage_at::date)), sha256(convert_to(c.full_name || '|' || c.email, 'UTF8'))
    FROM nemura.candidate c
   WHERE c.organisation_id = nemura.current_org() AND c.stage = 'not_progressing'
     AND NOT (c.consent_future_at IS NOT NULL AND c.consent_future_withdrawn_at IS NULL)
  UNION ALL
  SELECT c.id, h.start_date, sha256(convert_to(c.full_name || '|' || c.email, 'UTF8'))
    FROM nemura.candidate c JOIN nemura.hire h ON h.organisation_id = c.organisation_id AND h.candidate_id = c.id
   WHERE c.organisation_id = nemura.current_org() AND c.stage = 'hired'
$$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION nemura.purge_candidates(p_ids uuid[], p_on date) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$
DECLARE
  n integer := 0;
  c nemura.candidate;
  started date;
BEGIN
  FOR c IN SELECT * FROM nemura.candidate WHERE organisation_id = nemura.current_org() AND id = ANY (p_ids) FOR UPDATE LOOP
    IF c.stage = 'hired' THEN
      SELECT h.start_date INTO started FROM nemura.hire h WHERE h.organisation_id = c.organisation_id AND h.candidate_id = c.id;
      IF started IS NULL OR started > p_on - 180 THEN
        RAISE EXCEPTION 'A hired candidate is deleted only six months after the start date' USING ERRCODE = 'NM215';
      END IF;
    ELSIF c.stage <> 'not_progressing' OR (c.consent_future_at IS NOT NULL AND c.consent_future_withdrawn_at IS NULL) OR greatest(c.stage_at::date, coalesce(c.consent_future_withdrawn_at::date, c.stage_at::date)) > p_on - 180 THEN
      RAISE EXCEPTION 'A candidate is deleted only six months after the decision, when not progressing and with no consent to keep them' USING ERRCODE = 'NM215';
    END IF;
    DELETE FROM nemura.candidate_access WHERE organisation_id = c.organisation_id AND candidate_id = c.id;
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
