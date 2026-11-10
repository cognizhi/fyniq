-- Repeated authorisation refusals (PRD-001 SEC-31; p1-security-hardening task 8.1, design D7): the API counts each
-- refusal by the policy layer (API-AUTHZ-01) of a person in a ten-minute window, and raises an alert once the window
-- holds more than the threshold (20 by default). Counts only: never a request, its body or its operation's data. Like
-- the rate-limit windows (000017) they are a counter, not a record: no audit event, and the worker deletes them once
-- they have ended (the ratelimit.purge job).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.authz_refusal (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  user_id         uuid        NOT NULL,
  window_start    timestamptz NOT NULL,
  refused         integer     NOT NULL DEFAULT 1,
  CONSTRAINT authz_refusal_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT authz_refusal_user_key UNIQUE (organisation_id, user_id, window_start),
  CONSTRAINT authz_refusal_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT authz_refusal_refused_positive CHECK (refused > 0)
);
CREATE INDEX authz_refusal_start_idx ON fyniq.authz_refusal (organisation_id, window_start);

ALTER TABLE fyniq.authz_refusal ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.authz_refusal FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.authz_refusal
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

-- The API adds to a window (an upsert that returns the count); the worker deletes the windows that ended.
GRANT SELECT ON fyniq.authz_refusal TO fyniq_api, fyniq_readonly;
GRANT INSERT (user_id, window_start), UPDATE (refused) ON fyniq.authz_refusal TO fyniq_api;
GRANT SELECT (organisation_id, id, window_start), DELETE ON fyniq.authz_refusal TO fyniq_worker;
