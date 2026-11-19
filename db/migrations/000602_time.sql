-- Time entries, the running timer and weekly submissions (PRD-001 FR-PRJ-02, FR-PRJ-05, FR-PRJ-07; p1-projects task 2.2,
-- design D2 to D4).
--
-- Time is whole minutes by person, project, task and day, a billable flag and a note. An entry is a draft the person edits,
-- is submitted with the rest of that person's week on that project, and is approved or returned by the project's manager or
-- the Controller; an approved entry never changes again (a correction is a reversing entry with the opposite minutes, and a
-- new entry). Time posts nothing: salaries are in the ledger through payroll (FR-PRJ-12).
--
-- One submission is a person's week on one project, because the managers differ by project. The database keeps what must
-- hold whoever asks: nobody decides their own time, a decision is a manager's of the project or the Controller's (FY602), and
-- an entry follows its submission (FY603). Audit events are written under the person's week (object time_week), one event
-- for a saved grid, not one for each entry.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- The Monday of a day's ISO week, and a person's week as an ID the audit trail names (a hash, so it needs no table).
-- +goose StatementBegin
CREATE FUNCTION fyniq.time_week_start(d date) RETURNS date
LANGUAGE sql IMMUTABLE PARALLEL SAFE
SET search_path = pg_catalog
AS $$ SELECT d - (extract(isodow FROM d)::integer - 1) $$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION fyniq.time_week_id(u uuid, d date) RETURNS uuid
LANGUAGE sql IMMUTABLE PARALLEL SAFE
SET search_path = pg_catalog, fyniq
AS $$ SELECT md5(u::text || ':' || fyniq.time_week_start(d)::text)::uuid $$;
-- +goose StatementEnd

CREATE TABLE fyniq.timesheet_submission (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  user_id         uuid        NOT NULL,
  project_id      uuid        NOT NULL,
  week_start      date        NOT NULL,
  status          text        NOT NULL DEFAULT 'submitted',
  submitted_at    timestamptz NOT NULL DEFAULT now(),
  decided_by      uuid,
  decided_at      timestamptz,
  comment         text        NOT NULL DEFAULT '',
  week_id         uuid        GENERATED ALWAYS AS (fyniq.time_week_id(user_id, week_start)) STORED,
  version         integer     NOT NULL DEFAULT 1,
  CONSTRAINT timesheet_submission_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT timesheet_submission_project_fkey FOREIGN KEY (organisation_id, project_id) REFERENCES fyniq.project (organisation_id, id),
  CONSTRAINT timesheet_submission_user_fkey FOREIGN KEY (organisation_id, user_id) REFERENCES fyniq.user_membership (organisation_id, user_id),
  CONSTRAINT timesheet_submission_decider_fkey FOREIGN KEY (organisation_id, decided_by) REFERENCES fyniq.user_membership (organisation_id, user_id),
  CONSTRAINT timesheet_submission_monday CHECK (extract(isodow FROM week_start) = 1),
  CONSTRAINT timesheet_submission_status_known CHECK (status IN ('submitted', 'approved', 'returned')),
  CONSTRAINT timesheet_submission_decided CHECK ((status = 'submitted') = (decided_by IS NULL AND decided_at IS NULL)),
  CONSTRAINT timesheet_submission_comment_length CHECK (length(comment) <= 1000),
  CONSTRAINT timesheet_submission_returned_says_why CHECK (status <> 'returned' OR length(btrim(comment)) > 0),
  CONSTRAINT timesheet_submission_version_positive CHECK (version >= 1)
);
-- One open submission of a person's week on a project.
CREATE UNIQUE INDEX timesheet_submission_open ON fyniq.timesheet_submission (organisation_id, user_id, project_id, week_start) WHERE status = 'submitted';
CREATE INDEX timesheet_submission_project_idx ON fyniq.timesheet_submission (organisation_id, project_id, status);

