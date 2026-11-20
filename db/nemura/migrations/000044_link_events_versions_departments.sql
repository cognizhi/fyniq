-- The events Nemura sends to Fyniq, their versions, and the departments Fyniq tells Nemura of (PRD-002 NM-INT-01, 04, 05, 10; p1-nemura-fyniq-link tasks 2.2, 3.3, 5.1).
--
--  * link_event is the durable, identity-only log of what was queued for Fyniq: the event's ID and sequence, its type, the object and its version, and where it stands (queued, sent, failed, refused).
--    The outbox's rows are purged once done, and the link's health (events waiting or failed, the last sync), its retry and its gap repair all need to know what was sent. It never holds a payload:
--    a payload is built from the source rows when it is sent (design D5). Its identity never changes; the worker alone moves its state.
--  * link_object_version is the version a person's events carry. A person record changes in place, so its events number themselves: one more each time the record changes while the link is active.
--    A bank-detail version is the ordinal of the person's confirmed bank account changes and a hold's is its own version; neither needs a counter.
--  * link_mapping and link_department are what Fyniq says of its departments (people.department.changed, PRD-002 Appendix E): the version of its payroll account mapping and each department's value ID,
--    name and cost centre. Append-only by mapping version; the newest is current. A pay run is sent with the version it was calculated against and with Fyniq's own department IDs.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE nemura.link_event (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  link_id         uuid        NOT NULL,
  event_id        uuid        NOT NULL,
  epoch           integer     NOT NULL,
  sequence        bigint      NOT NULL,
  type            text        NOT NULL,
  object_type     text        NOT NULL,
  object_id       text        NOT NULL,
  object_version  integer     NOT NULL,
  state           text        NOT NULL DEFAULT 'queued',
  attempts        integer     NOT NULL DEFAULT 0,
  code            text,
  queued_at       timestamptz NOT NULL,
  settled_at      timestamptz,
  CONSTRAINT link_event_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT link_event_event_key UNIQUE (organisation_id, event_id),
  CONSTRAINT link_event_sequence_key UNIQUE (organisation_id, link_id, sequence),
  CONSTRAINT link_event_link_fkey FOREIGN KEY (organisation_id, link_id) REFERENCES nemura.fyniq_link (organisation_id, id),
  CONSTRAINT link_event_state_known CHECK (state IN ('queued', 'sent', 'failed', 'refused')),
  CONSTRAINT link_event_settled_with_state CHECK ((state IN ('sent', 'refused')) = (settled_at IS NOT NULL)),
  CONSTRAINT link_event_code_length CHECK (code IS NULL OR char_length(code) <= 200)
);
CREATE INDEX link_event_state ON nemura.link_event (organisation_id, link_id, state, sequence);
CREATE INDEX link_event_object ON nemura.link_event (organisation_id, object_type, object_id, object_version);

-- +goose StatementBegin
CREATE FUNCTION nemura.link_event_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR (NEW.id, NEW.link_id, NEW.event_id, NEW.epoch, NEW.sequence, NEW.type, NEW.object_type, NEW.object_id, NEW.object_version, NEW.queued_at) IS DISTINCT FROM
     (OLD.id, OLD.link_id, OLD.event_id, OLD.epoch, OLD.sequence, OLD.type, OLD.object_type, OLD.object_id, OLD.object_version, OLD.queued_at)
     OR (OLD.state IN ('sent', 'refused') AND NEW.state IS DISTINCT FROM OLD.state) OR NEW.attempts < OLD.attempts THEN
    RAISE EXCEPTION 'The log of what was sent to Fyniq is kept as it was queued: only its state moves, and a settled event stays settled' USING ERRCODE = 'NM192';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER link_event_guard BEFORE UPDATE OR DELETE ON nemura.link_event FOR EACH ROW EXECUTE FUNCTION nemura.link_event_guard();

CREATE TABLE nemura.link_object_version (
  organisation_id uuid    NOT NULL DEFAULT nemura.current_org(),
  object_type     text    NOT NULL,
  object_id       text    NOT NULL,
  version         integer NOT NULL,
  CONSTRAINT link_object_version_pkey PRIMARY KEY (organisation_id, object_type, object_id),
  CONSTRAINT link_object_version_positive CHECK (version >= 1)
);

CREATE TABLE nemura.link_mapping (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  mapping_version integer     NOT NULL,
  event_id        uuid        NOT NULL,
  received_at     timestamptz NOT NULL,
  CONSTRAINT link_mapping_pkey PRIMARY KEY (organisation_id, mapping_version),
  CONSTRAINT link_mapping_positive CHECK (mapping_version >= 1)
);

CREATE TABLE nemura.link_department (
  organisation_id uuid NOT NULL DEFAULT nemura.current_org(),
  mapping_version integer NOT NULL,
  department_id   uuid NOT NULL,   -- Fyniq's own dimension value
  name            text NOT NULL,
  cost_centre_id  uuid,
  CONSTRAINT link_department_pkey PRIMARY KEY (organisation_id, mapping_version, department_id),
  CONSTRAINT link_department_mapping_fkey FOREIGN KEY (organisation_id, mapping_version) REFERENCES nemura.link_mapping (organisation_id, mapping_version),
  CONSTRAINT link_department_name_length CHECK (char_length(name) BETWEEN 1 AND 200)
);

CREATE TRIGGER link_mapping_guard BEFORE UPDATE OR DELETE ON nemura.link_mapping FOR EACH ROW EXECUTE FUNCTION nemura.link_append_only();
CREATE TRIGGER link_department_guard BEFORE UPDATE OR DELETE ON nemura.link_department FOR EACH ROW EXECUTE FUNCTION nemura.link_append_only();

ALTER TABLE nemura.link_event ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.link_event FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.link_event USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.link_object_version ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.link_object_version FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.link_object_version USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.link_mapping ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.link_mapping FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.link_mapping USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.link_department ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.link_department FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.link_department USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());

REVOKE ALL ON nemura.link_event, nemura.link_object_version, nemura.link_mapping, nemura.link_department FROM PUBLIC;
GRANT SELECT ON nemura.link_event, nemura.link_object_version, nemura.link_mapping, nemura.link_department TO nemura_api, nemura_worker, nemura_readonly;
-- A change is queued in the transaction that made it, by whichever role made it; the worker alone settles it.
GRANT INSERT (link_id, event_id, epoch, sequence, type, object_type, object_id, object_version, queued_at) ON nemura.link_event TO nemura_api, nemura_worker;
GRANT UPDATE (state, attempts, code, settled_at) ON nemura.link_event TO nemura_worker;
GRANT INSERT (object_type, object_id, version), UPDATE (version) ON nemura.link_object_version TO nemura_api, nemura_worker;
-- What Fyniq says of its departments arrives through the inbox, which the API runs.
GRANT INSERT (mapping_version, event_id, received_at) ON nemura.link_mapping TO nemura_api;
GRANT INSERT (mapping_version, department_id, name, cost_centre_id) ON nemura.link_department TO nemura_api;
