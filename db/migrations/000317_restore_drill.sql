-- The restore drill register (PRD-001 DR-06, NFR-DR-01, NFR-DR-02, NFR-DR-03, CTL-RES-02 Q; p1-operations-dr task 4.3,
-- design D2): each full drill a person runs by docs/runbooks/restore-drill.md, with its scenario, what it restored from,
-- the data it lost and how long it took, and DR-06's checks on the restored copy: the ledger's and the audit chain's
-- verifications, documents, keys, sign-in, the Scheduler tick and the outbox's reconciliation. A drill counts as recovered
-- only when every check passed. Drills are only ever added (FY323), each audited (AUD-01), under the "Compliance Center"
-- row of Appendix D.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.restore_drill (
  organisation_id   uuid        NOT NULL DEFAULT fyniq.current_org(),
  id                uuid        NOT NULL DEFAULT uuidv7(),
  held_on           date        NOT NULL,
  scenario          text        NOT NULL,  -- project_loss, corruption or zone_failure (NFR-DR-01, NFR-DR-02)
  restored_from     text        NOT NULL,  -- the weekly export or the point in time the copy was made from
  data_lost_minutes integer     NOT NULL,  -- measured against NFR-DR-01
  took_minutes      integer     NOT NULL,  -- measured against NFR-DR-02
  ledger            boolean     NOT NULL,
  audit_chain       boolean     NOT NULL,
  documents         boolean     NOT NULL,
  keys              boolean     NOT NULL,
  sign_in           boolean     NOT NULL,
  tick              boolean     NOT NULL,
  outbox            boolean     NOT NULL,
  findings          text        NOT NULL,
  recorded_by       text        NOT NULL,
  recorded_at       timestamptz NOT NULL,
  CONSTRAINT restore_drill_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT restore_drill_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT restore_drill_scenario_known CHECK (scenario IN ('project_loss', 'corruption', 'zone_failure')),
  CONSTRAINT restore_drill_texts CHECK (length(btrim(restored_from)) BETWEEN 1 AND 500 AND length(btrim(findings)) BETWEEN 1 AND 8000),
  CONSTRAINT restore_drill_minutes CHECK (data_lost_minutes >= 0 AND took_minutes > 0),
  CONSTRAINT restore_drill_by_format CHECK (recorded_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);

-- Nothing changes or goes.
-- +goose StatementBegin
CREATE FUNCTION fyniq.restore_drill_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'Restore drills never change or go' USING ERRCODE = 'FY323',
    HINT = 'Record the next drill (DR-06).';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER restore_drill_guard BEFORE UPDATE OR DELETE ON fyniq.restore_drill
  FOR EACH ROW EXECUTE FUNCTION fyniq.restore_drill_guard();
CREATE TRIGGER restore_drill_no_truncate BEFORE TRUNCATE ON fyniq.restore_drill
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.restore_drill
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('restore_drill', 'id');

ALTER TABLE fyniq.restore_drill ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.restore_drill FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.restore_drill
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

GRANT INSERT (held_on, scenario, restored_from, data_lost_minutes, took_minutes, ledger, audit_chain, documents, keys, sign_in, tick,
  outbox, findings, recorded_by, recorded_at) ON fyniq.restore_drill TO fyniq_api;
GRANT SELECT ON fyniq.restore_drill TO fyniq_api, fyniq_worker, fyniq_readonly;

