-- Mail received at the email-in addresses (PRD-001 FR-INT-22, FR-DOC-12; the owner's decisions of 23 Oct 2026, rev
-- 0.106; p1-templates-email task 2.2, second slice).
--
-- A person's sign-in email, as their provider last verified it, is kept on their identity, so mail they send to their
-- own address, or to their organisation's, is known as theirs (member_emails). Before, it was used only for the sign-in
-- alert and never stored.
--
-- Each message is recorded when the provider's signed webhook announces it, then fetched by the worker: its SPF, DKIM
-- and DMARC results and its sender's standing decide it: accepted (a member or a contact, or a sender accepted
-- before), quarantined (anyone else, until someone decides, or until the organisation's quarantine days pass), or
-- rejected (a sender rejected before). Its body and attachments, checked and scanned like any upload, are kept as
-- untrusted files (AI-18) beside it, never as evidence: a person makes evidence of them when they confirm what they
-- show. A rejected or expired message's files are removed. A message's status moves forward only (FY185); a file is
-- only ever removed, once (FY186).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- ------------------------------------------------------------------ the people's verified sign-in emails
ALTER TABLE fyniq.user_identity ADD COLUMN email text;
ALTER TABLE fyniq.user_identity ADD COLUMN email_verified_at timestamptz;
ALTER TABLE fyniq.user_identity ADD CONSTRAINT user_identity_email_form
  CHECK ((email IS NULL AND email_verified_at IS NULL)
    OR (email = lower(email) AND length(email) BETWEEN 3 AND 254 AND email_verified_at IS NOT NULL)) NOT VALID;

-- +goose StatementBegin
-- record_identity_email keeps the email a sign-in's provider verified on its identity, if the identity has a user.
CREATE FUNCTION fyniq.record_identity_email(p_issuer text, p_subject text, p_email text, p_now timestamptz) RETURNS void
LANGUAGE sql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  UPDATE fyniq.user_identity SET email = lower(p_email), email_verified_at = p_now
   WHERE issuer = p_issuer AND subject = p_subject AND email IS DISTINCT FROM lower(p_email)
$$;

-- member_emails are the verified sign-in emails of the organisation's active members.
CREATE FUNCTION fyniq.member_emails() RETURNS TABLE (user_id uuid, email text)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT i.user_id, i.email FROM fyniq.user_identity i JOIN fyniq.app_user u USING (user_id)
   WHERE i.email IS NOT NULL AND u.status = 'active'
     AND EXISTS (SELECT 1 FROM fyniq.user_membership m
                  WHERE m.organisation_id = fyniq.current_org() AND m.user_id = i.user_id AND m.status = 'active')
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION fyniq.record_identity_email(text, text, text, timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.record_identity_email(text, text, text, timestamptz) TO fyniq_api;
REVOKE ALL ON FUNCTION fyniq.member_emails() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.member_emails() TO fyniq_api, fyniq_worker;

-- ------------------------------------------------------------------ messages and their files
CREATE TABLE fyniq.inbound_message (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  address_id      uuid        NOT NULL,
  provider        text        NOT NULL,
  provider_id     text        NOT NULL,  -- the provider's ID of the received email
  sender          text        NOT NULL,  -- in lower case
  subject         text,
  received_at     timestamptz NOT NULL,
  status          text        NOT NULL DEFAULT 'received',
  spf             text,
  dkim            text,
  dmarc           text,
  fetched_at      timestamptz,
  expires_at      timestamptz,           -- a quarantined message's end, unless someone decides first; none: kept until then
  decided_by      text,
  decided_at      timestamptz,
  last_error      text,
  CONSTRAINT inbound_message_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT inbound_message_address_fkey FOREIGN KEY (organisation_id, address_id)
    REFERENCES fyniq.capture_address (organisation_id, id),
  CONSTRAINT inbound_message_once UNIQUE (organisation_id, provider, provider_id, address_id),
  CONSTRAINT inbound_message_provider_known CHECK (provider IN ('resend', 'mailpit')), -- mailpit: local mode's substitute (ADR-T4)
  CONSTRAINT inbound_message_status_known CHECK (status IN ('received', 'accepted', 'quarantined', 'rejected', 'expired', 'failed')),
  CONSTRAINT inbound_message_sender_lower CHECK (sender = lower(sender) AND length(sender) BETWEEN 3 AND 254),
  CONSTRAINT inbound_message_lengths CHECK (length(provider_id) BETWEEN 1 AND 200 AND length(subject) <= 500
    AND length(spf) BETWEEN 1 AND 40 AND length(dkim) BETWEEN 1 AND 40 AND length(dmarc) BETWEEN 1 AND 40 AND length(last_error) BETWEEN 1 AND 500),
  CONSTRAINT inbound_message_decided_by_format CHECK (decided_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$'),
  CONSTRAINT inbound_message_decided_pair CHECK ((decided_at IS NULL) = (decided_by IS NULL))
);
CREATE INDEX inbound_message_status ON fyniq.inbound_message (organisation_id, status, received_at);
CREATE INDEX inbound_message_sender ON fyniq.inbound_message (organisation_id, sender, address_id) WHERE status = 'quarantined';

CREATE TABLE fyniq.inbound_file (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  message_id      uuid        NOT NULL,
  kind            text        NOT NULL,
  file_name       text        NOT NULL,
  media_type      text        NOT NULL,
  size_bytes      bigint      NOT NULL,
  sha256          text        NOT NULL,
  original_sha256 text        NOT NULL,
  blob_key        text        NOT NULL,
  stored_at       timestamptz NOT NULL,
  removed_at      timestamptz,
  CONSTRAINT inbound_file_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT inbound_file_message_fkey FOREIGN KEY (organisation_id, message_id)
    REFERENCES fyniq.inbound_message (organisation_id, id),
  CONSTRAINT inbound_file_kind_known CHECK (kind IN ('body', 'attachment')),
  CONSTRAINT inbound_file_lengths CHECK (length(file_name) BETWEEN 1 AND 255 AND length(media_type) BETWEEN 3 AND 100
    AND size_bytes BETWEEN 1 AND 26214400 AND sha256 ~ '^[0-9a-f]{64}$' AND original_sha256 ~ '^[0-9a-f]{64}$'
    AND blob_key ~ '^inbound/[0-9a-f-]{36}/[0-9a-f-]{36}/[0-9a-f-]{36}$')
);
CREATE INDEX inbound_file_message ON fyniq.inbound_file (organisation_id, message_id);

-- A message's status moves forward only: received to accepted, quarantined, rejected or failed; quarantined to accepted,
-- rejected or expired; and what it was received as never changes (FY185). A file is only ever removed, once (FY186).
-- +goose StatementBegin
CREATE FUNCTION fyniq.inbound_message_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE'
     OR (NEW.address_id, NEW.provider, NEW.provider_id, NEW.sender, NEW.subject, NEW.received_at)
        IS DISTINCT FROM (OLD.address_id, OLD.provider, OLD.provider_id, OLD.sender, OLD.subject, OLD.received_at)
     OR (NEW.status <> OLD.status AND NOT (
          (OLD.status = 'received' AND NEW.status IN ('accepted', 'quarantined', 'rejected', 'failed'))
       OR (OLD.status = 'quarantined' AND NEW.status IN ('accepted', 'rejected', 'expired'))))
     OR (NEW.status = OLD.status AND OLD.status <> 'received') THEN
    RAISE EXCEPTION 'An inbound message''s status moves forward only, and what it was received as never changes' USING ERRCODE = 'FY185',
      HINT = 'A quarantined message is accepted, rejected or expires; the others are final (FR-INT-22).';
  END IF;
  RETURN NEW;
END
$$;

CREATE FUNCTION fyniq.inbound_file_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR OLD.removed_at IS NOT NULL OR NEW.removed_at IS NULL
     OR (NEW.message_id, NEW.kind, NEW.file_name, NEW.media_type, NEW.size_bytes, NEW.sha256, NEW.original_sha256, NEW.blob_key, NEW.stored_at)
        IS DISTINCT FROM (OLD.message_id, OLD.kind, OLD.file_name, OLD.media_type, OLD.size_bytes, OLD.sha256, OLD.original_sha256, OLD.blob_key,
                          OLD.stored_at) THEN
    RAISE EXCEPTION 'An inbound file is only ever removed, once' USING ERRCODE = 'FY186',
      HINT = 'Its message''s rejection or expiry removes it (FR-INT-22).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER inbound_message_guard BEFORE UPDATE OR DELETE ON fyniq.inbound_message
  FOR EACH ROW EXECUTE FUNCTION fyniq.inbound_message_guard();
CREATE TRIGGER inbound_message_no_truncate BEFORE TRUNCATE ON fyniq.inbound_message
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();
CREATE TRIGGER inbound_file_guard BEFORE UPDATE OR DELETE ON fyniq.inbound_file
  FOR EACH ROW EXECUTE FUNCTION fyniq.inbound_file_guard();
CREATE TRIGGER inbound_file_no_truncate BEFORE TRUNCATE ON fyniq.inbound_file
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();

ALTER TABLE fyniq.inbound_message ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.inbound_message FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.inbound_message
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.inbound_file ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.inbound_file FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.inbound_file
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE OF status ON fyniq.inbound_message
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('inbound_message', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.inbound_file
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('inbound_message', 'message_id');

-- The API records a message from the webhook and decides about senders; the worker fetches, stores and expires.
GRANT SELECT ON fyniq.inbound_message, fyniq.inbound_file TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (address_id, provider, provider_id, sender, subject, received_at) ON fyniq.inbound_message TO fyniq_api;
GRANT UPDATE (status, decided_by, decided_at) ON fyniq.inbound_message TO fyniq_api;
GRANT UPDATE (status, spf, dkim, dmarc, fetched_at, expires_at, last_error) ON fyniq.inbound_message TO fyniq_worker;
GRANT INSERT (message_id, kind, file_name, media_type, size_bytes, sha256, original_sha256, blob_key, stored_at) ON fyniq.inbound_file TO fyniq_worker;
GRANT UPDATE (removed_at) ON fyniq.inbound_file TO fyniq_api, fyniq_worker;
