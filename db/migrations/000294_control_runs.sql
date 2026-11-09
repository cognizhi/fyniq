-- The Compliance Center's verifications (PRD-001 CMP-01; CTL-FIN-08, CTL-IAM-01, CTL-AUD-05; p1-compliance-center
-- slice 4, design D4).
--
-- Each night compliance.run makes, for each organisation, the verifications its control registry names (`verify
-- <name>`): payments' dual control, members' second factor, and exports and reveals reconciled with the trail. Each run
-- is recorded here, by the worker, with what it found: the evidence the Compliance Center shows for the check. A run is
-- never changed or deleted; it is the record of what was verified (FY307).
--
-- user_session and webauthn_credential are installation tables no role reads. members_second_factor answers, for the
-- organisation the transaction is set to and for the worker only, which active members have a passkey, which have
-- signed in with a second factor, and which were seen in the window in a session that passed none (SEC-01).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.control_run (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  control         text        NOT NULL,  -- the control of the registry, such as CTL-FIN-08
  frequency       text        NOT NULL,
  check_how       text        NOT NULL,  -- the check, as the registry names it: verify <name>
  since           timestamptz NOT NULL,  -- the window it read: from since to ran_at
  ran_at          timestamptz NOT NULL,
  ok              boolean     NOT NULL,
  item_count      integer     NOT NULL,
  items           jsonb       NOT NULL DEFAULT '[]',  -- the first 50 of what it found
  detail          text        NOT NULL,
  CONSTRAINT control_run_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT control_run_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT control_run_control_format CHECK (control ~ '^CTL-[A-Z]+-[0-9]{2}$'),
  CONSTRAINT control_run_frequency_known CHECK (frequency IN ('CI', 'D', 'N', 'W', 'M', 'Q', 'S', 'E')),
  CONSTRAINT control_run_check_format CHECK (check_how ~ '^verify [a-z0-9.-]{1,60}$'),
  CONSTRAINT control_run_window CHECK (since < ran_at),
  CONSTRAINT control_run_found CHECK (item_count >= 0 AND ok = (item_count = 0)),
  CONSTRAINT control_run_items CHECK (jsonb_typeof(items) = 'array' AND jsonb_array_length(items) <= LEAST(item_count, 50)),
  CONSTRAINT control_run_detail_length CHECK (length(btrim(detail)) BETWEEN 1 AND 2000)
);
CREATE INDEX control_run_latest_idx ON fyniq.control_run (organisation_id, check_how, ran_at);

-- A run is the record of what was verified: never changed or deleted.
-- +goose StatementBegin
CREATE FUNCTION fyniq.control_run_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'A verification''s run never changes or goes' USING ERRCODE = 'FY307',
    HINT = 'The next run records what it finds then (CMP-01).';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER control_run_guard BEFORE UPDATE OR DELETE ON fyniq.control_run
  FOR EACH ROW EXECUTE FUNCTION fyniq.control_run_guard();
CREATE TRIGGER control_run_no_truncate BEFORE TRUNCATE ON fyniq.control_run
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();

ALTER TABLE fyniq.control_run ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.control_run FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.control_run
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

GRANT INSERT (control, frequency, check_how, since, ran_at, ok, item_count, items, detail) ON fyniq.control_run TO fyniq_worker;
GRANT SELECT ON fyniq.control_run TO fyniq_api, fyniq_worker, fyniq_readonly;

-- The active members of the transaction's organisation and their second factors (CTL-IAM-01, SEC-01): how many passkeys
-- each has, how many of their sign-ins passed a second factor, and how many of their sessions seen since `since` passed
-- none, with the latest such sighting. Nothing else of a session or a passkey leaves the function.
-- +goose StatementBegin
CREATE FUNCTION fyniq.members_second_factor(since timestamptz)
RETURNS TABLE (user_id uuid, passkeys integer, sessions_with integer, sessions_without integer, last_without timestamptz)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT m.user_id,
         (SELECT count(*)::integer FROM fyniq.webauthn_credential w WHERE w.user_id = m.user_id),
         (SELECT count(*)::integer FROM fyniq.user_session s WHERE s.user_id = m.user_id AND s.step_up_at IS NOT NULL),
         (SELECT count(*)::integer FROM fyniq.user_session s
           WHERE s.user_id = m.user_id AND s.step_up_at IS NULL AND s.last_seen_at >= since),
         (SELECT max(s.last_seen_at) FROM fyniq.user_session s
           WHERE s.user_id = m.user_id AND s.step_up_at IS NULL AND s.last_seen_at >= since)
    FROM fyniq.user_membership m
   WHERE m.organisation_id = fyniq.current_org() AND m.status = 'active'
   ORDER BY m.user_id
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION fyniq.members_second_factor(timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.members_second_factor(timestamptz) TO fyniq_worker;

