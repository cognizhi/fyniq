-- The close checklist (PRD-001 FR-GL-07, FM-4; the founders' decisions of 22 Oct 2026, rev 0.68; p1-general-ledger-close
-- task 7.2).
--
-- Each month's close has a checklist from the organisation's template: tasks with an owner and a due day. Until the
-- organisation saves a template of its own, the platform's recommended one applies (version 0, in code). A template is
-- saved as versions, each the whole list, and a version never changes or goes (FY165). Who may save one (the Accountant
-- or the Controller, C on "Period close / reopen") is the API's check.
--
-- What happens to a task in a month is an append-only log: done, skipped with a reason, or opened again, each by whom
-- and when; the latest entry for a task is its state. An entry is written only while the month is open (FY164; the
-- period read FOR SHARE, so it never interleaves with the close) and never changes or goes (FY165). The close checks
-- refuse the close while a task is neither done nor skipped (Appendix C.2 #58).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.close_checklist_template (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  version         integer     NOT NULL,
  tasks           jsonb       NOT NULL,  -- each task: its key, title, detail, owner role and due day
  written_by      text        NOT NULL,
  written_at      timestamptz NOT NULL,
  CONSTRAINT close_checklist_template_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT close_checklist_template_version_key UNIQUE (organisation_id, version),
  CONSTRAINT close_checklist_template_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT close_checklist_template_version_positive CHECK (version > 0),
  CONSTRAINT close_checklist_template_tasks_array CHECK (jsonb_typeof(tasks) = 'array' AND jsonb_array_length(tasks) BETWEEN 1 AND 40
    AND octet_length(tasks::text) <= 65536),
  CONSTRAINT close_checklist_template_actor_format CHECK (written_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);

CREATE TABLE fyniq.close_checklist_entry (
  organisation_id  uuid        NOT NULL DEFAULT fyniq.current_org(),
  id               uuid        NOT NULL DEFAULT uuidv7(),
  period_id        uuid        NOT NULL,
  task_key         text        NOT NULL,
  template_version integer     NOT NULL,  -- the template the task came from; 0 for the platform's recommended one
  state            text        NOT NULL,
  note             text,                  -- what was done, or why it was skipped
  written_by       text        NOT NULL,
  written_at       timestamptz NOT NULL,
  CONSTRAINT close_checklist_entry_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT close_checklist_entry_period_fkey FOREIGN KEY (organisation_id, period_id) REFERENCES fyniq.period (organisation_id, id),
  CONSTRAINT close_checklist_entry_key_format CHECK (task_key ~ '^[a-z0-9][a-z0-9-]{0,39}$'),
  CONSTRAINT close_checklist_entry_template_version CHECK (template_version >= 0),
  CONSTRAINT close_checklist_entry_state_known CHECK (state IN ('done', 'skipped', 'open')),
  CONSTRAINT close_checklist_entry_note_length CHECK (note IS NULL OR length(btrim(note)) BETWEEN 1 AND 1000),
  CONSTRAINT close_checklist_entry_skip_reason CHECK (state <> 'skipped' OR note IS NOT NULL),
  CONSTRAINT close_checklist_entry_actor_format CHECK (written_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);
CREATE INDEX close_checklist_entry_period
  ON fyniq.close_checklist_entry (organisation_id, period_id, task_key, written_at);

-- A template version and a checklist entry never change or go; an entry is written only while its month is open.
-- +goose StatementBegin
CREATE FUNCTION fyniq.close_checklist_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  p record;
BEGIN
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'A record of the close checklist never changes or goes' USING ERRCODE = 'FY165',
      HINT = 'Save a new template version, or mark the task again: the latest entry is its state (FR-GL-07).';
  END IF;
  IF TG_TABLE_NAME = 'close_checklist_entry' THEN
    SELECT status, starts_on, ends_on INTO p FROM fyniq.period
     WHERE organisation_id = NEW.organisation_id AND id = NEW.period_id
     FOR SHARE;
    IF p.status IS DISTINCT FROM 'open' THEN
      RAISE EXCEPTION 'The period % to % is %, so its checklist does not change now', p.starts_on, p.ends_on, p.status
        USING ERRCODE = 'FY164', HINT = 'Mark checklist tasks while the period is open; reopen it first (FR-GL-08).';
    END IF;
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER close_checklist_template_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.close_checklist_template
  FOR EACH ROW EXECUTE FUNCTION fyniq.close_checklist_guard();
CREATE TRIGGER close_checklist_entry_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.close_checklist_entry
  FOR EACH ROW EXECUTE FUNCTION fyniq.close_checklist_guard();
CREATE TRIGGER close_checklist_template_no_truncate BEFORE TRUNCATE ON fyniq.close_checklist_template
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();
CREATE TRIGGER close_checklist_entry_no_truncate BEFORE TRUNCATE ON fyniq.close_checklist_entry
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();

ALTER TABLE fyniq.close_checklist_template ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.close_checklist_template FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.close_checklist_template
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.close_checklist_entry ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.close_checklist_entry FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.close_checklist_entry
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.close_checklist_template
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('close_checklist_template', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.close_checklist_entry
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('close_checklist_entry', 'id');

GRANT SELECT ON fyniq.close_checklist_template, fyniq.close_checklist_entry TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
GRANT INSERT (version, tasks, written_by, written_at) ON fyniq.close_checklist_template TO fyniq_api;
GRANT INSERT (period_id, task_key, template_version, state, note, written_by, written_at) ON fyniq.close_checklist_entry TO fyniq_api;
