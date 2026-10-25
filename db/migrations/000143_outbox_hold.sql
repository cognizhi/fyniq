-- A held outbox row (PRD-001 FR-INT-20, FR-INT-26; p1-connector-framework task 5.1): work that cannot start because every
-- connector it needs is paused or disabled waits without spending its attempts. The handler that leased the row puts it
-- back as dispatched with a later deadline, so the sweep delivers it again then, and raises its max_attempts by the one
-- the lease took: attempts still never count backwards (FY070). A row held so often that max_attempts reaches the
-- table's 100 keeps its last budget, so a very long pause can still end in a dead row (db/README.md).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- +goose StatementBegin
CREATE FUNCTION fyniq.hold_outbox(p_id uuid, p_holder text, p_until timestamptz, p_why text) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  UPDATE fyniq.outbox SET state = 'dispatched', lease_holder = NULL, lease_expires_at = NULL, deadline_at = p_until,
    last_error = p_why, max_attempts = least(max_attempts + 1, 100)
   WHERE id = p_id AND state = 'running' AND lease_holder = p_holder AND organisation_id = fyniq.current_org();
  IF NOT FOUND THEN
    RAISE EXCEPTION 'Outbox row % is not running under %', p_id, p_holder USING ERRCODE = 'FY071';
  END IF;
END
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION fyniq.hold_outbox(uuid, text, timestamptz, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.hold_outbox(uuid, text, timestamptz, text) TO fyniq_api, fyniq_worker, fyniq_executor;
