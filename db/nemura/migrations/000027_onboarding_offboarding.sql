-- Joiners' tasks and leavers' dates (PRD-002 NM-ONB-02, NM-ONB-03; p1-nemura-self-service-letters slice 4).
--
-- A company that uses Fyniq switches on "invite new people to Fyniq": adding a person then raises one task, for someone with the right to invite, that is done or dismissed
-- (the link will close it when the invitation is made). An offboarding keeps the dates that start statutory clocks apart: when notice was given, the last day and, for a
-- retrenchment, when employees were told; with the checklist (equipment, exit interview, removal of Fyniq access). Final pay and IR21 belong to payroll.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE nemura.onboarding_settings (
  organisation_id  uuid        NOT NULL DEFAULT nemura.current_org(),
  invite_to_fyniq  boolean     NOT NULL DEFAULT false,
  updated_by       uuid        NOT NULL,
  updated_at       timestamptz NOT NULL,
  CONSTRAINT onboarding_settings_pkey PRIMARY KEY (organisation_id),
  CONSTRAINT onboarding_settings_by_fkey FOREIGN KEY (organisation_id, updated_by) REFERENCES nemura.member (organisation_id, id)
);

CREATE TABLE nemura.join_task (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  person_id       uuid        NOT NULL,
  kind            text        NOT NULL DEFAULT 'fyniq_invitation',
  state           text        NOT NULL DEFAULT 'open',
  raised_at       timestamptz NOT NULL,
  decided_by      uuid,
  decided_at      timestamptz,
  note            text,
  CONSTRAINT join_task_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT join_task_person_fkey FOREIGN KEY (organisation_id, person_id) REFERENCES nemura.person (organisation_id, id),
  CONSTRAINT join_task_decided_by_fkey FOREIGN KEY (organisation_id, decided_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT join_task_kind_known CHECK (kind IN ('fyniq_invitation')),
  CONSTRAINT join_task_state_known CHECK (state IN ('open', 'done', 'dismissed')),
  CONSTRAINT join_task_decided_whole CHECK ((state = 'open') = (decided_at IS NULL AND decided_by IS NULL)),
  CONSTRAINT join_task_note_length CHECK (note IS NULL OR length(note) <= 300),
  CONSTRAINT join_task_once UNIQUE (organisation_id, person_id, kind)
);
CREATE INDEX join_task_open_idx ON nemura.join_task (organisation_id, raised_at) WHERE state = 'open';

-- +goose StatementBegin
CREATE FUNCTION nemura.join_task_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR NEW.person_id IS DISTINCT FROM OLD.person_id OR NEW.kind IS DISTINCT FROM OLD.kind OR NEW.raised_at IS DISTINCT FROM OLD.raised_at
     OR OLD.state <> 'open' OR NEW.state = 'open' THEN
    RAISE EXCEPTION 'A task is done or dismissed once' USING ERRCODE = 'NM170';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER join_task_guard BEFORE UPDATE OR DELETE ON nemura.join_task FOR EACH ROW EXECUTE FUNCTION nemura.join_task_guard();

CREATE TABLE nemura.offboarding (
  organisation_id          uuid        NOT NULL DEFAULT nemura.current_org(),
  id                       uuid        NOT NULL DEFAULT uuidv7(),
  person_id                uuid        NOT NULL,
  kind                     text        NOT NULL,
  notice_given_on          date,
  last_day                 date        NOT NULL,
  retrenchment_notified_on date,
  equipment_returned_on    date,
  exit_interview_on        date,
  fyniq_access_removed_on  date,
  state                    text        NOT NULL DEFAULT 'active',
  created_by               uuid        NOT NULL,
  created_at               timestamptz NOT NULL,
  updated_at               timestamptz NOT NULL,
  CONSTRAINT offboarding_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT offboarding_person_fkey FOREIGN KEY (organisation_id, person_id) REFERENCES nemura.person (organisation_id, id),
  CONSTRAINT offboarding_by_fkey FOREIGN KEY (organisation_id, created_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT offboarding_kind_known CHECK (kind IN ('resignation', 'termination', 'retrenchment', 'end_of_contract', 'retirement', 'other')),
  CONSTRAINT offboarding_state_known CHECK (state IN ('active', 'cancelled')),
  -- the date of telling employees belongs to a retrenchment and to nothing else
  CONSTRAINT offboarding_retrenchment_date CHECK ((kind = 'retrenchment') OR retrenchment_notified_on IS NULL),
  CONSTRAINT offboarding_notice_before_last_day CHECK (notice_given_on IS NULL OR notice_given_on <= last_day)
);
CREATE UNIQUE INDEX offboarding_one_active ON nemura.offboarding (organisation_id, person_id) WHERE state = 'active';

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.onboarding_settings DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('onboarding_settings', 'organisation_id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.join_task DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('join_task', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.offboarding DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('offboarding', 'id');

ALTER TABLE nemura.onboarding_settings ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.onboarding_settings FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.onboarding_settings USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.join_task ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.join_task FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.join_task USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.offboarding ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.offboarding FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.offboarding USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());

GRANT SELECT ON nemura.onboarding_settings, nemura.join_task, nemura.offboarding TO nemura_api, nemura_worker, nemura_readonly;
GRANT INSERT (invite_to_fyniq, updated_by, updated_at), UPDATE (invite_to_fyniq, updated_by, updated_at) ON nemura.onboarding_settings TO nemura_api;
GRANT INSERT (id, person_id, kind, raised_at), UPDATE (state, decided_by, decided_at, note) ON nemura.join_task TO nemura_api;
GRANT INSERT (id, person_id, kind, notice_given_on, last_day, retrenchment_notified_on, equipment_returned_on, exit_interview_on, fyniq_access_removed_on, created_by, created_at, updated_at),
      UPDATE (kind, notice_given_on, last_day, retrenchment_notified_on, equipment_returned_on, exit_interview_on, fyniq_access_removed_on, state, updated_at) ON nemura.offboarding TO nemura_api;
