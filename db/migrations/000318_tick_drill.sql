-- The heartbeat drill register, fyniq.tick_drill (PRD-001 NFR-OBS-02, CTL-RES-05 Q; p1-operations-dr task 7.2, design D6): each drill in
-- which the tick was paused in staging, by docs/runbooks/heartbeat-drill.md, with how long it was paused and how many
-- minutes after the pause the first alert and the escalation came, if they did. It passes when the platform owner was
-- told within 30 minutes and the Owners within 60. Drills are only ever added (FY324), each audited (AUD-01), under the
-- "Compliance Center" row of Appendix D.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.tick_drill (
  organisation_id      uuid        NOT NULL DEFAULT fyniq.current_org(),
  id                   uuid        NOT NULL DEFAULT uuidv7(),
  held_on              date        NOT NULL,
  environment          text        NOT NULL,  -- where the tick was paused: staging
  paused_minutes       integer     NOT NULL,
  first_alert_minutes  integer,               -- after the pause; NULL: no alert came
  escalation_minutes   integer,               -- after the pause; NULL: no escalation came
  findings             text        NOT NULL,
  recorded_by          text        NOT NULL,
  recorded_at          timestamptz NOT NULL,
  CONSTRAINT tick_drill_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT tick_drill_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT tick_drill_environment_known CHECK (environment IN ('staging', 'production')),
  CONSTRAINT tick_drill_minutes CHECK (paused_minutes > 0 AND (first_alert_minutes IS NULL OR first_alert_minutes >= 0)
    AND (escalation_minutes IS NULL OR escalation_minutes >= 0)),
  CONSTRAINT tick_drill_findings CHECK (length(btrim(findings)) BETWEEN 1 AND 8000),
  CONSTRAINT tick_drill_by_format CHECK (recorded_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);

-- Nothing changes or goes.
-- +goose StatementBegin
CREATE FUNCTION fyniq.tick_drill_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'Heartbeat drills never change or go' USING ERRCODE = 'FY324',
    HINT = 'Record the next drill (NFR-OBS-02).';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER tick_drill_guard BEFORE UPDATE OR DELETE ON fyniq.tick_drill
  FOR EACH ROW EXECUTE FUNCTION fyniq.tick_drill_guard();
CREATE TRIGGER tick_drill_no_truncate BEFORE TRUNCATE ON fyniq.tick_drill
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.tick_drill
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('tick_drill', 'id');

ALTER TABLE fyniq.tick_drill ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.tick_drill FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.tick_drill
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

GRANT INSERT (held_on, environment, paused_minutes, first_alert_minutes, escalation_minutes, findings, recorded_by, recorded_at)
  ON fyniq.tick_drill TO fyniq_api;
GRANT SELECT ON fyniq.tick_drill TO fyniq_api, fyniq_worker, fyniq_readonly;
