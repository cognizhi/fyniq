-- The transactional outbox (PRD-001 §12.11, ADR-T7): background work is written as a row in the same
-- transaction as the change it belongs to, so no job is lost and none is invented. After commit the
-- row is dispatched to its worker class (Cloud Tasks; locally the push dispatcher, rev 0.23); a
-- handler takes a lease on it before working, and the tick sweeps rows that never left or overran.
--
-- States: pending → dispatched → running → done | failed | unknown_outcome | dead. A failed row is
-- dispatched again until its attempts run out, then it is dead (Checks C.2 #12, with a manual
-- replay back to pending). An unknown outcome (a time-out after sending to an outside service) is
-- reconciled with the provider before anything else happens to it.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.outbox (
  organisation_id  uuid        NOT NULL DEFAULT fyniq.current_org(),
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
  job_execution    text,
  lease_holder     text,
  lease_expires_at timestamptz,
  deadline_at      timestamptz,
  last_error       text,
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT outbox_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT outbox_business_key UNIQUE (organisation_id, kind, business_key),
  CONSTRAINT outbox_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT outbox_worker_class_known CHECK (worker_class IN ('worker', 'executor', 'jobs')),
  CONSTRAINT outbox_kind_format CHECK (kind ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$' AND length(kind) <= 100),
  CONSTRAINT outbox_payload_object CHECK (jsonb_typeof(payload) = 'object' AND octet_length(payload::text) <= 262144),
  CONSTRAINT outbox_business_key_length CHECK (length(business_key) BETWEEN 1 AND 500),
  CONSTRAINT outbox_state_known
    CHECK (state IN ('pending', 'dispatched', 'running', 'done', 'failed', 'unknown_outcome', 'dead')),
  CONSTRAINT outbox_attempts_range CHECK (attempts >= 0 AND max_attempts BETWEEN 1 AND 100),
  CONSTRAINT outbox_dispatches_not_negative CHECK (dispatches >= 0),
  CONSTRAINT outbox_lease_whole CHECK ((lease_holder IS NULL) = (lease_expires_at IS NULL)),
  CONSTRAINT outbox_task_name_length CHECK (length(task_name) <= 500),
  CONSTRAINT outbox_last_error_length CHECK (length(last_error) <= 2000)
);
CREATE INDEX outbox_state_idx ON fyniq.outbox (organisation_id, state, deadline_at);

-- What a row is never changes; its state moves only along the contract.
-- +goose StatementBegin
CREATE FUNCTION fyniq.outbox_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF NEW.worker_class IS DISTINCT FROM OLD.worker_class OR NEW.kind IS DISTINCT FROM OLD.kind
     OR NEW.payload IS DISTINCT FROM OLD.payload OR NEW.business_key IS DISTINCT FROM OLD.business_key
     OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
    RAISE EXCEPTION 'Outbox row % cannot change what it is', OLD.id USING ERRCODE = 'FY070';
  END IF;
  IF NEW.state IS DISTINCT FROM OLD.state AND NOT (
       (OLD.state = 'pending'         AND NEW.state IN ('dispatched', 'dead', 'unknown_outcome')) OR
       (OLD.state = 'dispatched'      AND NEW.state IN ('running', 'dead', 'unknown_outcome')) OR
       (OLD.state = 'running'         AND NEW.state IN ('done', 'failed', 'unknown_outcome', 'dispatched', 'dead')) OR
       (OLD.state = 'failed'          AND NEW.state IN ('running', 'dispatched', 'dead', 'unknown_outcome')) OR
       (OLD.state = 'unknown_outcome' AND NEW.state IN ('dispatched', 'done', 'dead')) OR
       (OLD.state = 'dead'            AND NEW.state IN ('pending'))) THEN
    RAISE EXCEPTION 'Outbox row % cannot go from % to %', OLD.id, OLD.state, NEW.state
      USING ERRCODE = 'FY071', HINT = 'pending → dispatched → running → done | failed | unknown_outcome | dead (§12.11).';
  END IF;
  IF NEW.attempts < OLD.attempts OR NEW.dispatches < OLD.dispatches THEN
    RAISE EXCEPTION 'Outbox row % cannot count attempts or dispatches backwards', OLD.id USING ERRCODE = 'FY070';
  END IF;
  NEW.updated_at := now();
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER outbox_guard BEFORE UPDATE ON fyniq.outbox
  FOR EACH ROW EXECUTE FUNCTION fyniq.outbox_guard();

-- +goose StatementBegin
CREATE FUNCTION fyniq.outbox_no_delete() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'Outbox rows are kept (% on %)', TG_OP, TG_TABLE_NAME USING ERRCODE = 'FY070';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER outbox_no_delete BEFORE DELETE ON fyniq.outbox
  FOR EACH ROW EXECUTE FUNCTION fyniq.outbox_no_delete();
CREATE TRIGGER outbox_no_truncate BEFORE TRUNCATE ON fyniq.outbox
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.outbox_no_delete();

-- ------------------------------------------------------------------ row-level security
ALTER TABLE fyniq.outbox ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.outbox FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.outbox
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

-- ------------------------------------------------------------------ grants (db/README.md)
-- Commands write rows; the API dispatches them; handlers lease and finish them.
GRANT SELECT ON fyniq.outbox TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
GRANT INSERT (worker_class, kind, payload, business_key, max_attempts)
  ON fyniq.outbox TO fyniq_api, fyniq_worker, fyniq_executor;
GRANT UPDATE (state, attempts, dispatches, task_name, job_execution, lease_holder, lease_expires_at,
              deadline_at, last_error)
  ON fyniq.outbox TO fyniq_api, fyniq_worker, fyniq_executor;
