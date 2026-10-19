-- Rate limits per client and per organisation (PRD-001 FR-INT-16, FR-INT-02; p1-public-api task 2.3).
--
-- The API counts each authenticated request in a one-minute window for its client (the OAuth client and
-- subject, as the idempotency keys name them), separately for reads and writes, and refuses with 429 once
-- the window holds more than the limit (600 reads and 120 writes by default). Counting in the database
-- keeps every instance of the API to the same count with no other store running (ADR-T7: scale to zero).
-- The windows are a counter, not a record of the organisation's data: they carry no audit event, like the
-- idempotency keys, and the worker deletes them once they have ended (the ratelimit.purge job). Both times
-- come from the API's clock (DEV-05).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.rate_limit_window (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  client_id       text        NOT NULL,
  kind            text        NOT NULL,
  window_start    timestamptz NOT NULL,
  used            integer     NOT NULL DEFAULT 1,
  CONSTRAINT rate_limit_window_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT rate_limit_window_client_key UNIQUE (organisation_id, client_id, kind, window_start),
  CONSTRAINT rate_limit_window_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT rate_limit_window_client_length CHECK (length(client_id) BETWEEN 1 AND 200),
  CONSTRAINT rate_limit_window_kind_known CHECK (kind IN ('read', 'write')),
  CONSTRAINT rate_limit_window_used_positive CHECK (used > 0)
);
CREATE INDEX rate_limit_window_start_idx ON fyniq.rate_limit_window (organisation_id, window_start);

ALTER TABLE fyniq.rate_limit_window ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.rate_limit_window FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.rate_limit_window
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

-- The API adds to a window (an upsert that returns the count); the worker deletes the windows that ended.
GRANT SELECT ON fyniq.rate_limit_window TO fyniq_api, fyniq_readonly;
GRANT INSERT (client_id, kind, window_start), UPDATE (used) ON fyniq.rate_limit_window TO fyniq_api;
GRANT SELECT (organisation_id, id, window_start), DELETE ON fyniq.rate_limit_window TO fyniq_worker;
