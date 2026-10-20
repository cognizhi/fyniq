-- Approval policies as versioned, approval-gated data (PRD-001 FR-APR-01, FR-APR-09, FR-APR-10, SOD-8, rev 0.40;
-- p1-approval-engine tasks 2.1, 2.2).
--
-- A policy says who signs a request for an object type: ordered steps, each a quorum of people who hold the
-- approve right (A) on the object's Appendix D row, narrowed to some roles or to named members. A policy never
-- adds anyone Appendix D does not. Until an organisation changes a policy, the built-in one applies: one step,
-- quorum one. A change is itself an approval request (approval_policy.change), approved under the policy for
-- approval policies, which is fixed so no change can lock an organisation out of changing its policies; the
-- approved change adds the policy's next version. A version is never changed or deleted, and exists only with
-- the approved request that holds exactly its steps.
--
-- A request keeps the steps of the policy it started under (policy_steps), and the database approves it only when
-- every step has its quorum of approvals, signed in order. Adding a policy version voids every pending request of
-- its object type: a policy change restarts them (FR-APR-10). The checks on the request table are widened here
-- NOT VALID and validated by 000040 in a transaction of their own (squawk: constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- The steps of a policy: one to five objects, each with exactly a quorum of 1 to 5, a list of roles and a list of
-- member user IDs, never both lists filled.
-- +goose StatementBegin
CREATE FUNCTION fyniq.approval_steps_valid(p_steps jsonb) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  s jsonb;
BEGIN
  IF p_steps IS NULL OR jsonb_typeof(p_steps) <> 'array' OR jsonb_array_length(p_steps) NOT BETWEEN 1 AND 5 THEN
    RETURN false;
  END IF;
  FOR s IN SELECT value FROM jsonb_array_elements(p_steps) LOOP
    IF jsonb_typeof(s) <> 'object' OR jsonb_typeof(s -> 'quorum') IS DISTINCT FROM 'number' OR (s ->> 'quorum') !~ '^[1-5]$'
       OR jsonb_typeof(s -> 'roles') IS DISTINCT FROM 'array' OR jsonb_typeof(s -> 'users') IS DISTINCT FROM 'array' THEN
      RETURN false;
    END IF;
    IF EXISTS (SELECT 1 FROM jsonb_object_keys(s) k WHERE k NOT IN ('quorum', 'roles', 'users'))
       OR (jsonb_array_length(s -> 'roles') > 0 AND jsonb_array_length(s -> 'users') > 0)
       OR EXISTS (SELECT 1 FROM jsonb_array_elements(s -> 'roles') r WHERE jsonb_typeof(r) <> 'string' OR (r #>> '{}') !~ '^[a-z_]{1,40}$')
       OR EXISTS (SELECT 1 FROM jsonb_array_elements(s -> 'users') u
                   WHERE jsonb_typeof(u) <> 'string' OR (u #>> '{}') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$') THEN
      RETURN false;
    END IF;
  END LOOP;
  RETURN true;
END
$$;
-- +goose StatementEnd

CREATE TABLE fyniq.approval_policy (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  object_type     text        NOT NULL,
  version         integer     NOT NULL,
  steps           jsonb       NOT NULL,
  request_id      uuid        NOT NULL,
  created_by      uuid        NOT NULL,
  created_at      timestamptz NOT NULL,
  CONSTRAINT approval_policy_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT approval_policy_version_once UNIQUE (organisation_id, object_type, version),
  CONSTRAINT approval_policy_request_once UNIQUE (organisation_id, request_id),
  CONSTRAINT approval_policy_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT approval_policy_request_fkey FOREIGN KEY (organisation_id, request_id)
    REFERENCES fyniq.approval_request (organisation_id, id),
  CONSTRAINT approval_policy_created_by_fkey FOREIGN KEY (created_by) REFERENCES fyniq.app_user (user_id),
  -- The policy for approval policies is fixed: no organisation changes who approves a policy change.
  CONSTRAINT approval_policy_object_known CHECK (object_type IN ('organisation_setting', 'membership', 'member_invitation')),
  CONSTRAINT approval_policy_version_range CHECK (version BETWEEN 1 AND 1000000),
  CONSTRAINT approval_policy_steps_valid CHECK (fyniq.approval_steps_valid(steps))
);

-- A version is added once, as the next, by the approved request that holds exactly it; it never changes.
-- +goose StatementBegin
CREATE FUNCTION fyniq.approval_policy_guard() RETURNS trigger
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
     OR (r.change -> 'execute' ->> 'version') IS DISTINCT FROM NEW.version::text THEN
    RAISE EXCEPTION 'A version of an approval policy exists only with the approved request that holds exactly its steps'
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
CREATE TRIGGER approval_policy_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.approval_policy
  FOR EACH ROW EXECUTE FUNCTION fyniq.approval_policy_guard();

-- A new version restarts every pending request of its object type: they are voided, and audited by the unit of
-- work that added the version (require_audit). It runs as the function's owner, like approval_void_pending.
-- +goose StatementBegin
CREATE FUNCTION fyniq.approval_void_pending_type(p_type text, p_reason text) RETURNS void
LANGUAGE sql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  UPDATE fyniq.approval_request r
     SET status = 'void', decided_at = now(), status_reason = p_reason, version = r.version + 1,
         voided_xact = pg_current_xact_id()::text::bigint
   WHERE r.organisation_id = fyniq.current_org() AND r.object_type = p_type AND r.status = 'pending';
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION fyniq.approval_policy_voids() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  PERFORM fyniq.approval_void_pending_type(NEW.object_type, 'the approval policy changed after the request');
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER approval_policy_voids AFTER INSERT ON fyniq.approval_policy
  FOR EACH ROW EXECUTE FUNCTION fyniq.approval_policy_voids();

ALTER TABLE fyniq.approval_policy ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.approval_policy FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.approval_policy
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.approval_policy
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('approval_policy', 'id');

-- Only the API adds a version, when it executes an approved change; every role reads. Nobody changes or deletes.
GRANT SELECT ON fyniq.approval_policy TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
GRANT INSERT (object_type, version, steps, request_id, created_by, created_at) ON fyniq.approval_policy TO fyniq_api;
REVOKE ALL ON FUNCTION fyniq.approval_void_pending_type(text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.approval_void_pending_type(text, text) TO fyniq_api;
-- The checks call it as the role that writes: the API.
GRANT EXECUTE ON FUNCTION fyniq.approval_steps_valid(jsonb) TO fyniq_api;

-- ------------------------------------------------------------------ the requests keep their policy's steps

-- The steps of the policy a request started under. The requests before this migration were made under the built-in
-- policies, which are one step with a quorum of one.
ALTER TABLE fyniq.approval_request
  ADD COLUMN policy_steps jsonb NOT NULL DEFAULT '[{"quorum": 1, "roles": [], "users": []}]';
ALTER TABLE fyniq.approval_request
  ADD CONSTRAINT approval_request_policy_steps_valid CHECK (fyniq.approval_steps_valid(policy_steps)) NOT VALID;

-- A policy change is an object the engine knows.
ALTER TABLE fyniq.approval_request DROP CONSTRAINT approval_request_object_known;
ALTER TABLE fyniq.approval_request ADD CONSTRAINT approval_request_object_known CHECK (
  (object_type, operation) IN (('organisation_setting', 'organisation_setting.change'), ('membership', 'role.assign'),
                               ('membership', 'role.revoke'), ('member_invitation', 'member_invitation.create'),
                               ('approval_policy', 'approval_policy.change'))) NOT VALID;

GRANT INSERT (policy_steps) ON fyniq.approval_request TO fyniq_api;

-- approval_steps_met reports whether each of a request's first p_upto steps has its quorum of approvals of its
-- envelope, not voided.
-- +goose StatementBegin
CREATE FUNCTION fyniq.approval_steps_met(p_org uuid, p_request uuid, p_hash bytea, p_steps jsonb, p_upto integer) RETURNS boolean
LANGUAGE sql STABLE
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT NOT EXISTS (
    SELECT 1 FROM jsonb_array_elements(p_steps) WITH ORDINALITY AS st(step, n)
     WHERE st.n <= p_upto
       AND (SELECT count(*) FROM fyniq.approval_signature s
             WHERE s.organisation_id = p_org AND s.request_id = p_request AND s.step = st.n AND s.decision = 'approve'
               AND s.voided_at IS NULL AND s.envelope_hash = p_hash) < (st.step ->> 'quorum')::integer);
$$;
-- +goose StatementEnd
GRANT EXECUTE ON FUNCTION fyniq.approval_steps_met(uuid, uuid, bytea, jsonb, integer) TO fyniq_api;

-- A request is decided once, from pending, and nothing else about it changes; it is approved only when every step of
-- its policy has its quorum of signed approvals of its envelope (rev 0.40; before, one signed approval).
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.approval_request_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF OLD.status <> 'pending' OR NEW.status = 'pending' OR NEW.version <> OLD.version + 1
     OR (NEW.object_type, NEW.object_key, NEW.operation, NEW.summary, NEW.change, NEW.envelope, NEW.envelope_hash, NEW.policy_version,
         NEW.policy_steps, NEW.requested_by, NEW.requested_at, NEW.requester_mfa_method, NEW.requester_mfa_at, NEW.expires_at)
        IS DISTINCT FROM (OLD.object_type, OLD.object_key, OLD.operation, OLD.summary, OLD.change, OLD.envelope, OLD.envelope_hash,
         OLD.policy_version, OLD.policy_steps, OLD.requested_by, OLD.requested_at, OLD.requester_mfa_method, OLD.requester_mfa_at,
         OLD.expires_at) THEN
    RAISE EXCEPTION 'An approval request is decided once, from pending, and never changed otherwise' USING ERRCODE = 'FY102',
      HINT = 'Ask for the change again: a new request with a new envelope (FR-APR-10).';
  END IF;
  IF NEW.status = 'approved' AND NOT fyniq.approval_steps_met(NEW.organisation_id, NEW.id, NEW.envelope_hash, NEW.policy_steps,
                                                              jsonb_array_length(NEW.policy_steps)) THEN
    RAISE EXCEPTION 'An approval request is approved only when every step of its policy has its quorum of signed approvals of its envelope'
      USING ERRCODE = 'FY100', HINT = 'Sign the envelope with a second factor first, step by step (FR-APR-01, FR-APR-03).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

-- A signed approval signs its request's envelope, by someone other than the requester, while it is pending and
-- before it expires; and it signs the request's current step: one of its policy's steps, every earlier one met
-- and this one not yet (rev 0.40).
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.approval_signature_check() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  r fyniq.approval_request;
BEGIN
  IF TG_OP = 'UPDATE' THEN
    IF OLD.voided_at IS NOT NULL OR NEW.voided_at IS NULL
       OR (NEW.request_id, NEW.step, NEW.signer, NEW.decision, NEW.envelope_hash, NEW.mfa_method, NEW.mfa_at, NEW.signed_at, NEW.reason)
          IS DISTINCT FROM (OLD.request_id, OLD.step, OLD.signer, OLD.decision, OLD.envelope_hash, OLD.mfa_method, OLD.mfa_at, OLD.signed_at, OLD.reason) THEN
      RAISE EXCEPTION 'A signed approval is never changed, only voided once' USING ERRCODE = 'FY102';
    END IF;
    RETURN NEW;
  END IF;
  SELECT * INTO r FROM fyniq.approval_request q WHERE q.organisation_id = NEW.organisation_id AND q.id = NEW.request_id;
  IF r.envelope_hash IS DISTINCT FROM NEW.envelope_hash THEN
    RAISE EXCEPTION 'The signed hash is not the hash of the request''s envelope' USING ERRCODE = 'FY100',
      HINT = 'Sign the envelope the request holds, exactly (FR-APR-03, FR-APR-10).';
  END IF;
  IF r.requested_by = NEW.signer THEN
    RAISE EXCEPTION 'The requester never signs their own request' USING ERRCODE = 'FY101',
      HINT = 'Another member who holds the approval right signs it (FM-1).';
  END IF;
  IF r.status <> 'pending' OR NEW.signed_at >= r.expires_at OR NEW.signed_at < r.requested_at OR NEW.voided_at IS NOT NULL THEN
    RAISE EXCEPTION 'The request is not pending, or it expired at %', r.expires_at USING ERRCODE = 'FY103',
      HINT = 'Ask for the change again (FR-APR-05).';
  END IF;
  IF NEW.step > jsonb_array_length(r.policy_steps)
     OR NOT fyniq.approval_steps_met(r.organisation_id, r.id, r.envelope_hash, r.policy_steps, NEW.step - 1)
     OR (SELECT count(*) FROM fyniq.approval_signature s
          WHERE s.organisation_id = r.organisation_id AND s.request_id = r.id AND s.step = NEW.step AND s.decision = 'approve'
            AND s.voided_at IS NULL AND s.envelope_hash = r.envelope_hash) >= (r.policy_steps -> (NEW.step - 1) ->> 'quorum')::integer THEN
    RAISE EXCEPTION 'Step % is not the request''s current step', NEW.step USING ERRCODE = 'FY106',
      HINT = 'Sign the first step whose quorum is not met yet; the steps are signed in order (FR-APR-01).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
