-- Long operations (PRD-001 FR-INT-11; p1-public-api task 2.1, design "Operations for long work go through the outbox"):
-- a request whose work runs in the background answers 202 with an operation the caller polls at
-- GET /v1/operations/{id}. An operation names the job that does the work, the resource the work makes or changes, and
-- who asked; its status is its job's, read from the outbox, so the worker records nothing more. An operation never
-- changes or goes (FY284).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.operation (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  kind            text        NOT NULL,
  job_id          uuid        NOT NULL,
  resource        text        NOT NULL,
  requested_by    text        NOT NULL,
  requested_at    timestamptz NOT NULL,
  CONSTRAINT operation_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT operation_job_fkey FOREIGN KEY (organisation_id, job_id) REFERENCES fyniq.outbox (organisation_id, id),
  CONSTRAINT operation_kind_known CHECK (kind IN ('report_export', 'checks_run')),
  CONSTRAINT operation_resource_path CHECK (resource ~ '^/v1/[a-z0-9/_-]{1,200}$'),
  CONSTRAINT operation_requested_by_format CHECK (requested_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);
CREATE INDEX operation_job ON fyniq.operation (organisation_id, job_id);

ALTER TABLE fyniq.operation ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.operation FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.operation
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.operation
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('operation', 'id');

-- +goose StatementBegin
CREATE FUNCTION fyniq.operation_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'An operation is kept as it was asked for' USING ERRCODE = 'FY284',
    HINT = 'Its status is its job''s, read from the outbox.';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER operation_guard BEFORE UPDATE OR DELETE ON fyniq.operation
  FOR EACH ROW EXECUTE FUNCTION fyniq.operation_guard();
CREATE TRIGGER operation_no_truncate BEFORE TRUNCATE ON fyniq.operation
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.operation_guard();

GRANT SELECT, INSERT ON fyniq.operation TO fyniq_api;
GRANT SELECT ON fyniq.operation TO fyniq_worker, fyniq_executor, fyniq_readonly;
