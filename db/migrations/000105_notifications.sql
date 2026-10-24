-- Notifications: one event stream, each person's notification centre, their preferences and their digests (PRD-001
-- FR-NTF-01, FR-NTF-02, FR-NTF-05; the owner's decisions of 24 Oct 2026, rev 0.108; p1-templates-email task 3.1).
--
-- Something that happened is recorded once as a notification event, in the unit of work of what happened, and fanned
-- out by the worker to each person it concerns: a notification in their centre, and by email at once or in their daily
-- or weekly digest, as their preference for its type says (security and approvals at once, the rest in the daily digest
-- at 08:00 Singapore time; security types can never be turned off). An event never changes or goes (FY187); a person's
-- notification changes only in being read, unread or archived (FY188); a digest records the notifications it carried.
-- Email deliveries are email_message rows, so every attempt and outcome is logged (FR-NTF-05).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Notifications and digests are email of their own purposes.
ALTER TABLE fyniq.email_message DROP CONSTRAINT email_message_purpose_known;
ALTER TABLE fyniq.email_message ADD CONSTRAINT email_message_purpose_known
  CHECK (purpose IN ('test', 'sign_in_alert', 'notification', 'digest')) NOT VALID;

CREATE TABLE fyniq.notification_event (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  type            text        NOT NULL,
  record_type     text,
  record_id       uuid,
  title           text        NOT NULL,  -- plain words, no amounts or personal data beyond what the type allows
  link            text,                  -- a path in the web app, such as /approvals/…
  actor           text        NOT NULL,
  audience        jsonb       NOT NULL,  -- whom it concerns: users, and roles holding a right on a row
  occurred_at     timestamptz NOT NULL,
  fanned_out_at   timestamptz,
  CONSTRAINT notification_event_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT notification_event_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT notification_event_type_format CHECK (type ~ '^[a-z]+(\.[a-z_]+)+$' AND length(type) <= 60),
  CONSTRAINT notification_event_lengths CHECK (length(title) BETWEEN 1 AND 300 AND length(link) BETWEEN 1 AND 300
    AND length(record_type) BETWEEN 1 AND 60),
  CONSTRAINT notification_event_link_path CHECK (link ~ '^/[A-Za-z0-9/_?=&.-]*$'),
  CONSTRAINT notification_event_actor_format CHECK (actor ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);
CREATE INDEX notification_event_pending ON fyniq.notification_event (organisation_id, occurred_at) WHERE fanned_out_at IS NULL;

CREATE TABLE fyniq.notification (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  event_id        uuid        NOT NULL,
  user_id         uuid        NOT NULL,
  type            text        NOT NULL,
  severity        text        NOT NULL,
  actionable      boolean     NOT NULL,
  email           text        NOT NULL,  -- how it goes by email: immediate, daily, weekly or off, as decided at fan-out
  email_message_id uuid,                 -- its email, when sent at once
  digest_id       uuid,                  -- the digest that carried it
  created_at      timestamptz NOT NULL,
  read_at         timestamptz,
  archived_at     timestamptz,
  CONSTRAINT notification_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT notification_event_fkey FOREIGN KEY (organisation_id, event_id) REFERENCES fyniq.notification_event (organisation_id, id),
  CONSTRAINT notification_member_fkey FOREIGN KEY (organisation_id, user_id) REFERENCES fyniq.user_membership (organisation_id, user_id),
  CONSTRAINT notification_email_fkey FOREIGN KEY (organisation_id, email_message_id) REFERENCES fyniq.email_message (organisation_id, id),
  CONSTRAINT notification_once UNIQUE (organisation_id, event_id, user_id),
  CONSTRAINT notification_severity_known CHECK (severity IN ('info', 'warning', 'critical')),
  CONSTRAINT notification_email_known CHECK (email IN ('immediate', 'daily', 'weekly', 'off'))
);
CREATE INDEX notification_inbox ON fyniq.notification (organisation_id, user_id, created_at DESC) WHERE archived_at IS NULL;
CREATE INDEX notification_digest_due ON fyniq.notification (organisation_id, email, created_at)
  WHERE digest_id IS NULL AND email IN ('daily', 'weekly');

CREATE TABLE fyniq.notification_digest (
  organisation_id  uuid        NOT NULL DEFAULT fyniq.current_org(),
  id               uuid        NOT NULL DEFAULT uuidv7(),
  user_id          uuid        NOT NULL,
  cadence          text        NOT NULL,
  email_message_id uuid        NOT NULL,
  sent_at          timestamptz NOT NULL,
  CONSTRAINT notification_digest_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT notification_digest_member_fkey FOREIGN KEY (organisation_id, user_id) REFERENCES fyniq.user_membership (organisation_id, user_id),
  CONSTRAINT notification_digest_email_fkey FOREIGN KEY (organisation_id, email_message_id) REFERENCES fyniq.email_message (organisation_id, id),
  CONSTRAINT notification_digest_cadence_known CHECK (cadence IN ('daily', 'weekly'))
);
ALTER TABLE fyniq.notification ADD CONSTRAINT notification_digest_fkey FOREIGN KEY (organisation_id, digest_id)
  REFERENCES fyniq.notification_digest (organisation_id, id);

CREATE TABLE fyniq.notification_preference (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  user_id         uuid        NOT NULL,
  type            text        NOT NULL,  -- an event type, or 'quiet_hours'
  email           text,                  -- immediate, daily, weekly or off
  quiet_from      smallint,              -- minutes since midnight, Singapore time
  quiet_to        smallint,
  set_at          timestamptz NOT NULL,
  CONSTRAINT notification_preference_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT notification_preference_member_fkey FOREIGN KEY (organisation_id, user_id)
    REFERENCES fyniq.user_membership (organisation_id, user_id),
  CONSTRAINT notification_preference_type_format CHECK (type ~ '^[a-z][a-z_]*(\.[a-z_]+)*$' AND length(type) <= 60),
  CONSTRAINT notification_preference_quiet_range CHECK (quiet_from BETWEEN 0 AND 1439 AND quiet_to BETWEEN 0 AND 1439),
  CONSTRAINT notification_preference_shape CHECK ((type = 'quiet_hours' AND email IS NULL AND (quiet_from IS NULL) = (quiet_to IS NULL))
    OR (type <> 'quiet_hours' AND email IN ('immediate', 'daily', 'weekly', 'off') AND quiet_from IS NULL AND quiet_to IS NULL))
);
CREATE INDEX notification_preference_latest ON fyniq.notification_preference (organisation_id, user_id, type, set_at);

-- An event never changes but for its fan-out, recorded once (FY187); a notification changes only in being read, unread or
-- archived, and in the email that carried it, each recorded once (FY188); digests and preferences are append-only (FY189).
-- +goose StatementBegin
CREATE FUNCTION fyniq.notification_event_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR OLD.fanned_out_at IS NOT NULL OR NEW.fanned_out_at IS NULL
     OR (NEW.type, NEW.record_type, NEW.record_id, NEW.title, NEW.link, NEW.actor, NEW.audience, NEW.occurred_at)
        IS DISTINCT FROM (OLD.type, OLD.record_type, OLD.record_id, OLD.title, OLD.link, OLD.actor, OLD.audience, OLD.occurred_at) THEN
    RAISE EXCEPTION 'A notification event never changes or goes' USING ERRCODE = 'FY187',
      HINT = 'Only its fan-out is recorded, once (FR-NTF-05).';
  END IF;
  RETURN NEW;
END
$$;

CREATE FUNCTION fyniq.notification_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE'
     OR (NEW.event_id, NEW.user_id, NEW.type, NEW.severity, NEW.actionable, NEW.email, NEW.created_at)
        IS DISTINCT FROM (OLD.event_id, OLD.user_id, OLD.type, OLD.severity, OLD.actionable, OLD.email, OLD.created_at)
     OR (OLD.email_message_id IS NOT NULL AND NEW.email_message_id IS DISTINCT FROM OLD.email_message_id)
     OR (OLD.digest_id IS NOT NULL AND NEW.digest_id IS DISTINCT FROM OLD.digest_id)
     OR (OLD.archived_at IS NOT NULL AND NEW.archived_at IS DISTINCT FROM OLD.archived_at) THEN
    RAISE EXCEPTION 'A notification changes only in being read, unread or archived' USING ERRCODE = 'FY188',
      HINT = 'Mark it read or unread, or archive it (FR-NTF-01).';
  END IF;
  RETURN NEW;
END
$$;

CREATE FUNCTION fyniq.notification_append_only() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION '% never changes or goes', TG_TABLE_NAME USING ERRCODE = 'FY189',
    HINT = 'Add a new one: the latest is in force (FR-NTF-02).';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER notification_event_guard BEFORE UPDATE OR DELETE ON fyniq.notification_event
  FOR EACH ROW EXECUTE FUNCTION fyniq.notification_event_guard();
CREATE TRIGGER notification_guard BEFORE UPDATE OR DELETE ON fyniq.notification
  FOR EACH ROW EXECUTE FUNCTION fyniq.notification_guard();
CREATE TRIGGER notification_digest_guard BEFORE UPDATE OR DELETE ON fyniq.notification_digest
  FOR EACH ROW EXECUTE FUNCTION fyniq.notification_append_only();
CREATE TRIGGER notification_preference_guard BEFORE UPDATE OR DELETE ON fyniq.notification_preference
  FOR EACH ROW EXECUTE FUNCTION fyniq.notification_append_only();
CREATE TRIGGER notification_event_no_truncate BEFORE TRUNCATE ON fyniq.notification_event
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();
CREATE TRIGGER notification_no_truncate BEFORE TRUNCATE ON fyniq.notification
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();
CREATE TRIGGER notification_digest_no_truncate BEFORE TRUNCATE ON fyniq.notification_digest
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();
CREATE TRIGGER notification_preference_no_truncate BEFORE TRUNCATE ON fyniq.notification_preference
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();

ALTER TABLE fyniq.notification_event ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.notification_event FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.notification_event
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.notification ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.notification FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.notification
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.notification_digest ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.notification_digest FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.notification_digest
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.notification_preference ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.notification_preference FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.notification_preference
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

-- What happened is audited where it happened; the event, the fan-out and preferences are audited here too.
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.notification_event
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('notification_event', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.notification_preference
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('notification_preference', 'id');

-- Whoever acts emits an event (the API, the worker's jobs); the worker fans out and sends digests; a person reads,
-- archives and sets preferences through the API.
GRANT SELECT ON fyniq.notification_event, fyniq.notification, fyniq.notification_digest, fyniq.notification_preference
  TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (type, record_type, record_id, title, link, actor, audience, occurred_at) ON fyniq.notification_event TO fyniq_api, fyniq_worker;
GRANT UPDATE (fanned_out_at) ON fyniq.notification_event TO fyniq_worker;
GRANT INSERT (event_id, user_id, type, severity, actionable, email, created_at) ON fyniq.notification TO fyniq_worker;
GRANT UPDATE (email_message_id, digest_id) ON fyniq.notification TO fyniq_worker;
GRANT UPDATE (read_at, archived_at) ON fyniq.notification TO fyniq_api;
GRANT INSERT (user_id, cadence, email_message_id, sent_at) ON fyniq.notification_digest TO fyniq_worker;
GRANT INSERT (user_id, type, email, quiet_from, quiet_to, set_at) ON fyniq.notification_preference TO fyniq_api;
-- The worker queues the emails of notifications and digests, and no other: a trigger holds it to those purposes, so a
-- test message or a sign-in alert is still only the API's to queue. It reads who the members are to fan out.
GRANT INSERT (purpose, recipient, subject) ON fyniq.email_message TO fyniq_worker;
-- +goose StatementBegin
CREATE FUNCTION fyniq.email_message_worker_purposes() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF current_user = 'fyniq_worker' AND NEW.purpose NOT IN ('notification', 'digest') THEN
    RAISE EXCEPTION 'The worker queues only notification and digest email, not %', NEW.purpose USING ERRCODE = '42501';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER email_message_worker_purposes BEFORE INSERT ON fyniq.email_message
  FOR EACH ROW EXECUTE FUNCTION fyniq.email_message_worker_purposes();
GRANT EXECUTE ON FUNCTION fyniq.member_names() TO fyniq_worker;