CREATE TABLE fyniq.time_entry (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  user_id         uuid        NOT NULL,
  project_id      uuid        NOT NULL,
  task_id         uuid        NOT NULL,
  work_date       date        NOT NULL,
  minutes         integer     NOT NULL,
  billable        boolean     NOT NULL,
  note            text        NOT NULL DEFAULT '',
  status          text        NOT NULL DEFAULT 'draft',
  submission_id   uuid,
  reverses_id     uuid,
  week_id         uuid        GENERATED ALWAYS AS (fyniq.time_week_id(user_id, work_date)) STORED,
  version         integer     NOT NULL DEFAULT 1,
  created_at      timestamptz NOT NULL DEFAULT now(),
  changed_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT time_entry_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT time_entry_project_fkey FOREIGN KEY (organisation_id, project_id) REFERENCES fyniq.project (organisation_id, id),
  CONSTRAINT time_entry_task_fkey FOREIGN KEY (organisation_id, project_id, task_id)
    REFERENCES fyniq.project_task (organisation_id, project_id, id),
  CONSTRAINT time_entry_user_fkey FOREIGN KEY (organisation_id, user_id) REFERENCES fyniq.user_membership (organisation_id, user_id),
  CONSTRAINT time_entry_reverses_fkey FOREIGN KEY (organisation_id, reverses_id) REFERENCES fyniq.time_entry (organisation_id, id),
  CONSTRAINT time_entry_submission_fkey FOREIGN KEY (organisation_id, submission_id) REFERENCES fyniq.timesheet_submission (organisation_id, id),
  -- Whole minutes, at most a day, never none; a reversing entry is the one with minutes below zero.
  CONSTRAINT time_entry_minutes CHECK (minutes BETWEEN -1440 AND 1440 AND minutes <> 0 AND (reverses_id IS NOT NULL) = (minutes < 0)),
  CONSTRAINT time_entry_status_known CHECK (status IN ('draft', 'submitted', 'approved', 'returned')),
  CONSTRAINT time_entry_submission_follows_status CHECK (status = 'returned' OR (status = 'draft') = (submission_id IS NULL)),
  CONSTRAINT time_entry_note_length CHECK (length(note) <= 500),
  CONSTRAINT time_entry_version_positive CHECK (version >= 1)
);
CREATE UNIQUE INDEX time_entry_one_reversal ON fyniq.time_entry (organisation_id, reverses_id) WHERE reverses_id IS NOT NULL;
CREATE INDEX time_entry_person_idx ON fyniq.time_entry (organisation_id, user_id, work_date);
CREATE INDEX time_entry_project_idx ON fyniq.time_entry (organisation_id, project_id, work_date);
CREATE INDEX time_entry_submission_idx ON fyniq.time_entry (organisation_id, submission_id) WHERE submission_id IS NOT NULL;

-- One running timer a person (FR-PRJ-05): stopped into a draft entry.
CREATE TABLE fyniq.time_timer (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  user_id         uuid        NOT NULL,
  project_id      uuid        NOT NULL,
  task_id         uuid        NOT NULL,
  billable        boolean     NOT NULL,
  note            text        NOT NULL DEFAULT '',
  started_at      timestamptz NOT NULL,
  CONSTRAINT time_timer_pkey PRIMARY KEY (organisation_id, user_id),
  CONSTRAINT time_timer_id_key UNIQUE (organisation_id, id),
  CONSTRAINT time_timer_task_fkey FOREIGN KEY (organisation_id, project_id, task_id)
    REFERENCES fyniq.project_task (organisation_id, project_id, id),
  CONSTRAINT time_timer_user_fkey FOREIGN KEY (organisation_id, user_id) REFERENCES fyniq.user_membership (organisation_id, user_id),
  CONSTRAINT time_timer_note_length CHECK (length(note) <= 500)
);

