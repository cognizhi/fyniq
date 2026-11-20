-- Outbound webhooks and events (PRD-001 FR-INT-12; p2-api-third-parties task 1; design D1 to D4): an organisation's endpoints, each with the event types it
-- asked for (an explicit allow-list, never a wildcard), a signing secret sealed at rest, a state (pending until the endpoint has echoed a challenge, then
-- active; paused or disabled), and the deliveries the worker makes: one per event and endpoint, written in the same transaction that fans the event out
-- (the transactional outbox, so none is lost and none exists for a change that did not commit), signed and sent at each attempt, retried for three days, then
-- dead for a person to replay. Every attempt is recorded, insert-only. Data in a delivery is the event's IDs and times, never a title or a detail.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.webhook_endpoint (
  organisation_id         uuid        NOT NULL DEFAULT fyniq.current_org(),
  id                      uuid        NOT NULL DEFAULT uuidv7(),
  url                     text        NOT NULL,
  label                   text        NOT NULL,
  event_types             text[]      NOT NULL,
  state                   text        NOT NULL DEFAULT 'pending',
  secret_sealed           bytea       NOT NULL,  -- the signing secret, sealed under the field key and bound to this endpoint; shown once when made
  secret_hint             text        NOT NULL,  -- the secret's last four characters, to tell two apart
  previous_secret_sealed  bytea,                 -- the secret before a roll, also signed with until previous_secret_until
  previous_secret_until   timestamptz,
  challenge_hash          text,                  -- SHA-256 of the challenge asked of the endpoint, until it has echoed it
  verified_at             timestamptz,
  failure_run             integer     NOT NULL DEFAULT 0,  -- deliveries failed in a row; ten pause the endpoint
  paused_reason           text,
  version                 integer     NOT NULL DEFAULT 1,
  created_by              text        NOT NULL,
  created_at              timestamptz NOT NULL,
  updated_at              timestamptz NOT NULL,
  CONSTRAINT webhook_endpoint_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT webhook_endpoint_state_known CHECK (state IN ('pending', 'active', 'paused', 'disabled')),
  CONSTRAINT webhook_endpoint_url_form CHECK (url ~ '^https?://[^[:space:]]+$' AND length(url) <= 2048),
  CONSTRAINT webhook_endpoint_label_length CHECK (length(label) BETWEEN 1 AND 100),
  CONSTRAINT webhook_endpoint_events CHECK (cardinality(event_types) BETWEEN 1 AND 50 AND array_position(event_types, NULL) IS NULL),
  CONSTRAINT webhook_endpoint_hint CHECK (length(secret_hint) = 4),
  CONSTRAINT webhook_endpoint_active_verified CHECK (state NOT IN ('active', 'paused') OR verified_at IS NOT NULL),
  CONSTRAINT webhook_endpoint_roll_form CHECK ((previous_secret_sealed IS NULL) = (previous_secret_until IS NULL)),
  CONSTRAINT webhook_endpoint_failures CHECK (failure_run >= 0),
  CONSTRAINT webhook_endpoint_paused_reason CHECK (paused_reason IS NULL OR length(paused_reason) BETWEEN 1 AND 500),
  CONSTRAINT webhook_endpoint_version_positive CHECK (version >= 1),
  CONSTRAINT webhook_endpoint_by_form CHECK (created_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);
CREATE INDEX webhook_endpoint_state ON fyniq.webhook_endpoint (organisation_id, state);

