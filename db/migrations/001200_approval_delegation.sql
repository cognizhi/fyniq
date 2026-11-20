-- Delegation of approval authority (PRD-001 FR-APR-06; p2-mobile-approvals-automation design D1, rev 0.617).
--
-- An approver names a delegate for a start and an end day (at most 90 days). A delegation lends no right: the delegate must hold the approval right on
-- the object's row themselves (the API checks it, as it does every approver's), and it only makes them eligible for a step the delegator could sign.
-- The delegate signs as themselves with their own step-up or passkey, and the signature records on whose behalf (on_behalf_of). The database refuses a
-- signature on behalf of someone without a live delegation on the day it was signed (by signed_at, not the clock), one for the requester, one for a
-- person who signed already or has a delegate's signature, and a person's own signature after a delegate signed for them (FYD02). A delegation is only ever
-- ended, once, and never changed or deleted (FYD01). Reminders and escalations of pending approvals (FR-APR-08) are logged in approval_reminder, insert-only,
-- one row per request, kind and number, by the worker alone: they notify and never decide.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.approval_delegation (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  delegator       uuid        NOT NULL,
  delegate        uuid        NOT NULL,
  starts_on       date        NOT NULL,
  ends_on         date        NOT NULL,
  reason          text        NOT NULL,
  status          text        NOT NULL DEFAULT 'active',
  ended_at        timestamptz,
  ended_by        uuid,
  version         integer     NOT NULL DEFAULT 1,
  created_by      uuid        NOT NULL,
  created_at      timestamptz NOT NULL,
  CONSTRAINT approval_delegation_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT approval_delegation_delegator_fkey FOREIGN KEY (delegator) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT approval_delegation_delegate_fkey FOREIGN KEY (delegate) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT approval_delegation_ended_by_fkey FOREIGN KEY (ended_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT approval_delegation_not_self CHECK (delegator <> delegate),
  CONSTRAINT approval_delegation_by_delegator CHECK (created_by = delegator),
  CONSTRAINT approval_delegation_days CHECK (ends_on >= starts_on AND ends_on - starts_on <= 90),
  CONSTRAINT approval_delegation_reason CHECK (length(btrim(reason)) BETWEEN 1 AND 300),
  CONSTRAINT approval_delegation_status CHECK (status IN ('active', 'ended')),
  CONSTRAINT approval_delegation_ended_form CHECK ((status = 'ended') = (ended_at IS NOT NULL AND ended_by IS NOT NULL)),
  CONSTRAINT approval_delegation_version CHECK (version >= 1)
);
CREATE INDEX approval_delegation_delegate ON fyniq.approval_delegation (organisation_id, delegate, ends_on);
CREATE INDEX approval_delegation_delegator ON fyniq.approval_delegation (organisation_id, delegator, ends_on);

-- +goose StatementBegin
CREATE FUNCTION fyniq.approval_delegation_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP IN ('DELETE', 'TRUNCATE') THEN
    RAISE EXCEPTION 'A delegation of approval authority is kept: it is only ended' USING ERRCODE = 'FYD01', HINT = 'End it instead (FR-APR-06).';
  END IF;
  IF OLD.status <> 'active' OR NEW.status <> 'ended' OR NEW.version <> OLD.version + 1
     OR (NEW.organisation_id, NEW.id, NEW.delegator, NEW.delegate, NEW.starts_on, NEW.ends_on, NEW.reason, NEW.created_by, NEW.created_at)
        IS DISTINCT FROM (OLD.organisation_id, OLD.id, OLD.delegator, OLD.delegate, OLD.starts_on, OLD.ends_on, OLD.reason, OLD.created_by, OLD.created_at) THEN
    RAISE EXCEPTION 'A delegation of approval authority is only ended, once, and nothing else about it changes' USING ERRCODE = 'FYD01',
      HINT = 'Make a new delegation for other days or another delegate (FR-APR-06).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER approval_delegation_guard BEFORE UPDATE OR DELETE ON fyniq.approval_delegation FOR EACH ROW EXECUTE FUNCTION fyniq.approval_delegation_guard();
CREATE TRIGGER approval_delegation_no_truncate BEFORE TRUNCATE ON fyniq.approval_delegation FOR EACH STATEMENT EXECUTE FUNCTION fyniq.approval_delegation_guard();

ALTER TABLE fyniq.approval_delegation ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.approval_delegation FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.approval_delegation USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.approval_delegation
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('approval_delegation', 'id');

REVOKE ALL ON fyniq.approval_delegation FROM PUBLIC;
GRANT SELECT ON fyniq.approval_delegation TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
GRANT INSERT (delegator, delegate, starts_on, ends_on, reason, created_by, created_at) ON fyniq.approval_delegation TO fyniq_api;
GRANT UPDATE (status, ended_at, ended_by, version) ON fyniq.approval_delegation TO fyniq_api;

-- A signature on behalf of a delegator.
ALTER TABLE fyniq.approval_signature ADD COLUMN on_behalf_of uuid;
ALTER TABLE fyniq.approval_signature ADD CONSTRAINT approval_signature_on_behalf_fkey FOREIGN KEY (on_behalf_of) REFERENCES fyniq.app_user (user_id) NOT VALID;
ALTER TABLE fyniq.approval_signature ADD CONSTRAINT approval_signature_on_behalf_other CHECK (on_behalf_of IS NULL OR (on_behalf_of <> signer AND decision IN ('approve', 'reject'))) NOT VALID;
-- squawk-ignore require-concurrent-index-creation
CREATE UNIQUE INDEX approval_signature_on_behalf_once ON fyniq.approval_signature (organisation_id, request_id, on_behalf_of) WHERE on_behalf_of IS NOT NULL;
GRANT INSERT (on_behalf_of) ON fyniq.approval_signature TO fyniq_api;

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
           NEW.passkey_challenge_id, NEW.passkey_id, NEW.authenticator_data, NEW.client_data, NEW.assertion_signature, NEW.selection_sha256, NEW.on_behalf_of)
          IS DISTINCT FROM (OLD.request_id, OLD.step, OLD.signer, OLD.decision, OLD.envelope_hash, OLD.mfa_method, OLD.mfa_at, OLD.signed_at,
           OLD.reason, OLD.passkey_challenge_id, OLD.passkey_id, OLD.authenticator_data, OLD.client_data, OLD.assertion_signature,
           OLD.selection_sha256, OLD.on_behalf_of) THEN
      RAISE EXCEPTION 'A signed approval is never changed, only voided once' USING ERRCODE = 'FY102';
    END IF;
    RETURN NEW;
  END IF;
  SELECT * INTO r FROM fyniq.approval_request q WHERE q.organisation_id = NEW.organisation_id AND q.id = NEW.request_id;
  IF r.envelope_hash IS DISTINCT FROM NEW.envelope_hash THEN
    RAISE EXCEPTION 'The signed hash is not the hash of the request''s envelope' USING ERRCODE = 'FY100',
      HINT = 'Sign the envelope the request holds, exactly (FR-APR-03, FR-APR-10).';
  END IF;
  -- The person who asked may sign their own tier-1 AI proposal (AI-10, AI-89): ai_proposal's alone.
  IF r.requested_by = NEW.signer AND NOT (r.object_type = 'ai_proposal' AND EXISTS (
       SELECT 1 FROM fyniq.ai_proposal p
        WHERE p.organisation_id = r.organisation_id AND p.approval_request_id = r.id AND p.risk_tier = 1
          AND p.requested_by = NEW.signer)) THEN
    RAISE EXCEPTION 'The requester never signs their own request' USING ERRCODE = 'FY101',
      HINT = 'Another member who holds the approval right signs it (FM-1).';
  END IF;
  -- An AI app's proposal is not a turn's: the person who asked may sign it only on a step-up taken after the proposal was made,
  -- so the delegated token, or a step-up from before the app asked, never stands for them (AI-16, AI-89 rev 0.298).
  IF r.requested_by = NEW.signer AND r.object_type = 'ai_proposal' AND EXISTS (
       SELECT 1 FROM fyniq.ai_proposal p
        WHERE p.organisation_id = r.organisation_id AND p.approval_request_id = r.id AND p.mcp_app IS NOT NULL)
     AND NEW.mfa_at <= r.requested_at THEN
    RAISE EXCEPTION 'The person an AI app proposed for signs it only on a step-up taken after it was proposed' USING ERRCODE = 'FY342',
      HINT = 'Confirm it''s you again now, or ask another member who holds the approval right (AI-89).';
  END IF;
  -- A selection is the items of an AI proposal its approver approves, re-hashed (AI-32): an ai_proposal's approval's alone.
  IF NEW.selection_sha256 IS NOT NULL AND (r.object_type <> 'ai_proposal' OR NEW.decision <> 'approve') THEN
    RAISE EXCEPTION 'Only an approval of an AI proposal selects its items' USING ERRCODE = 'FY342';
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
  -- A delegate's signature (FR-APR-06): on a live delegation of the day it was signed, for a delegator who has not signed and has no one else signing
  -- for them, never for the requester, whose own request nobody signs on their behalf; a person whom a delegate signed for does not sign again.
  IF NEW.on_behalf_of IS NOT NULL THEN
    IF NEW.on_behalf_of = r.requested_by
       OR NOT EXISTS (SELECT 1 FROM fyniq.approval_delegation d
                       WHERE d.organisation_id = NEW.organisation_id AND d.delegator = NEW.on_behalf_of AND d.delegate = NEW.signer
                         AND d.created_at <= NEW.signed_at AND (d.ended_at IS NULL OR d.ended_at > NEW.signed_at)
                         AND d.starts_on <= (NEW.signed_at AT TIME ZONE 'Asia/Singapore')::date
                         AND d.ends_on >= (NEW.signed_at AT TIME ZONE 'Asia/Singapore')::date)
       OR EXISTS (SELECT 1 FROM fyniq.approval_signature s
                   WHERE s.organisation_id = NEW.organisation_id AND s.request_id = NEW.request_id AND s.voided_at IS NULL
                     AND (s.signer = NEW.on_behalf_of OR s.on_behalf_of = NEW.on_behalf_of)) THEN
      RAISE EXCEPTION 'A delegate signs only on a live delegation, for someone who has not signed and is not the requester' USING ERRCODE = 'FYD02',
        HINT = 'Sign as yourself, or ask the delegator to sign (FR-APR-06).';
    END IF;
  ELSIF EXISTS (SELECT 1 FROM fyniq.approval_signature s
                 WHERE s.organisation_id = NEW.organisation_id AND s.request_id = NEW.request_id AND s.voided_at IS NULL AND s.on_behalf_of = NEW.signer) THEN
    RAISE EXCEPTION 'Someone already signed on your behalf, and a person is counted once in a request' USING ERRCODE = 'FYD02',
      HINT = 'Nothing to do: your delegate signed this step for you (FR-APR-06).';
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

