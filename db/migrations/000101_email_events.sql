-- What the email providers report about a message, and the addresses not to send to (PRD-001 FR-INT-21, rev 0.103;
-- p1-templates-email task 2.1, third slice).
--
-- A provider's webhook, once its signature is verified, records each event about a message it took: delivered,
-- delayed, a soft or hard bounce, a complaint, suppressed by the provider, or failed. A delivery the provider repeats is
-- recorded once (its delivery ID is unique per provider). Opens and clicks are not recorded. An event never changes or
-- goes (FY180). The message's own outcome (accepted, failed, unknown) is final and stays as it is: an event is about
-- what happened after the provider took it, and some providers never report "delivered".
--
-- A hard bounce, a complaint or the provider's suppression puts the recipient on the organisation's suppression list:
-- nothing more is sent to it until an Administrator releases it. A suppression keeps its address, reason and event;
-- only its release is recorded on it, once (FY181).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.email_event (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  message_id      uuid        NOT NULL,
  provider        text        NOT NULL,
  delivery_id     text        NOT NULL,  -- the provider's ID of the event, the same on each retry
  kind            text        NOT NULL,
  recipient       text        NOT NULL,
  detail          text,                  -- the provider's reason, kept short
  occurred_at     timestamptz NOT NULL,
  received_at     timestamptz NOT NULL,
  CONSTRAINT email_event_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT email_event_message_fkey FOREIGN KEY (organisation_id, message_id)
    REFERENCES fyniq.email_message (organisation_id, id),
  CONSTRAINT email_event_once UNIQUE (organisation_id, provider, delivery_id),
  CONSTRAINT email_event_provider_known CHECK (provider IN ('zeptomail', 'resend')),
  CONSTRAINT email_event_kind_known CHECK (kind IN ('delivered', 'delayed', 'soft_bounce', 'hard_bounce', 'complaint', 'suppressed',
    'failed')),
  CONSTRAINT email_event_lengths CHECK (length(delivery_id) BETWEEN 1 AND 200 AND length(recipient) BETWEEN 3 AND 254
    AND length(detail) BETWEEN 1 AND 500)
);
CREATE INDEX email_event_message ON fyniq.email_event (organisation_id, message_id, occurred_at);

CREATE TABLE fyniq.email_suppression (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  address         text        NOT NULL,  -- in lower case
  reason          text        NOT NULL,
  event_id        uuid        NOT NULL,
  suppressed_at   timestamptz NOT NULL,
  released_at     timestamptz,
  released_by     text,
  CONSTRAINT email_suppression_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT email_suppression_event_fkey FOREIGN KEY (organisation_id, event_id)
    REFERENCES fyniq.email_event (organisation_id, id),
  CONSTRAINT email_suppression_reason_known CHECK (reason IN ('hard_bounce', 'complaint', 'suppressed')),
  CONSTRAINT email_suppression_address_lower CHECK (address = lower(address) AND length(address) BETWEEN 3 AND 254),
  CONSTRAINT email_suppression_released_by CHECK ((released_at IS NULL) = (released_by IS NULL)
    AND released_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);
-- One suppression in force per address.
CREATE UNIQUE INDEX email_suppression_in_force ON fyniq.email_suppression (organisation_id, address) WHERE released_at IS NULL;

-- An event never changes or goes (FY180); a suppression keeps all but its release, recorded once (FY181).
-- +goose StatementBegin
CREATE FUNCTION fyniq.email_event_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'An email event never changes or goes' USING ERRCODE = 'FY180',
    HINT = 'A provider''s later event is recorded beside it (FR-INT-21).';
END
$$;

CREATE FUNCTION fyniq.email_suppression_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR OLD.released_at IS NOT NULL OR NEW.released_at IS NULL
     OR (NEW.address, NEW.reason, NEW.event_id, NEW.suppressed_at) IS DISTINCT FROM (OLD.address, OLD.reason, OLD.event_id, OLD.suppressed_at) THEN
    RAISE EXCEPTION 'An email suppression is only ever released, once' USING ERRCODE = 'FY181',
      HINT = 'Release it; a later bounce suppresses the address again (FR-INT-21).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER email_event_guard BEFORE UPDATE OR DELETE ON fyniq.email_event
  FOR EACH ROW EXECUTE FUNCTION fyniq.email_event_guard();
CREATE TRIGGER email_event_no_truncate BEFORE TRUNCATE ON fyniq.email_event
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();
CREATE TRIGGER email_suppression_guard BEFORE UPDATE OR DELETE ON fyniq.email_suppression
  FOR EACH ROW EXECUTE FUNCTION fyniq.email_suppression_guard();
CREATE TRIGGER email_suppression_no_truncate BEFORE TRUNCATE ON fyniq.email_suppression
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();

ALTER TABLE fyniq.email_event ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.email_event FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.email_event
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.email_suppression ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.email_suppression FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.email_suppression
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.email_event
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('email_message', 'message_id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.email_suppression
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('email_suppression', 'id');

-- The API receives the webhooks and releases suppressions; the worker reads them before it sends.
GRANT SELECT ON fyniq.email_event, fyniq.email_suppression TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (message_id, provider, delivery_id, kind, recipient, detail, occurred_at, received_at) ON fyniq.email_event TO fyniq_api;
GRANT INSERT (address, reason, event_id, suppressed_at) ON fyniq.email_suppression TO fyniq_api;
GRANT UPDATE (released_at, released_by) ON fyniq.email_suppression TO fyniq_api;
