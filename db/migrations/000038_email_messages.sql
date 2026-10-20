-- Outbound email messages (PRD-001 FR-INT-21, FR-NTF-05; p1-templates-email task 2.1).
--
-- A message is sent through the outbox, so it is sent once, or Fyniq knows it may not have been (G4-073). This
-- row is its record: purpose, recipient, subject, provider and outcome, never the body (the body travels in the
-- outbox job only). An attempt marks attempt_started_at before it hands the message over; an attempt that finds
-- a mark without an outcome asks the provider, and when the provider cannot say (SMTP never can), the message
-- becomes unknown and is never sent again on a guess. Every outcome is audited.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.email_message (
  organisation_id    uuid        NOT NULL DEFAULT fyniq.current_org(),
  id                 uuid        NOT NULL DEFAULT uuidv7(),
  purpose            text        NOT NULL,
  recipient          text        NOT NULL,
  subject            text        NOT NULL,
  status             text        NOT NULL DEFAULT 'queued',
  provider           text,
  provider_id        text,
  attempts           integer     NOT NULL DEFAULT 0,
  attempt_started_at timestamptz,
  accepted_at        timestamptz,
  last_error         text,
  created_at         timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT email_message_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT email_message_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT email_message_purpose_known CHECK (purpose IN ('test')),
  CONSTRAINT email_message_status_known CHECK (status IN ('queued', 'accepted', 'failed', 'unknown')),
  CONSTRAINT email_message_lengths CHECK (length(recipient) BETWEEN 3 AND 254 AND length(subject) BETWEEN 1 AND 200
    AND length(provider) BETWEEN 1 AND 40 AND length(provider_id) BETWEEN 1 AND 500 AND length(last_error) BETWEEN 1 AND 500),
  CONSTRAINT email_message_attempts_range CHECK (attempts BETWEEN 0 AND 100),
  CONSTRAINT email_message_accepted_has_provider CHECK (status <> 'accepted' OR (provider IS NOT NULL AND accepted_at IS NOT NULL))
);

-- An outcome is final: an accepted, failed or unknown message never changes again.
-- +goose StatementBegin
CREATE FUNCTION fyniq.email_message_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF OLD.status <> 'queued' THEN
    RAISE EXCEPTION 'The email message % has its outcome, %, which never changes', OLD.id, OLD.status USING ERRCODE = 'FY130';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER email_message_guard BEFORE UPDATE ON fyniq.email_message
  FOR EACH ROW EXECUTE FUNCTION fyniq.email_message_guard();

ALTER TABLE fyniq.email_message ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.email_message FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.email_message
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

-- The message and each outcome are audited; marking an attempt is not a change of state.
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE OF status ON fyniq.email_message
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('email_message', 'id');

-- The API queues a message; the worker sends it and records the outcome.
GRANT SELECT ON fyniq.email_message TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (purpose, recipient, subject) ON fyniq.email_message TO fyniq_api;
GRANT UPDATE (status, provider, provider_id, attempts, attempt_started_at, accepted_at, last_error) ON fyniq.email_message TO fyniq_worker;