-- +goose StatementBegin
-- A decision on a week is a manager's of the project or the Controller's, never the person's own (FY602); a submission
-- keeps its person, project and week, is never deleted, and is decided once.
CREATE FUNCTION fyniq.timesheet_submission_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  manages boolean;
  controller boolean;
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A submitted week is never deleted' USING ERRCODE = 'FY602', HINT = 'Return it with a comment instead';
  END IF;
  IF TG_OP = 'INSERT' THEN
    IF NEW.status <> 'submitted' OR NEW.version <> 1 THEN
      RAISE EXCEPTION 'A week is submitted first, at version 1' USING ERRCODE = 'FY602';
    END IF;
    RETURN NEW;
  END IF;
  IF OLD.status <> 'submitted' THEN
    RAISE EXCEPTION 'A decided week is not decided again' USING ERRCODE = 'FY602', HINT = 'Correct an approved entry with a reversing entry, and submit the week again';
  END IF;
  IF NEW.version <> OLD.version + 1 OR (NEW.user_id, NEW.project_id, NEW.week_start, NEW.submitted_at)
     IS DISTINCT FROM (OLD.user_id, OLD.project_id, OLD.week_start, OLD.submitted_at) THEN
    RAISE EXCEPTION 'A week changes one version at a time and keeps its person, project and week' USING ERRCODE = 'FY602';
  END IF;
  IF NEW.status NOT IN ('approved', 'returned') OR NEW.decided_by IS NULL THEN
    RAISE EXCEPTION 'A submitted week is approved or returned by a named person' USING ERRCODE = 'FY602';
  END IF;
  IF NEW.decided_by = NEW.user_id THEN
    RAISE EXCEPTION 'Nobody decides their own time' USING ERRCODE = 'FY602', HINT = 'Another manager of the project, or the Controller, decides it';
  END IF;
  SELECT EXISTS (SELECT 1 FROM fyniq.project_member m
                  WHERE m.organisation_id = NEW.organisation_id AND m.project_id = NEW.project_id AND m.user_id = NEW.decided_by
                    AND m.is_manager AND m.is_active) INTO manages;
  SELECT EXISTS (SELECT 1 FROM fyniq.user_membership um
                   JOIN fyniq.role_assignment r ON r.organisation_id = um.organisation_id AND r.membership_id = um.id
                  WHERE um.organisation_id = NEW.organisation_id AND um.user_id = NEW.decided_by AND um.status = 'active'
                    AND r.role = 'controller' AND r.revoked_at IS NULL AND (r.expires_at IS NULL OR r.expires_at > now())) INTO controller;
  IF NOT (manages OR controller) THEN
    RAISE EXCEPTION 'Only a manager of the project, or the Controller, decides its time' USING ERRCODE = 'FY602',
      HINT = 'Name the person a manager of the project, and give them the Approver role';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER timesheet_submission_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.timesheet_submission
  FOR EACH ROW EXECUTE FUNCTION fyniq.timesheet_submission_guard();

-- +goose StatementBegin
-- An entry is a draft or returned one's to edit, follows its submission through submitted, approved or returned, and is
-- never changed or deleted once submitted; a reversing entry has the opposite minutes of an approved entry, once (FY603).
CREATE FUNCTION fyniq.time_entry_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  p fyniq.project;
  orig fyniq.time_entry;
  sub fyniq.timesheet_submission;
  tk fyniq.project_task;
  member boolean;
