-- Security events' request details beside the chain (PRD-001 AUD-06, the owner's decision of rev 0.230;
-- p1-audit-explorer slice 2, design D2).
--
-- A security event (a sign-in or sign-out, a sign-in method or passkey change, a recovery, a reveal of a full
-- identifier, an export) keeps the IP address and user agent it came from here, not in its body. Its body carries their
-- keyed fingerprint (request.detail), so the chain still proves them while they exist. After the security-log retention
-- (`retention.security_events_days`, 400 days by default and at least 30) the erasure job deletes them; the event stays
-- and verifies.
--
-- A row is inserted with its event, in the same transaction, never changed, and deleted only through
-- audit_erase_details, by the worker, and only once it is older than the shortest retention the setting allows (30
-- days, by the database's own clock). No role holds DELETE or UPDATE.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.audit_request_detail (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL,  -- the event's id: one row per security event
  occurred_at     timestamptz NOT NULL,  -- the event's occurred_at, by the application's clock
  ip              text        NOT NULL,
  user_agent      text        NOT NULL,
  CONSTRAINT audit_request_detail_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT audit_request_detail_event_fkey FOREIGN KEY (organisation_id, id)
    REFERENCES fyniq.audit_event (organisation_id, id),
  CONSTRAINT audit_request_detail_ip_length CHECK (length(ip) <= 64),
  CONSTRAINT audit_request_detail_agent_length CHECK (length(user_agent) <= 1000),
  CONSTRAINT audit_request_detail_something CHECK (ip <> '' OR user_agent <> '')
);
CREATE INDEX audit_request_detail_age_idx ON fyniq.audit_request_detail (organisation_id, occurred_at);

-- Never changed; deleted only by audit_erase_details, and never while younger than 30 days.
-- +goose StatementBegin
CREATE FUNCTION fyniq.audit_request_detail_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'UPDATE' THEN
    RAISE EXCEPTION 'A security event''s request details never change' USING ERRCODE = 'FY303',
      HINT = 'They are erased whole after the security-log retention (AUD-06).';
  END IF;
  IF current_setting('fyniq.audit_erasing', true) IS DISTINCT FROM 'on' OR OLD.occurred_at > now() - interval '30 days' THEN
    RAISE EXCEPTION 'A security event''s request details are erased only by the erasure job, after the security-log retention'
      USING ERRCODE = 'FY303', HINT = 'They are kept at least 30 days, and as long as retention.security_events_days says (AUD-06).';
  END IF;
  RETURN OLD;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER audit_request_detail_guard BEFORE UPDATE OR DELETE ON fyniq.audit_request_detail
  FOR EACH ROW EXECUTE FUNCTION fyniq.audit_request_detail_guard();
CREATE TRIGGER audit_request_detail_no_truncate BEFORE TRUNCATE ON fyniq.audit_request_detail
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();

-- The erasure: the transaction's organisation's details older than p_cutoff, which must be at least 30 days ago by the
-- database's clock. It answers how many went and the first and last sequence numbers of their events.
-- +goose StatementBegin
CREATE FUNCTION fyniq.audit_erase_details(p_cutoff timestamptz, OUT erased bigint, OUT first_seq bigint, OUT last_seq bigint)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  org uuid := fyniq.require_org();
BEGIN
  IF p_cutoff IS NULL OR p_cutoff > now() - interval '30 days' THEN
    RAISE EXCEPTION 'Security events'' request details are kept at least 30 days: % is too recent', p_cutoff
      USING ERRCODE = 'FY303', HINT = 'Erase only before the security-log retention (retention.security_events_days, AUD-06).';
  END IF;
  PERFORM set_config('fyniq.audit_erasing', 'on', true);
  WITH gone AS (
    DELETE FROM fyniq.audit_request_detail d WHERE d.organisation_id = org AND d.occurred_at < p_cutoff RETURNING d.id
  )
  SELECT count(*), min(e.seq), max(e.seq) INTO erased, first_seq, last_seq
    FROM gone JOIN fyniq.audit_event e ON e.organisation_id = org AND e.id = gone.id;
  PERFORM set_config('fyniq.audit_erasing', 'off', true);
END
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION fyniq.audit_erase_details(timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.audit_erase_details(timestamptz) TO fyniq_worker;

ALTER TABLE fyniq.audit_request_detail ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.audit_request_detail FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.audit_request_detail
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

-- Services write a security event's details with it; the worker and the read-only role read them; nobody updates them.
GRANT INSERT (id, occurred_at, ip, user_agent) ON fyniq.audit_request_detail TO fyniq_api, fyniq_worker, fyniq_executor;
GRANT SELECT ON fyniq.audit_request_detail TO fyniq_worker, fyniq_readonly;