CREATE TABLE fyniq.webhook_delivery (
  organisation_id   uuid        NOT NULL DEFAULT fyniq.current_org(),
  id                uuid        NOT NULL DEFAULT uuidv7(),
  endpoint_id       uuid        NOT NULL,
  event_id          uuid        NOT NULL,   -- the notification event it carries
  event_type        text        NOT NULL,   -- fyniq.<object>.<event>.v<N>
  message_id        text        NOT NULL,   -- the webhook-id: the same on every attempt and every replay
  payload           jsonb       NOT NULL,   -- the CloudEvent's data: IDs and times only
  state             text        NOT NULL DEFAULT 'pending',
  attempts          integer     NOT NULL DEFAULT 0,
  replays           integer     NOT NULL DEFAULT 0,
  next_attempt_at   timestamptz NOT NULL,
  first_attempt_at  timestamptz,
  last_attempt_at   timestamptz,
  last_status       integer,
  last_error        text,
  delivered_at      timestamptz,
  created_at        timestamptz NOT NULL,
  CONSTRAINT webhook_delivery_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT webhook_delivery_endpoint_fkey FOREIGN KEY (organisation_id, endpoint_id) REFERENCES fyniq.webhook_endpoint (organisation_id, id),
  CONSTRAINT webhook_delivery_once UNIQUE (organisation_id, endpoint_id, event_id),
  CONSTRAINT webhook_delivery_message_once UNIQUE (organisation_id, message_id),
  CONSTRAINT webhook_delivery_state_known CHECK (state IN ('pending', 'delivered', 'dead')),
  CONSTRAINT webhook_delivery_delivered_form CHECK ((state = 'delivered') = (delivered_at IS NOT NULL)),
  CONSTRAINT webhook_delivery_type_form CHECK (event_type ~ '^fyniq\.[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*\.v[1-9][0-9]*$'),
  CONSTRAINT webhook_delivery_payload_object CHECK (jsonb_typeof(payload) = 'object' AND octet_length(payload::text) <= 8192),
  CONSTRAINT webhook_delivery_counts CHECK (attempts >= 0 AND replays >= 0),
  CONSTRAINT webhook_delivery_error_length CHECK (last_error IS NULL OR length(last_error) <= 200)
);
CREATE INDEX webhook_delivery_due ON fyniq.webhook_delivery (next_attempt_at) WHERE state = 'pending';
CREATE INDEX webhook_delivery_endpoint ON fyniq.webhook_delivery (organisation_id, endpoint_id, created_at DESC);

-- Every attempt, insert-only: when, the answer's status (none when the connection failed), why it failed in a few words, and how long it took.
CREATE TABLE fyniq.webhook_attempt (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  delivery_id     uuid        NOT NULL,
  number          integer     NOT NULL,
  at              timestamptz NOT NULL,
  status          integer,
  error           text,
  duration_ms     integer     NOT NULL,
  CONSTRAINT webhook_attempt_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT webhook_attempt_delivery_fkey FOREIGN KEY (organisation_id, delivery_id) REFERENCES fyniq.webhook_delivery (organisation_id, id),
  CONSTRAINT webhook_attempt_error_length CHECK (error IS NULL OR length(error) <= 200),
  CONSTRAINT webhook_attempt_duration CHECK (duration_ms >= 0)
);
CREATE INDEX webhook_attempt_delivery ON fyniq.webhook_attempt (organisation_id, delivery_id, number);

