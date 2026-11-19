-- Nemura's transactional outbox (PRD-002 NM-NFR-03, ADR-N5; PRD-001 §12.11, ADR-T7): background work is written as a row in
-- the same transaction as the change it belongs to, so no job is lost and none is invented. After commit the row is
-- dispatched to its worker class (Cloud Tasks; locally the push dispatcher); a handler takes a lease on it before working,
-- and the tick sweeps rows that never left or overran. The same table queues the hand-off to Fyniq when it is down.
--
-- States: pending -> dispatched -> running -> done | failed | dead. A failed row is dispatched again until its attempts run
-- out, then it is dead (with a manual replay back to pending). Unknown outcomes and holds arrive with the first call to an
-- outside service.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE nemura.outbox (
  organisation_id  uuid        NOT NULL DEFAULT nemura.current_org(),
  id               uuid        NOT NULL DEFAULT uuidv7(),
  worker_class     text        NOT NULL,
  kind             text        NOT NULL,
  payload          jsonb       NOT NULL DEFAULT '{}',
  business_key     text,
  state            text        NOT NULL DEFAULT 'pending',
  attempts         integer     NOT NULL DEFAULT 0,
  max_attempts     integer     NOT NULL DEFAULT 5,
  dispatches       integer     NOT NULL DEFAULT 0,
  task_name        text,
  lease_holder     text,
  lease_expires_at timestamptz,
  deadline_at      timestamptz,
  last_error       text,
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT outbox_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT outbox_business_key UNIQUE (organisation_id, kind, business_key),
  CONSTRAINT outbox_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES nemura.organisation (organisation_id),
  CONSTRAINT outbox_worker_class_known CHECK (worker_class IN ('worker', 'executor')),
  CONSTRAINT outbox_kind_format CHECK (kind ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$' AND length(kind) <= 100),
  CONSTRAINT outbox_payload_object CHECK (jsonb_typeof(payload) = 'object' AND octet_length(payload::text) <= 262144),
  CONSTRAINT outbox_business_key_length CHECK (length(business_key) BETWEEN 1 AND 500),
  CONSTRAINT outbox_state_known CHECK (state IN ('pending', 'dispatched', 'running', 'done', 'failed', 'dead')),
  CONSTRAINT outbox_attempts_range CHECK (attempts >= 0 AND max_attempts BETWEEN 1 AND 100),
  CONSTRAINT outbox_dispatches_not_negative CHECK (dispatches >= 0),
  CONSTRAINT outbox_lease_whole CHECK ((lease_holder IS NULL) = (lease_expires_at IS NULL)),
  CONSTRAINT outbox_task_name_length CHECK (length(task_name) <= 500),
  CONSTRAINT outbox_last_error_length CHECK (length(last_error) <= 2000)
);
CREATE INDEX outbox_state_idx ON nemura.outbox (organisation_id, state, deadline_at);

-- What a row is never changes; its state moves only along the contract.
-- +goose StatementBegin
CREATE FUNCTION nemura.outbox_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF NEW.worker_class IS DISTINCT FROM OLD.worker_class OR NEW.kind IS DISTINCT FROM OLD.kind
     OR NEW.payload IS DISTINCT FROM OLD.payload OR NEW.business_key IS DISTINCT FROM OLD.business_key
     OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
    RAISE EXCEPTION 'Outbox row % cannot change what it is', OLD.id USING ERRCODE = 'NM070';
  END IF;
  IF NEW.state IS DISTINCT FROM OLD.state AND NOT (
       (OLD.state = 'pending'    AND NEW.state IN ('dispatched', 'dead')) OR
       (OLD.state = 'dispatched' AND NEW.state IN ('running', 'dead')) OR
       (OLD.state = 'running'    AND NEW.state IN ('done', 'failed', 'dispatched', 'dead')) OR
       (OLD.state = 'failed'     AND NEW.state IN ('running', 'dispatched', 'dead')) OR
       (OLD.state = 'dead'       AND NEW.state IN ('pending'))) THEN
    RAISE EXCEPTION 'Outbox row % cannot go from % to %', OLD.id, OLD.state, NEW.state
      USING ERRCODE = 'NM071', HINT = 'pending -> dispatched -> running -> done | failed | dead (§12.11).';
  END IF;
  IF NEW.attempts < OLD.attempts OR NEW.dispatches < OLD.dispatches THEN
    RAISE EXCEPTION 'Outbox row % cannot count attempts or dispatches backwards', OLD.id USING ERRCODE = 'NM070';
  END IF;
  NEW.updated_at := now();
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER outbox_guard BEFORE UPDATE ON nemura.outbox
  FOR EACH ROW EXECUTE FUNCTION nemura.outbox_guard();

-- +goose StatementBegin
CREATE FUNCTION nemura.outbox_no_delete() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'Outbox rows are kept (% on %)', TG_OP, TG_TABLE_NAME USING ERRCODE = 'NM070';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER outbox_no_delete BEFORE DELETE ON nemura.outbox
  FOR EACH ROW EXECUTE FUNCTION nemura.outbox_no_delete();
CREATE TRIGGER outbox_no_truncate BEFORE TRUNCATE ON nemura.outbox
  FOR EACH STATEMENT EXECUTE FUNCTION nemura.outbox_no_delete();

-- ------------------------------------------------------------------ row-level security
ALTER TABLE nemura.outbox ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.outbox FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.outbox
  USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());

-- ------------------------------------------------------------------ grants (db/README.md)
-- Commands write rows; the API dispatches them; handlers lease and finish them.
GRANT SELECT ON nemura.outbox TO nemura_api, nemura_worker, nemura_executor, nemura_readonly;
GRANT INSERT (worker_class, kind, payload, business_key, max_attempts)
  ON nemura.outbox TO nemura_api, nemura_worker, nemura_executor;
GRANT UPDATE (state, attempts, dispatches, task_name, lease_holder, lease_expires_at, deadline_at, last_error)
  ON nemura.outbox TO nemura_api, nemura_worker, nemura_executor;
