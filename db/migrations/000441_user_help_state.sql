-- A person's help state: how far they went with the first-run tour (PRD-001 FR-UX-34, QA-UI-06, G4-104; the data
-- model's user_help_state, rev 0.3 and rev 0.8; p1-app-shell task 4.2). The tour changes no business data: its only
-- write is this, the person's own progress, kept per organisation since each one's interface differs by role. Each
-- change is a new row, audited; the latest is in force, so the history of offers, skips and finishes stays.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.user_help_state (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  user_id         uuid        NOT NULL,
  tour            text        NOT NULL,  -- started (at step), finished or skipped (at step)
  step            smallint    NOT NULL,  -- the step shown last, from 0; a tour has at most seven
  set_at          timestamptz NOT NULL,
  CONSTRAINT user_help_state_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT user_help_state_member_fkey FOREIGN KEY (organisation_id, user_id)
    REFERENCES fyniq.user_membership (organisation_id, user_id),
  CONSTRAINT user_help_state_tour_known CHECK (tour IN ('started', 'finished', 'skipped')),
  CONSTRAINT user_help_state_step_range CHECK (step BETWEEN 0 AND 6)
);
CREATE INDEX user_help_state_latest ON fyniq.user_help_state (organisation_id, user_id, set_at);

-- Append-only, as notification preferences are (FY189): a new row is the new state.
CREATE TRIGGER user_help_state_guard BEFORE UPDATE OR DELETE ON fyniq.user_help_state
  FOR EACH ROW EXECUTE FUNCTION fyniq.notification_append_only();
CREATE TRIGGER user_help_state_no_truncate BEFORE TRUNCATE ON fyniq.user_help_state
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.user_help_state
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('user_help_state', 'id');

ALTER TABLE fyniq.user_help_state ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.user_help_state FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.user_help_state
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

-- A person sets their own through the API, which saves only the caller's; the worker reads it for the tenant export.
GRANT SELECT ON fyniq.user_help_state TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (user_id, tour, step, set_at) ON fyniq.user_help_state TO fyniq_api;