-- An endpoint's identity and its secrets' seal never change but through the API; the worker may only count failures and pause or disable an active endpoint
-- (FYW01); nothing is deleted. A delivery's endpoint, event, type, message ID and payload never change; the API may only put a dead delivery back to pending,
-- once more each time (FYW02); the worker may not touch a delivered one; attempts are never changed or deleted (FYW03).
-- +goose StatementBegin
CREATE FUNCTION fyniq.webhook_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR TG_OP = 'TRUNCATE' THEN
    RAISE EXCEPTION 'A webhook record is never deleted' USING ERRCODE = 'FYW01', HINT = 'Disable an endpoint instead (FR-INT-12).';
  END IF;
  IF TG_TABLE_NAME = 'webhook_endpoint' THEN
    IF (NEW.organisation_id, NEW.id, NEW.created_by, NEW.created_at) IS DISTINCT FROM (OLD.organisation_id, OLD.id, OLD.created_by, OLD.created_at) THEN
      RAISE EXCEPTION 'A webhook endpoint keeps its identity' USING ERRCODE = 'FYW01';
    END IF;
    IF current_user = 'fyniq_worker' THEN
      IF (NEW.url, NEW.label, NEW.event_types, NEW.secret_sealed, NEW.secret_hint, NEW.previous_secret_sealed, NEW.previous_secret_until, NEW.challenge_hash, NEW.verified_at)
           IS DISTINCT FROM (OLD.url, OLD.label, OLD.event_types, OLD.secret_sealed, OLD.secret_hint, OLD.previous_secret_sealed, OLD.previous_secret_until, OLD.challenge_hash, OLD.verified_at)
         OR (NEW.state IS DISTINCT FROM OLD.state AND NOT (OLD.state = 'active' AND NEW.state IN ('paused', 'disabled'))) THEN
        RAISE EXCEPTION 'The worker counts an endpoint''s failures and pauses or disables an active one; nothing else' USING ERRCODE = 'FYW01';
      END IF;
    END IF;
  ELSE
    IF (NEW.organisation_id, NEW.id, NEW.endpoint_id, NEW.event_id, NEW.event_type, NEW.message_id, NEW.payload, NEW.created_at)
         IS DISTINCT FROM (OLD.organisation_id, OLD.id, OLD.endpoint_id, OLD.event_id, OLD.event_type, OLD.message_id, OLD.payload, OLD.created_at) THEN
      RAISE EXCEPTION 'A webhook delivery keeps what it carries' USING ERRCODE = 'FYW02';
    END IF;
    IF OLD.state = 'delivered' THEN
      RAISE EXCEPTION 'A delivered webhook is never changed' USING ERRCODE = 'FYW02';
    END IF;
    IF current_user = 'fyniq_api' AND NOT (OLD.state = 'dead' AND NEW.state = 'pending' AND NEW.replays = OLD.replays + 1 AND NEW.attempts = OLD.attempts) THEN
      RAISE EXCEPTION 'The API only puts a dead delivery back for another try' USING ERRCODE = 'FYW02';
    END IF;
    IF current_user = 'fyniq_worker' AND OLD.state = 'dead' THEN
      RAISE EXCEPTION 'A dead delivery waits for a person to replay it' USING ERRCODE = 'FYW02';
    END IF;
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER webhook_guard BEFORE UPDATE OR DELETE ON fyniq.webhook_endpoint FOR EACH ROW EXECUTE FUNCTION fyniq.webhook_guard();
CREATE TRIGGER webhook_guard BEFORE UPDATE OR DELETE ON fyniq.webhook_delivery FOR EACH ROW EXECUTE FUNCTION fyniq.webhook_guard();
-- +goose StatementBegin
CREATE FUNCTION fyniq.webhook_attempt_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'A recorded webhook attempt is never changed or deleted' USING ERRCODE = 'FYW03', HINT = 'It is the log of what was sent (FR-INT-12).';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER webhook_attempt_guard BEFORE UPDATE OR DELETE ON fyniq.webhook_attempt FOR EACH ROW EXECUTE FUNCTION fyniq.webhook_attempt_guard();
CREATE TRIGGER webhook_endpoint_no_truncate BEFORE TRUNCATE ON fyniq.webhook_endpoint FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();
CREATE TRIGGER webhook_delivery_no_truncate BEFORE TRUNCATE ON fyniq.webhook_delivery FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();
CREATE TRIGGER webhook_attempt_no_truncate BEFORE TRUNCATE ON fyniq.webhook_attempt FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();

ALTER TABLE fyniq.webhook_endpoint ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.webhook_endpoint FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.webhook_endpoint USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.webhook_delivery ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.webhook_delivery FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.webhook_delivery USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.webhook_attempt ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.webhook_attempt FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.webhook_attempt USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

-- A change to an endpoint is audited in its transaction (AUD-01); deliveries and attempts are the delivery log, whose replays and dead-lettering the code audits.
-- A count of failures (failure_run, updated_at) is not a change of the endpoint and is not audited; pausing, disabling and every setting are.
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE OF url, label, event_types, state, secret_sealed, secret_hint, previous_secret_sealed, previous_secret_until, challenge_hash,
  verified_at, paused_reason, version ON fyniq.webhook_endpoint
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('webhook_endpoint', 'id');

GRANT SELECT ON fyniq.webhook_endpoint, fyniq.webhook_delivery, fyniq.webhook_attempt TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (id, url, label, event_types, secret_sealed, secret_hint, created_by, created_at, updated_at) ON fyniq.webhook_endpoint TO fyniq_api;
GRANT UPDATE (url, label, event_types, state, secret_sealed, secret_hint, previous_secret_sealed, previous_secret_until, challenge_hash, verified_at, failure_run, paused_reason, version, updated_at)
  ON fyniq.webhook_endpoint TO fyniq_api;
GRANT UPDATE (state, failure_run, paused_reason, version, updated_at) ON fyniq.webhook_endpoint TO fyniq_worker;
GRANT INSERT (endpoint_id, event_id, event_type, message_id, payload, next_attempt_at, created_at) ON fyniq.webhook_delivery TO fyniq_worker;
GRANT UPDATE (state, attempts, next_attempt_at, first_attempt_at, last_attempt_at, last_status, last_error, delivered_at) ON fyniq.webhook_delivery TO fyniq_worker;
GRANT UPDATE (state, replays, next_attempt_at) ON fyniq.webhook_delivery TO fyniq_api;
GRANT INSERT (delivery_id, number, at, status, error, duration_ms) ON fyniq.webhook_attempt TO fyniq_worker;