BEGIN
  IF TG_OP = 'DELETE' THEN
    IF OLD.status NOT IN ('draft', 'returned') THEN
      RAISE EXCEPTION 'A submitted or approved entry is never deleted' USING ERRCODE = 'FY603', HINT = 'Correct an approved entry with a reversing entry';
    END IF;
    RETURN OLD;
  END IF;
  IF TG_OP = 'INSERT' THEN
    IF NEW.status <> 'draft' OR NEW.version <> 1 OR NEW.submission_id IS NOT NULL THEN
      RAISE EXCEPTION 'An entry starts as a draft, at version 1' USING ERRCODE = 'FY603';
    END IF;
    SELECT * INTO p FROM fyniq.project x WHERE x.organisation_id = NEW.organisation_id AND x.id = NEW.project_id;
    IF NEW.reverses_id IS NULL THEN
      IF p.status = 'closed' THEN
        RAISE EXCEPTION 'A closed project takes no new time' USING ERRCODE = 'FY603', HINT = 'Reopen the project first';
      END IF;
      SELECT EXISTS (SELECT 1 FROM fyniq.project_member m WHERE m.organisation_id = NEW.organisation_id AND m.project_id = NEW.project_id
                        AND m.user_id = NEW.user_id AND m.is_active) INTO member;
      IF NOT member THEN
        RAISE EXCEPTION 'Time is logged by a member of the project' USING ERRCODE = 'FY603', HINT = 'Ask a project manager to add you to the project';
      END IF;
      SELECT * INTO tk FROM fyniq.project_task t WHERE t.organisation_id = NEW.organisation_id AND t.id = NEW.task_id;
      IF NOT tk.is_active THEN
        RAISE EXCEPTION 'Time is logged against a task in use' USING ERRCODE = 'FY603', HINT = 'Choose another task, or put the task back in use';
      END IF;
      IF NEW.billable AND p.billing_method = 'internal' THEN
        RAISE EXCEPTION 'Time on an internal project is not billable' USING ERRCODE = 'FY603';
      END IF;
    ELSE
      SELECT * INTO orig FROM fyniq.time_entry o WHERE o.organisation_id = NEW.organisation_id AND o.id = NEW.reverses_id;
      IF orig.status IS DISTINCT FROM 'approved' OR orig.reverses_id IS NOT NULL
         OR (orig.user_id, orig.project_id, orig.task_id, orig.work_date, orig.billable)
            IS DISTINCT FROM (NEW.user_id, NEW.project_id, NEW.task_id, NEW.work_date, NEW.billable)
         OR orig.minutes <> -NEW.minutes THEN
        RAISE EXCEPTION 'A reversing entry has the opposite minutes of an approved entry, on its day, person, task and billing' USING ERRCODE = 'FY603';
      END IF;
    END IF;
    RETURN NEW;
  END IF;
  -- An update.
  IF OLD.status IN ('approved') THEN
    RAISE EXCEPTION 'An approved entry never changes' USING ERRCODE = 'FY603', HINT = 'Correct it with a reversing entry and a new one, and submit the week again';
  END IF;
  IF NEW.version <> OLD.version + 1 OR (NEW.user_id, NEW.project_id, NEW.reverses_id, NEW.created_at)
     IS DISTINCT FROM (OLD.user_id, OLD.project_id, OLD.reverses_id, OLD.created_at) THEN
    RAISE EXCEPTION 'An entry changes one version at a time and keeps its person, project and reversal' USING ERRCODE = 'FY603';
  END IF;
  IF OLD.status = 'submitted' THEN
    -- Only the submission's decision moves it on.
    IF NEW.status NOT IN ('approved', 'returned') OR (NEW.task_id, NEW.work_date, NEW.minutes, NEW.billable, NEW.note, NEW.submission_id)
       IS DISTINCT FROM (OLD.task_id, OLD.work_date, OLD.minutes, OLD.billable, OLD.note, OLD.submission_id) THEN
      RAISE EXCEPTION 'A submitted entry changes only when its week is decided' USING ERRCODE = 'FY603';
    END IF;
  ELSE
    -- A draft or returned entry is edited, or submitted with its week; a reversing entry keeps its day, minutes and task.
    IF OLD.reverses_id IS NOT NULL AND (NEW.task_id, NEW.work_date, NEW.minutes, NEW.billable) IS DISTINCT FROM (OLD.task_id, OLD.work_date, OLD.minutes, OLD.billable) THEN
      RAISE EXCEPTION 'A reversing entry keeps the day, minutes, task and billing of what it reverses' USING ERRCODE = 'FY603';
    END IF;
    IF NEW.status NOT IN ('draft', 'returned', 'submitted') THEN
      RAISE EXCEPTION 'A draft or returned entry is submitted with its week' USING ERRCODE = 'FY603';
    END IF;
    IF NEW.billable AND (SELECT x.billing_method FROM fyniq.project x WHERE x.organisation_id = NEW.organisation_id AND x.id = NEW.project_id) = 'internal'
       AND NEW.reverses_id IS NULL THEN
      RAISE EXCEPTION 'Time on an internal project is not billable' USING ERRCODE = 'FY603';
    END IF;
  END IF;
  IF NEW.status IN ('submitted', 'approved', 'returned') AND NEW.status IS DISTINCT FROM OLD.status THEN
    SELECT * INTO sub FROM fyniq.timesheet_submission s WHERE s.organisation_id = NEW.organisation_id AND s.id = NEW.submission_id;
    IF sub.id IS NULL OR (sub.user_id, sub.project_id) IS DISTINCT FROM (NEW.user_id, NEW.project_id)
       OR sub.week_start <> fyniq.time_week_start(NEW.work_date) OR sub.status <> NEW.status THEN
      RAISE EXCEPTION 'An entry follows its submission: the same person, project and week, and the same status' USING ERRCODE = 'FY603';
    END IF;
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER time_entry_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.time_entry
  FOR EACH ROW EXECUTE FUNCTION fyniq.time_entry_guard();

