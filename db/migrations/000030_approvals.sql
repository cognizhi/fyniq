-- The approval engine's requests and signed approvals (PRD-001 FR-APR-03, FR-APR-05, FR-APR-10, FM-1, rev 0.36;
-- p1-approval-engine tasks 1.1–1.3).
--
-- A request holds the approval envelope exactly as signed (RFC 8785 canonical JSON) and its SHA-256, which the
-- database recomputes; the change that executes on the final approval; the policy version it started under;
-- and its requester with the second factor they passed. It waits 7 days from the app's clock, then expires.
-- A request is decided once (approved, rejected, void or expired) and never changed otherwise.
--
-- A signed approval (approval_signature) is the electronic signature of §14.2: the signer, the time, the second
-- factor and the hash of the envelope signed. The database refuses one without a hash or a second factor, one
-- whose hash is not its request's, one by the requester (FM-1's two-person rule), and one on a request that is
-- not pending or has expired. It is never changed, except to be voided.
--
-- Voiding is the database's: when the record a pending request would change gets a new version (a setting's
-- next change, a member's roles), the request becomes void, and voiding a request voids its signatures. The
-- executing transaction marks its own request approved before it changes the record, so only the others void.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.approval_request (
  organisation_id      uuid        NOT NULL DEFAULT fyniq.current_org(),
  id                   uuid        NOT NULL DEFAULT uuidv7(),
  object_type          text        NOT NULL,
  object_key           text        NOT NULL,
  operation            text        NOT NULL,
  summary              text        NOT NULL,
  change               jsonb       NOT NULL,
  envelope             text        NOT NULL,
  envelope_hash        bytea       NOT NULL,
  policy_version       text        NOT NULL,
  requested_by         uuid        NOT NULL,
  requested_at         timestamptz NOT NULL,
  requester_mfa_method text        NOT NULL,
  requester_mfa_at     timestamptz NOT NULL,
  expires_at           timestamptz NOT NULL,
  status               text        NOT NULL DEFAULT 'pending',
  decided_at           timestamptz,
  status_reason        text,
  voided_xact          bigint,
  version              integer     NOT NULL DEFAULT 1,
  CONSTRAINT approval_request_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT approval_request_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT approval_request_requested_by_fkey FOREIGN KEY (requested_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT approval_request_object_known CHECK (
    (object_type, operation) IN (('organisation_setting', 'organisation_setting.change'), ('membership', 'role.assign'),
                                 ('membership', 'role.revoke'), ('member_invitation', 'member_invitation.create'))),
  CONSTRAINT approval_request_key_length CHECK (length(object_key) BETWEEN 1 AND 100),
  CONSTRAINT approval_request_summary_length CHECK (length(summary) BETWEEN 1 AND 500),
  CONSTRAINT approval_request_change_object CHECK (jsonb_typeof(change) = 'object'),
  CONSTRAINT approval_request_envelope_length CHECK (length(envelope) BETWEEN 2 AND 65536),
  CONSTRAINT approval_request_envelope_hashed CHECK (envelope_hash = sha256(convert_to(envelope, 'UTF8'))),
  CONSTRAINT approval_request_policy_length CHECK (length(policy_version) BETWEEN 1 AND 100),
  CONSTRAINT approval_request_mfa_known CHECK (requester_mfa_method IN ('session_step_up', 'passkey', 'totp')),
  CONSTRAINT approval_request_mfa_fresh CHECK (requester_mfa_at <= requested_at AND requested_at - requester_mfa_at <= interval '5 minutes'),
  CONSTRAINT approval_request_expiry CHECK (expires_at > requested_at AND expires_at <= requested_at + interval '30 days'),
  CONSTRAINT approval_request_status_known CHECK (status IN ('pending', 'approved', 'rejected', 'void', 'expired')),
  CONSTRAINT approval_request_decided CHECK ((status = 'pending') = (decided_at IS NULL)),
  CONSTRAINT approval_request_reason_length CHECK (status_reason IS NULL OR length(status_reason) BETWEEN 1 AND 500),
  CONSTRAINT approval_request_version_positive CHECK (version > 0)
);
-- One request waits per object at a time.
CREATE UNIQUE INDEX approval_request_one_pending ON fyniq.approval_request (organisation_id, object_type, object_key)
  WHERE status = 'pending';
CREATE INDEX approval_request_by_status ON fyniq.approval_request (organisation_id, status, requested_at);

CREATE TABLE fyniq.approval_signature (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  request_id      uuid        NOT NULL,
  step            integer     NOT NULL,
  signer          uuid        NOT NULL,
  decision        text        NOT NULL,
  envelope_hash   bytea       NOT NULL,
  mfa_method      text        NOT NULL,
  mfa_at          timestamptz NOT NULL,
  signed_at       timestamptz NOT NULL,
  reason          text,
  voided_at       timestamptz,
  CONSTRAINT approval_signature_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT approval_signature_once UNIQUE (organisation_id, request_id, signer),
  CONSTRAINT approval_signature_request_fkey FOREIGN KEY (organisation_id, request_id)
    REFERENCES fyniq.approval_request (organisation_id, id),
  CONSTRAINT approval_signature_signer_fkey FOREIGN KEY (signer) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT approval_signature_step_positive CHECK (step BETWEEN 1 AND 20),
  CONSTRAINT approval_signature_decision_known CHECK (decision IN ('approve', 'reject')),
  CONSTRAINT approval_signature_hash_sha256 CHECK (octet_length(envelope_hash) = 32),
  CONSTRAINT approval_signature_mfa_known CHECK (mfa_method IN ('session_step_up', 'passkey', 'totp')),
  CONSTRAINT approval_signature_mfa_fresh CHECK (mfa_at <= signed_at AND signed_at - mfa_at <= interval '5 minutes'),
  CONSTRAINT approval_signature_reason CHECK ((decision = 'reject') = (reason IS NOT NULL) AND (reason IS NULL OR length(btrim(reason)) BETWEEN 1 AND 500))
);

-- A request is decided once, from pending, and nothing else about it changes; every change takes its next version.
-- +goose StatementBegin
CREATE FUNCTION fyniq.approval_request_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF OLD.status <> 'pending' OR NEW.status = 'pending' OR NEW.version <> OLD.version + 1
     OR (NEW.object_type, NEW.object_key, NEW.operation, NEW.summary, NEW.change, NEW.envelope, NEW.envelope_hash, NEW.policy_version,
         NEW.requested_by, NEW.requested_at, NEW.requester_mfa_method, NEW.requester_mfa_at, NEW.expires_at)
        IS DISTINCT FROM (OLD.object_type, OLD.object_key, OLD.operation, OLD.summary, OLD.change, OLD.envelope, OLD.envelope_hash,
         OLD.policy_version, OLD.requested_by, OLD.requested_at, OLD.requester_mfa_method, OLD.requester_mfa_at, OLD.expires_at) THEN
    RAISE EXCEPTION 'An approval request is decided once, from pending, and never changed otherwise' USING ERRCODE = 'FY102',
      HINT = 'Ask for the change again: a new request with a new envelope (FR-APR-10).';
  END IF;
  IF NEW.status = 'approved' AND NOT EXISTS (
       SELECT 1 FROM fyniq.approval_signature s
        WHERE s.organisation_id = NEW.organisation_id AND s.request_id = NEW.id AND s.decision = 'approve' AND s.voided_at IS NULL
          AND s.envelope_hash = NEW.envelope_hash) THEN
    RAISE EXCEPTION 'An approval request is approved only by a signed approval of its envelope' USING ERRCODE = 'FY100',
      HINT = 'Sign the envelope with a second factor first (FR-APR-03).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER approval_request_guard BEFORE UPDATE ON fyniq.approval_request
  FOR EACH ROW EXECUTE FUNCTION fyniq.approval_request_guard();

-- A signed approval signs its request's envelope, by someone other than the requester, while it is pending and
-- before it expires.
-- +goose StatementBegin
CREATE FUNCTION fyniq.approval_signature_check() RETURNS trigger
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
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER approval_signature_check BEFORE INSERT OR UPDATE ON fyniq.approval_signature
  FOR EACH ROW EXECUTE FUNCTION fyniq.approval_signature_check();

-- Voiding a request voids its signatures. It runs as the function's owner: the API never updates a signature.
-- +goose StatementBegin
CREATE FUNCTION fyniq.approval_request_voided() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  UPDATE fyniq.approval_signature s SET voided_at = coalesce(NEW.decided_at, now())
   WHERE s.organisation_id = NEW.organisation_id AND s.request_id = NEW.id AND s.voided_at IS NULL;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER approval_request_voided AFTER UPDATE OF status ON fyniq.approval_request
  FOR EACH ROW WHEN (NEW.status = 'void') EXECUTE FUNCTION fyniq.approval_request_voided();

-- A new version of the record voids the pending requests that would change it (FR-APR-05). The voided requests
-- are audited by the unit of work that changed the record, in the same transaction (require_audit). The voiding
-- runs as the function's owner, so the API's own grant never reaches voided_xact; row-level security still holds.
-- +goose StatementBegin
CREATE FUNCTION fyniq.approval_void_pending(p_type text, p_key text, p_reason text) RETURNS void
LANGUAGE sql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  UPDATE fyniq.approval_request r
     SET status = 'void', decided_at = now(), status_reason = p_reason, version = r.version + 1,
         voided_xact = pg_current_xact_id()::text::bigint
   WHERE r.organisation_id = fyniq.current_org() AND r.object_type = p_type AND r.object_key = p_key AND r.status = 'pending';
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION fyniq.organisation_setting_voids() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  PERFORM fyniq.approval_void_pending('organisation_setting', NEW.key, 'the setting changed after the request');
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER organisation_setting_voids AFTER INSERT ON fyniq.organisation_setting
  FOR EACH ROW EXECUTE FUNCTION fyniq.organisation_setting_voids();

-- +goose StatementBegin
CREATE FUNCTION fyniq.role_assignment_voids() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  PERFORM fyniq.approval_void_pending('membership', NEW.membership_id::text, 'the member''s roles changed after the request');
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER role_assignment_voids AFTER INSERT OR UPDATE ON fyniq.role_assignment
  FOR EACH ROW EXECUTE FUNCTION fyniq.role_assignment_voids();

ALTER TABLE fyniq.approval_request ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.approval_request FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.approval_request
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.approval_signature ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.approval_signature FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.approval_signature
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.approval_request
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('approval_request', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.approval_signature
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('approval_request', 'request_id');

-- Only the API requests, signs and decides; every role reads. Nobody deletes.
GRANT SELECT ON fyniq.approval_request, fyniq.approval_signature TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
GRANT INSERT (object_type, object_key, operation, summary, change, envelope, envelope_hash, policy_version, requested_by, requested_at,
  requester_mfa_method, requester_mfa_at, expires_at), UPDATE (status, decided_at, status_reason, version)
  ON fyniq.approval_request TO fyniq_api;
GRANT INSERT (request_id, step, signer, decision, envelope_hash, mfa_method, mfa_at, signed_at, reason)
  ON fyniq.approval_signature TO fyniq_api;
REVOKE ALL ON FUNCTION fyniq.approval_void_pending(text, text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.approval_void_pending(text, text, text) TO fyniq_api;
