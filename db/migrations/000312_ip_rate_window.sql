-- Per-IP limits on the routes that take no session (PRD-001 SEC-23; p1-security-hardening task 6.2, design D4): sign-in,
-- joining and recovery, the public documents and the setup code, before anyone is known to belong to an organisation.
-- One window a minute per client address and kind, kept as FR-INT-16's windows are. The address itself is never kept:
-- the API sends a SHA-256 of it, and the jobs purge a window minutes after it ends. Global: no application role holds a
-- grant on the table; the API counts through count_ip_request, the jobs purge through purge_ip_rate_windows.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.ip_rate_window (
  client       text        NOT NULL,
  kind         text        NOT NULL,
  window_start timestamptz NOT NULL,
  used         integer     NOT NULL DEFAULT 1,
  CONSTRAINT ip_rate_window_pkey PRIMARY KEY (client, kind, window_start),
  CONSTRAINT ip_rate_window_client_hash CHECK (client ~ '^[0-9a-f]{64}$'),
  CONSTRAINT ip_rate_window_kind_known CHECK (kind IN ('signin', 'join', 'public', 'setup')),
  CONSTRAINT ip_rate_window_used_positive CHECK (used >= 1)
);
CREATE INDEX ip_rate_window_start ON fyniq.ip_rate_window (window_start);
REVOKE ALL ON fyniq.ip_rate_window FROM PUBLIC;

-- +goose StatementBegin
-- Adds one request of kind from client (a SHA-256 of its address) to the window starting at p_window, and returns how
-- many the window now holds.
CREATE FUNCTION fyniq.count_ip_request(p_client text, p_kind text, p_window timestamptz) RETURNS integer
LANGUAGE sql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  INSERT INTO fyniq.ip_rate_window AS w (client, kind, window_start) VALUES (p_client, p_kind, p_window)
  ON CONFLICT (client, kind, window_start) DO UPDATE SET used = w.used + 1
  RETURNING w.used
$$;

-- Deletes the windows that started before p_before, and returns how many.
CREATE FUNCTION fyniq.purge_ip_rate_windows(p_before timestamptz) RETURNS bigint
LANGUAGE sql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  WITH gone AS (DELETE FROM fyniq.ip_rate_window w WHERE w.window_start < p_before RETURNING 1)
  SELECT count(*) FROM gone
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION fyniq.count_ip_request(text, text, timestamptz), fyniq.purge_ip_rate_windows(timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.count_ip_request(text, text, timestamptz) TO fyniq_api;
GRANT EXECUTE ON FUNCTION fyniq.purge_ip_rate_windows(timestamptz) TO fyniq_worker;