-- +goose StatementBegin
-- A decided week has every entry of it decided the same way by the end of the transaction (FY603).
CREATE FUNCTION fyniq.timesheet_submission_settled() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF EXISTS (SELECT 1 FROM fyniq.time_entry e
              WHERE e.organisation_id = NEW.organisation_id AND e.submission_id = NEW.id AND e.status <> NEW.status) THEN
    RAISE EXCEPTION 'The entries of a week follow its decision' USING ERRCODE = 'FY603',
      HINT = 'Approve or return every entry of the week with it';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER timesheet_submission_settled AFTER INSERT OR UPDATE ON fyniq.timesheet_submission
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.timesheet_submission_settled();

-- +goose StatementBegin
-- A timer is a project member's, on a task in use of that project (FY604).
CREATE FUNCTION fyniq.time_timer_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  member boolean;
  live boolean;
BEGIN
  IF TG_OP = 'UPDATE' THEN
    RAISE EXCEPTION 'A timer is stopped, not changed' USING ERRCODE = 'FY604', HINT = 'Stop it and start another';
  END IF;
  SELECT EXISTS (SELECT 1 FROM fyniq.project_member m WHERE m.organisation_id = NEW.organisation_id AND m.project_id = NEW.project_id
                    AND m.user_id = NEW.user_id AND m.is_active) INTO member;
  SELECT t.is_active INTO live FROM fyniq.project_task t WHERE t.organisation_id = NEW.organisation_id AND t.id = NEW.task_id;
  IF NOT member OR NOT COALESCE(live, false) THEN
    RAISE EXCEPTION 'A timer runs for a member of the project, on a task in use' USING ERRCODE = 'FY604';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER time_timer_guard BEFORE INSERT OR UPDATE ON fyniq.time_timer
  FOR EACH ROW EXECUTE FUNCTION fyniq.time_timer_guard();

ALTER TABLE fyniq.timesheet_submission ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.timesheet_submission FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.timesheet_submission
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.time_entry ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.time_entry FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.time_entry
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.time_timer ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.time_timer FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.time_timer
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

-- AUD-08: every change has its audit event under the person's week, and a timer under its person.
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.timesheet_submission
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('time_week', 'week_id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.time_entry
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('time_week', 'week_id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.time_timer
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('time_timer', 'user_id');

GRANT SELECT ON fyniq.timesheet_submission, fyniq.time_entry, fyniq.time_timer TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
GRANT INSERT (user_id, project_id, week_start) ON fyniq.timesheet_submission TO fyniq_api;
GRANT UPDATE (status, decided_by, decided_at, comment, version) ON fyniq.timesheet_submission TO fyniq_api;
GRANT INSERT (user_id, project_id, task_id, work_date, minutes, billable, note, reverses_id) ON fyniq.time_entry TO fyniq_api;
GRANT UPDATE (task_id, work_date, minutes, billable, note, status, submission_id, version, changed_at) ON fyniq.time_entry TO fyniq_api;
GRANT DELETE ON fyniq.time_entry TO fyniq_api;
GRANT INSERT (user_id, project_id, task_id, billable, note, started_at) ON fyniq.time_timer TO fyniq_api;
GRANT DELETE ON fyniq.time_timer TO fyniq_api;

GRANT EXECUTE ON FUNCTION fyniq.time_week_start(date), fyniq.time_week_id(uuid, date) TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