-- A pending approval's reminders and escalation (FR-APR-08): a row says that one was sent, so a run that comes again sends none twice. The worker alone
-- inserts one, and nobody changes or deletes one; it never signs, decides or extends a request.
CREATE TABLE fyniq.approval_reminder (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  request_id      uuid        NOT NULL,
  kind            text        NOT NULL,
  number          integer     NOT NULL,
  sent_at         timestamptz NOT NULL,
  CONSTRAINT approval_reminder_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT approval_reminder_request_fkey FOREIGN KEY (organisation_id, request_id) REFERENCES fyniq.approval_request (organisation_id, id),
  CONSTRAINT approval_reminder_once UNIQUE (organisation_id, request_id, kind, number),
  CONSTRAINT approval_reminder_kind CHECK ((kind = 'reminder' AND number BETWEEN 1 AND 3) OR (kind = 'escalation' AND number = 1))
);
-- +goose StatementBegin
CREATE FUNCTION fyniq.approval_reminder_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'A recorded approval reminder is never changed or deleted' USING ERRCODE = 'FYD03', HINT = 'It is the log of what was sent (FR-APR-08).';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER approval_reminder_guard BEFORE UPDATE OR DELETE ON fyniq.approval_reminder FOR EACH ROW EXECUTE FUNCTION fyniq.approval_reminder_guard();
CREATE TRIGGER approval_reminder_no_truncate BEFORE TRUNCATE ON fyniq.approval_reminder FOR EACH STATEMENT EXECUTE FUNCTION fyniq.approval_reminder_guard();
ALTER TABLE fyniq.approval_reminder ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.approval_reminder FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.approval_reminder USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.approval_reminder
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('approval_request', 'request_id');
REVOKE ALL ON fyniq.approval_reminder FROM PUBLIC;
GRANT SELECT ON fyniq.approval_reminder TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (request_id, kind, number, sent_at) ON fyniq.approval_reminder TO fyniq_worker;
