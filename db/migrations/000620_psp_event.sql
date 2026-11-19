-- The events a payment provider's signed webhooks bring (PRD-001 FR-PSP-01; p1-payment-providers task 1.5, design D3).
--
-- A verified webhook is stored once per connector and provider event ID: the raw payload (sealed, SEC-11, because it
-- carries a customer's name, email and address), its SHA-256, the provider's API version and the parser's version (so a
-- later reading of the same payload is explained), what the event names, and where it stands. An event only tells Fyniq
-- which object to fetch: nothing posts from this table. A redelivery is the same row (2xx, nothing changed). An event
-- whose type the parser does not know is parked for a person (UC-PSP-01); it posts nothing.
--
-- The row is evidence of what arrived: what it was received as never changes (FY620), and its status moves forward only
-- (FY621): received to processed, parked or failed; failed to processed, parked or failed again (a retry); parked to
-- processed once a person has dealt with it. Nothing is deleted.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.psp_event (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  connector_id    uuid        NOT NULL,
  provider        text        NOT NULL,  -- the connector's manifest ID, such as stripe or hitpay
  event_id        text        NOT NULL,  -- the provider's event ID, or what the parser made of the event (HitPay sends none)
  event_type      text        NOT NULL,
  api_version     text,
  parser_version  text        NOT NULL,  -- the manifest's parser_version, such as stripe@1
  object_kind     text,                  -- what the event names, when it names an object to fetch
  object_id       text,
  payload         bytea       NOT NULL,  -- the raw body as received, sealed by ports.Cipher bound to this row
  payload_sha256  text        NOT NULL,  -- of the raw body, to prove a payload is the one received
  received_at     timestamptz NOT NULL,
  status          text        NOT NULL DEFAULT 'received',
  parked_reason   text,
  processed_at    timestamptz,
  last_error      text,
  CONSTRAINT psp_event_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT psp_event_connector_fkey FOREIGN KEY (organisation_id, connector_id)
    REFERENCES fyniq.organisation_connector (organisation_id, id),
  CONSTRAINT psp_event_once UNIQUE (organisation_id, connector_id, event_id),
  CONSTRAINT psp_event_provider_form CHECK (provider ~ '^[a-z][a-z0-9-]{1,39}$'), -- a connector's manifest ID: a new provider needs no migration
  CONSTRAINT psp_event_status_known CHECK (status IN ('received', 'processed', 'parked', 'failed')),
  CONSTRAINT psp_event_kind_known CHECK (object_kind IN ('charge', 'refund', 'dispute', 'payout', 'invoice', 'credit_note', 'subscription', 'customer')),
  CONSTRAINT psp_event_lengths CHECK (length(event_id) BETWEEN 1 AND 255 AND length(event_type) BETWEEN 1 AND 120
    AND length(api_version) BETWEEN 1 AND 120 AND length(parser_version) BETWEEN 3 AND 40 AND length(object_id) BETWEEN 1 AND 255
    AND payload_sha256 ~ '^[0-9a-f]{64}$' AND length(payload) BETWEEN 1 AND 2097152
    AND length(parked_reason) BETWEEN 1 AND 500 AND length(last_error) BETWEEN 1 AND 500),
  CONSTRAINT psp_event_object_pair CHECK (object_kind IS NULL OR object_id IS NOT NULL), -- an unknown type may name an object it has no kind for
  CONSTRAINT psp_event_parked_reason CHECK ((status = 'parked') = (parked_reason IS NOT NULL)),
  CONSTRAINT psp_event_processed_at CHECK ((status = 'processed') = (processed_at IS NOT NULL))
);
CREATE INDEX psp_event_status ON fyniq.psp_event (organisation_id, status, received_at);
CREATE INDEX psp_event_object ON fyniq.psp_event (organisation_id, connector_id, object_kind, object_id);

-- +goose StatementBegin
CREATE FUNCTION fyniq.psp_event_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE'
     OR (NEW.connector_id, NEW.provider, NEW.event_id, NEW.event_type, NEW.api_version, NEW.parser_version, NEW.object_kind,
         NEW.object_id, NEW.payload, NEW.payload_sha256, NEW.received_at)
        IS DISTINCT FROM (OLD.connector_id, OLD.provider, OLD.event_id, OLD.event_type, OLD.api_version, OLD.parser_version,
         OLD.object_kind, OLD.object_id, OLD.payload, OLD.payload_sha256, OLD.received_at) THEN
    RAISE EXCEPTION 'A provider event is kept as it was received' USING ERRCODE = 'FY620',
      HINT = 'Only its standing changes: the payload, its type and its object never do (FR-PSP-01).';
  END IF;
  IF NEW.status <> OLD.status AND NOT (
       (OLD.status = 'received' AND NEW.status IN ('processed', 'parked', 'failed'))
    OR (OLD.status = 'failed' AND NEW.status IN ('processed', 'parked'))
    OR (OLD.status = 'parked' AND NEW.status = 'processed')) THEN
    RAISE EXCEPTION 'A provider event''s status moves forward only' USING ERRCODE = 'FY621',
      HINT = 'Received becomes processed, parked or failed; failed or parked ones are processed once dealt with.';
  END IF;
  IF NEW.status = OLD.status AND OLD.status IN ('processed') THEN
    RAISE EXCEPTION 'A provider event''s status moves forward only' USING ERRCODE = 'FY621',
      HINT = 'A processed event is final.';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER psp_event_guard BEFORE UPDATE OR DELETE ON fyniq.psp_event
  FOR EACH ROW EXECUTE FUNCTION fyniq.psp_event_guard();
CREATE TRIGGER psp_event_no_truncate BEFORE TRUNCATE ON fyniq.psp_event
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();

ALTER TABLE fyniq.psp_event ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.psp_event FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.psp_event
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE OF status ON fyniq.psp_event
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('psp_event', 'id');

-- The API receives and records the webhook; the worker works it and records where it stands.
GRANT SELECT ON fyniq.psp_event TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (id, connector_id, provider, event_id, event_type, api_version, parser_version, object_kind, object_id, payload,
              payload_sha256, received_at) ON fyniq.psp_event TO fyniq_api;
GRANT UPDATE (status, parked_reason, processed_at, last_error) ON fyniq.psp_event TO fyniq_api, fyniq_worker;
