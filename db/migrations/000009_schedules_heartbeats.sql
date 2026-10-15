-- Platform schedules and heartbeats (PRD-001 §12.11, ADR-T7).
--
-- The Scheduler tick (every 5 minutes) runs the platform's own jobs, such as audit anchoring
-- (hourly) and nightly verification, for every organisation. job_slot records each run by job and
-- slot, so a slot runs at most once, two runs of a job never overlap (a lease), and after a gap only
-- the latest missed slot runs while the others are recorded as skipped. heartbeat records each
-- successful tick, anchoring and verification, so an absence alert outside the request path can page
-- when they stop (NFR-OBS-02). Both are global tables: they belong to the installation, not to an
-- organisation, and services change them only through the functions below. Nothing here relies on the
-- database's own scheduler (pg_cron): the tick drives it all.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.job_slot (
  job              text        NOT NULL,
  slot             timestamptz NOT NULL,
  state            text        NOT NULL,
  holder           text,
  execution        text,
  lease_expires_at timestamptz,
  claimed_at       timestamptz NOT NULL,
  finished_at      timestamptz,
  detail           text,
  CONSTRAINT job_slot_pkey PRIMARY KEY (job, slot),
  CONSTRAINT job_slot_job_format CHECK (job ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$' AND length(job) <= 100),
  CONSTRAINT job_slot_state_known CHECK (state IN ('running', 'done', 'failed', 'skipped')),
  CONSTRAINT job_slot_lease_whole CHECK ((state = 'running') = (lease_expires_at IS NOT NULL)),
  CONSTRAINT job_slot_detail_length CHECK (length(detail) <= 2000)
);

CREATE TABLE fyniq.heartbeat (
  name        text        NOT NULL,
  last_beat   timestamptz NOT NULL,
  detail      text,
  CONSTRAINT heartbeat_pkey PRIMARY KEY (name),
  CONSTRAINT heartbeat_name_format CHECK (name ~ '^[a-z][a-z0-9_.]*$' AND length(name) <= 100),
  CONSTRAINT heartbeat_detail_length CHECK (length(detail) <= 2000)
);

CREATE VIEW fyniq.job_slots AS
  SELECT job, slot, state, holder, execution, lease_expires_at, claimed_at, finished_at, detail FROM fyniq.job_slot;
CREATE VIEW fyniq.heartbeats AS SELECT name, last_beat, detail FROM fyniq.heartbeat;

-- Claim a job's slot. It fails (false) when the slot already ran or was skipped, and when another run
-- of the job still holds an unexpired lease (no overlap). An expired run is marked failed first.
-- +goose StatementBegin
CREATE FUNCTION fyniq.claim_job_slot(p_job text, p_slot timestamptz, p_holder text, p_until timestamptz, p_now timestamptz)
RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  PERFORM pg_advisory_xact_lock(hashtextextended('fyniq.job_slot:' || p_job, 0));
  UPDATE fyniq.job_slot SET state = 'failed', lease_expires_at = NULL, finished_at = p_now,
         detail = 'the run overran its lease and was given up by the watchdog'
   WHERE job = p_job AND state = 'running' AND lease_expires_at < p_now;
  IF EXISTS (SELECT 1 FROM fyniq.job_slot WHERE job = p_job AND state = 'running') THEN
    RETURN false;
  END IF;
  INSERT INTO fyniq.job_slot (job, slot, state, holder, lease_expires_at, claimed_at)
  VALUES (p_job, p_slot, 'running', p_holder, p_until, p_now)
  ON CONFLICT (job, slot) DO NOTHING;
  RETURN FOUND;
END
$$;
-- +goose StatementEnd

-- Record the slots a gap made the job miss; they never run (only the latest missed slot does).
-- +goose StatementBegin
CREATE FUNCTION fyniq.skip_job_slots(p_job text, p_slots timestamptz[], p_now timestamptz) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  n integer;
BEGIN
  INSERT INTO fyniq.job_slot (job, slot, state, claimed_at, finished_at, detail)
  SELECT p_job, s, 'skipped', p_now, p_now, 'missed while the tick was not running; only the latest missed slot runs'
    FROM unnest(p_slots) AS s
  ON CONFLICT (job, slot) DO NOTHING;
  GET DIAGNOSTICS n = ROW_COUNT;
  RETURN n;
END
$$;
-- +goose StatementEnd

-- Record what a run started (its job execution) or how it ended.
-- +goose StatementBegin
CREATE FUNCTION fyniq.started_job_slot(p_job text, p_slot timestamptz, p_execution text) RETURNS void
LANGUAGE sql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  UPDATE fyniq.job_slot SET execution = p_execution WHERE job = p_job AND slot = p_slot AND state = 'running'
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION fyniq.finish_job_slot(p_job text, p_slot timestamptz, p_ok boolean, p_detail text, p_now timestamptz)
RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  UPDATE fyniq.job_slot
     SET state = CASE WHEN p_ok THEN 'done' ELSE 'failed' END, lease_expires_at = NULL, finished_at = p_now,
         detail = left(p_detail, 2000)
   WHERE job = p_job AND slot = p_slot AND state = 'running';
  RETURN FOUND;
END
$$;
-- +goose StatementEnd

-- The latest slot a job claimed or skipped, or NULL.
-- +goose StatementBegin
CREATE FUNCTION fyniq.latest_job_slot(p_job text) RETURNS timestamptz
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT max(slot) FROM fyniq.job_slot WHERE job = p_job
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION fyniq.beat(p_name text, p_at timestamptz, p_detail text) RETURNS void
LANGUAGE sql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  INSERT INTO fyniq.heartbeat (name, last_beat, detail) VALUES (p_name, p_at, left(p_detail, 2000))
  ON CONFLICT (name) DO UPDATE SET last_beat = greatest(heartbeat.last_beat, excluded.last_beat), detail = excluded.detail
$$;
-- +goose StatementEnd

-- ------------------------------------------------------------------ grants (db/README.md)
-- The tick runs in the API, and the platform's jobs as the worker; both read the directory of
-- organisations to sweep and run for each.
GRANT SELECT ON fyniq.tenants TO fyniq_api;
GRANT SELECT ON fyniq.job_slots, fyniq.heartbeats TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT EXECUTE ON FUNCTION fyniq.claim_job_slot(text, timestamptz, text, timestamptz, timestamptz),
                          fyniq.skip_job_slots(text, timestamptz[], timestamptz),
                          fyniq.started_job_slot(text, timestamptz, text),
                          fyniq.latest_job_slot(text)
  TO fyniq_api;
GRANT EXECUTE ON FUNCTION fyniq.finish_job_slot(text, timestamptz, boolean, text, timestamptz),
                          fyniq.beat(text, timestamptz, text)
  TO fyniq_api, fyniq_worker;
