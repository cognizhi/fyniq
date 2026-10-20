-- Approvals signed with a passkey (PRD-001 FR-APR-03, G4-056, rev 0.44; p1-approval-engine task 1.2, p1-users-roles
-- task 2.2).
--
-- To sign an approval request with a passkey, its signer asks for a challenge that is 16 random bytes followed by the
-- SHA-256 of the request's envelope, bound to their session, the organisation and the request: the authenticator's
-- signature is then over exactly what is approved. The challenge is taken once before it expires (5 minutes) and
-- answered once, in the transaction that records the signature. The signature keeps the assertion (authenticator data,
-- client data and signature) and the passkey that made it, so it can be verified again later; the database records a
-- passkey-signed approval only as the answer, by the signer's own passkey, to a challenge of that organisation, request
-- and envelope (FY142). A challenge from another organisation, request or envelope, or one taken late, signs nothing.
--
-- The checks on the challenge and signature tables are widened NOT VALID and validated by 000048 (squawk:
-- constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- ------------------------------------------------------------------ approval challenges

ALTER TABLE fyniq.webauthn_challenge ADD COLUMN request_organisation_id uuid; -- not a tenant column: the table is global
ALTER TABLE fyniq.webauthn_challenge ADD COLUMN request_id uuid;
ALTER TABLE fyniq.webauthn_challenge ADD COLUMN envelope_hash bytea;
ALTER TABLE fyniq.webauthn_challenge DROP CONSTRAINT webauthn_challenge_purpose_known;
ALTER TABLE fyniq.webauthn_challenge ADD CONSTRAINT webauthn_challenge_purpose_known
  CHECK (purpose IN ('register', 'step_up', 'approval')) NOT VALID;
ALTER TABLE fyniq.webauthn_challenge DROP CONSTRAINT webauthn_challenge_random;
-- Registration and step-up: 32 random bytes and nothing else. An approval: 16 random bytes and the envelope's SHA-256,
-- with the organisation and request it signs.
ALTER TABLE fyniq.webauthn_challenge ADD CONSTRAINT webauthn_challenge_shape CHECK (
  (purpose <> 'approval' AND length(challenge) = 32 AND request_organisation_id IS NULL AND request_id IS NULL AND envelope_hash IS NULL)
  OR (purpose = 'approval' AND length(challenge) = 48 AND octet_length(envelope_hash) = 32 AND substring(challenge FROM 17) = envelope_hash
      AND request_organisation_id IS NOT NULL AND request_id IS NOT NULL)) NOT VALID;

-- A challenge is taken once and answered once; nothing else about it changes, and it is never deleted.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.webauthn_challenge_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE'
     OR (NEW.id, NEW.user_id, NEW.purpose, NEW.challenge, NEW.session_hash, NEW.created_at, NEW.expires_at, NEW.request_organisation_id,
         NEW.request_id, NEW.envelope_hash)
        IS DISTINCT FROM (OLD.id, OLD.user_id, OLD.purpose, OLD.challenge, OLD.session_hash, OLD.created_at, OLD.expires_at,
                          OLD.request_organisation_id, OLD.request_id, OLD.envelope_hash)
     OR (OLD.taken_at IS NOT NULL AND NEW.taken_at IS DISTINCT FROM OLD.taken_at)
     OR (OLD.answered_by IS NOT NULL AND NEW.answered_by IS DISTINCT FROM OLD.answered_by) THEN
    RAISE EXCEPTION 'A passkey challenge is taken once and answered once, and never changes otherwise' USING ERRCODE = 'FY141',
      HINT = 'Ask for a new challenge.';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

-- Issue an approval challenge to a live session for a pending request of the organisation in force, over its envelope's
-- hash. Returns its ID.
-- +goose StatementBegin
CREATE FUNCTION fyniq.webauthn_approval_challenge_issue(p_session bytea, p_request uuid, p_envelope_hash bytea, p_nonce bytea,
  p_now timestamptz, p_expires timestamptz) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  who    uuid := fyniq.live_session_user(p_session, p_now);
  issued uuid;
BEGIN
  IF who IS NULL OR octet_length(p_nonce) <> 16 OR NOT EXISTS (
       SELECT 1 FROM fyniq.approval_request r
        WHERE r.organisation_id = fyniq.current_org() AND r.id = p_request AND r.status = 'pending'
          AND r.envelope_hash = p_envelope_hash AND r.expires_at > p_now) THEN
    RAISE EXCEPTION 'An approval challenge is for a live session and a pending request of this organisation, over its envelope'
      USING ERRCODE = 'FY142', HINT = 'Read the request again and ask for a new challenge.';
  END IF;
  INSERT INTO fyniq.webauthn_challenge (user_id, purpose, challenge, session_hash, created_at, expires_at, request_organisation_id,
                                        request_id, envelope_hash)
  VALUES (who, 'approval', p_nonce || p_envelope_hash, p_session, p_now, p_expires, fyniq.current_org(), p_request, p_envelope_hash)
  RETURNING webauthn_challenge.id INTO issued;
  RETURN issued;
END
$$;
-- +goose StatementEnd

-- Record a passkey's answer to an approval challenge the session took in the last 5 minutes, for the organisation in
-- force: by a live passkey of the challenge's user, whose counter moves up.
-- +goose StatementBegin
CREATE FUNCTION fyniq.webauthn_approval_answer(p_challenge uuid, p_session bytea, p_credential uuid, p_sign_count bigint,
  p_backup_state boolean, p_now timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  c fyniq.webauthn_challenge;
  k fyniq.webauthn_credential;
BEGIN
  SELECT * INTO c FROM fyniq.webauthn_challenge w WHERE w.id = p_challenge FOR UPDATE;
  SELECT * INTO k FROM fyniq.webauthn_credential w WHERE w.id = p_credential FOR UPDATE;
  IF c.id IS NULL OR c.purpose <> 'approval' OR c.session_hash <> p_session OR c.request_organisation_id IS DISTINCT FROM fyniq.current_org()
     OR c.taken_at IS NULL OR c.answered_by IS NOT NULL OR c.taken_at < p_now - interval '5 minutes'
     OR c.user_id IS DISTINCT FROM fyniq.live_session_user(p_session, p_now)
     OR k.id IS NULL OR k.user_id <> c.user_id OR k.revoked_at IS NOT NULL THEN
    RAISE EXCEPTION 'An approval is signed with a passkey only as the answer to its own challenge, by a live passkey of the signer'
      USING ERRCODE = 'FY142', HINT = 'Ask for a new challenge and confirm it''s you again.';
  END IF;
  IF (p_sign_count <> 0 OR k.sign_count <> 0) AND p_sign_count <= k.sign_count THEN
    RAISE EXCEPTION 'A passkey''s signature counter went from % to %: the answer is replayed or the authenticator cloned', k.sign_count, p_sign_count
      USING ERRCODE = 'FY140', HINT = 'Revoke the passkey and register a new one (WebAuthn §6.1.1).';
  END IF;
  IF p_sign_count <> k.sign_count THEN
    UPDATE fyniq.webauthn_credential w SET sign_count = p_sign_count WHERE w.id = p_credential;
  END IF;
  UPDATE fyniq.webauthn_credential w SET last_used_at = greatest(p_now, w.created_at), backup_state = p_backup_state AND w.backup_eligible
   WHERE w.id = p_credential;
  UPDATE fyniq.webauthn_challenge w SET answered_by = p_credential WHERE w.id = p_challenge;
END
$$;
-- +goose StatementEnd

-- What an approval challenge proves: its organisation, request, envelope hash and user, the passkey that answered it and
-- when it was taken. Read by the signature check.
-- +goose StatementBegin
CREATE FUNCTION fyniq.webauthn_approval_proof(p_challenge uuid,
  OUT organisation_id uuid, OUT request_id uuid, OUT envelope_hash bytea, OUT user_id uuid, OUT answered_by uuid, OUT taken_at timestamptz)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT c.request_organisation_id, c.request_id, c.envelope_hash, c.user_id, c.answered_by, c.taken_at
    FROM fyniq.webauthn_challenge c WHERE c.id = p_challenge AND c.purpose = 'approval'
$$;
-- +goose StatementEnd

-- ------------------------------------------------------------------ signatures keep their assertion

ALTER TABLE fyniq.approval_signature ADD COLUMN passkey_challenge_id uuid;
ALTER TABLE fyniq.approval_signature ADD COLUMN passkey_id uuid;
ALTER TABLE fyniq.approval_signature ADD COLUMN authenticator_data bytea;
ALTER TABLE fyniq.approval_signature ADD COLUMN client_data bytea;
ALTER TABLE fyniq.approval_signature ADD COLUMN assertion_signature bytea;
-- A passkey-signed approval keeps all of its assertion; any other keeps none.
ALTER TABLE fyniq.approval_signature ADD CONSTRAINT approval_signature_passkey_shape CHECK (
  (mfa_method = 'passkey' AND passkey_challenge_id IS NOT NULL AND passkey_id IS NOT NULL AND length(authenticator_data) BETWEEN 37 AND 2048
   AND length(client_data) BETWEEN 1 AND 4096 AND length(assertion_signature) BETWEEN 8 AND 2048)
  OR (mfa_method <> 'passkey' AND passkey_challenge_id IS NULL AND passkey_id IS NULL AND authenticator_data IS NULL AND client_data IS NULL
      AND assertion_signature IS NULL)) NOT VALID;

-- A signed approval signs its request's envelope, by someone other than the requester, while it is pending and before it
-- expires; it signs the request's current step (rev 0.40); and when signed with a passkey, it is the answer to that
-- organisation's, request's and envelope's challenge, by the signer, taken in the 5 minutes before (rev 0.44).
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.approval_signature_check() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  r fyniq.approval_request;
  c record;
BEGIN
  IF TG_OP = 'UPDATE' THEN
    IF OLD.voided_at IS NOT NULL OR NEW.voided_at IS NULL
       OR (NEW.request_id, NEW.step, NEW.signer, NEW.decision, NEW.envelope_hash, NEW.mfa_method, NEW.mfa_at, NEW.signed_at, NEW.reason,
           NEW.passkey_challenge_id, NEW.passkey_id, NEW.authenticator_data, NEW.client_data, NEW.assertion_signature)
          IS DISTINCT FROM (OLD.request_id, OLD.step, OLD.signer, OLD.decision, OLD.envelope_hash, OLD.mfa_method, OLD.mfa_at, OLD.signed_at,
           OLD.reason, OLD.passkey_challenge_id, OLD.passkey_id, OLD.authenticator_data, OLD.client_data, OLD.assertion_signature) THEN
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
  IF NEW.mfa_method = 'passkey' THEN
    SELECT * INTO c FROM fyniq.webauthn_approval_proof(NEW.passkey_challenge_id);
    IF c.organisation_id IS DISTINCT FROM NEW.organisation_id OR c.request_id IS DISTINCT FROM NEW.request_id
       OR c.envelope_hash IS DISTINCT FROM NEW.envelope_hash OR c.user_id IS DISTINCT FROM NEW.signer
       OR c.answered_by IS DISTINCT FROM NEW.passkey_id OR c.taken_at IS NULL OR NEW.signed_at - c.taken_at > interval '5 minutes'
       OR EXISTS (SELECT 1 FROM fyniq.approval_signature s
                   WHERE s.organisation_id = NEW.organisation_id AND s.passkey_challenge_id = NEW.passkey_challenge_id) THEN
      RAISE EXCEPTION 'A passkey-signed approval is the signer''s answer to a challenge over this request''s envelope'
        USING ERRCODE = 'FY142', HINT = 'Ask for a new challenge for this request and confirm it''s you again (FR-APR-03).';
    END IF;
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

GRANT INSERT (passkey_challenge_id, passkey_id, authenticator_data, client_data, assertion_signature) ON fyniq.approval_signature TO fyniq_api;
REVOKE ALL ON FUNCTION fyniq.webauthn_approval_challenge_issue(bytea, uuid, bytea, bytea, timestamptz, timestamptz),
  fyniq.webauthn_approval_answer(uuid, bytea, uuid, bigint, boolean, timestamptz), fyniq.webauthn_approval_proof(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.webauthn_approval_challenge_issue(bytea, uuid, bytea, bytea, timestamptz, timestamptz),
  fyniq.webauthn_approval_answer(uuid, bytea, uuid, bigint, boolean, timestamptz), fyniq.webauthn_approval_proof(uuid) TO fyniq_api;
