-- The report builder's saved definitions and their schedules (PRD-001 FR-RPT-04, FR-RPT-07, rev 0.540; p2-reporting-planning
-- tasks 2.2 and 2.3, design D1 to D3).
--
-- A definition is a saved report (rows, columns, filters as JSON, validated by the application against fyniq-report@1): private
-- to its owner or shared with the people who may read the whole ledger. It changes only as its next version and is never deleted,
-- only archived, so a schedule or a board pack that names it keeps naming it. A schedule says when a definition is sent and to
-- whom; each run is recorded once, with the people it skipped and why, and sends no figure in the database: the worker reads the
-- definition and the books at send time, under the permission checks the application makes (D3).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.report_definition (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  name            text        NOT NULL,
  description     text        NOT NULL DEFAULT '',
  definition      jsonb       NOT NULL,
  visibility      text        NOT NULL DEFAULT 'private',
  owner_id        uuid        NOT NULL,   -- the member who made it
  version         integer     NOT NULL DEFAULT 1,
  created_by      text        NOT NULL,
  created_at      timestamptz NOT NULL,
  updated_by      text        NOT NULL,
  updated_at      timestamptz NOT NULL,
  archived_at     timestamptz,
  CONSTRAINT report_definition_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT report_definition_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT report_definition_visibility CHECK (visibility IN ('private', 'shared')),
  CONSTRAINT report_definition_name CHECK (length(btrim(name)) BETWEEN 1 AND 80),
  CONSTRAINT report_definition_description CHECK (length(description) <= 500),
  CONSTRAINT report_definition_object CHECK (jsonb_typeof(definition) = 'object' AND definition ->> 'schema' = 'fyniq-report@1'
    AND jsonb_typeof(definition -> 'rows') = 'array' AND jsonb_array_length(definition -> 'rows') BETWEEN 1 AND 60
    AND jsonb_typeof(definition -> 'columns') = 'array' AND jsonb_array_length(definition -> 'columns') BETWEEN 1 AND 12),
  CONSTRAINT report_definition_size CHECK (pg_column_size(definition) <= 65536),
  CONSTRAINT report_definition_by_form CHECK (created_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$' AND updated_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$'),
  CONSTRAINT report_definition_version CHECK (version >= 1)
);
-- A name is used once among the live definitions of an organisation (case-insensitive).
CREATE UNIQUE INDEX report_definition_name_live ON fyniq.report_definition (organisation_id, lower(btrim(name))) WHERE archived_at IS NULL;

CREATE TABLE fyniq.report_schedule (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  definition_id   uuid        NOT NULL,
  cadence         text        NOT NULL,
  weekday         smallint,               -- 1 Monday to 7 Sunday, for a weekly schedule
  day_of_month    smallint,               -- 1 to 28, for a monthly schedule
  period_rule     text        NOT NULL,
  channel         text        NOT NULL DEFAULT 'email',
  recipients      uuid[]      NOT NULL,   -- member user IDs, each checked again at send time
  slack_channel   text,
  active          boolean     NOT NULL DEFAULT true,
  next_run_on     date        NOT NULL,
  last_run_on     date,
  version         integer     NOT NULL DEFAULT 1,
  created_by      text        NOT NULL,
  created_at      timestamptz NOT NULL,
  updated_at      timestamptz NOT NULL,
  CONSTRAINT report_schedule_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT report_schedule_definition_fkey FOREIGN KEY (organisation_id, definition_id) REFERENCES fyniq.report_definition (organisation_id, id),
  CONSTRAINT report_schedule_cadence CHECK ((cadence = 'weekly' AND weekday BETWEEN 1 AND 7 AND day_of_month IS NULL)
    OR (cadence = 'monthly' AND day_of_month BETWEEN 1 AND 28 AND weekday IS NULL)),
  CONSTRAINT report_schedule_period_rule CHECK (period_rule IN ('last_closed_month', 'month_to_date')),
  CONSTRAINT report_schedule_channel CHECK ((channel = 'email' AND slack_channel IS NULL) OR (channel = 'slack' AND length(btrim(slack_channel)) BETWEEN 1 AND 80)),
  CONSTRAINT report_schedule_recipients CHECK (cardinality(recipients) BETWEEN 1 AND 50),
  CONSTRAINT report_schedule_by_form CHECK (created_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$'),
  CONSTRAINT report_schedule_version CHECK (version >= 1)
);
CREATE INDEX report_schedule_due ON fyniq.report_schedule (organisation_id, next_run_on) WHERE active;

-- One run of a schedule: what it sent and whom it skipped (a recipient who no longer may read the report, or who is no longer a
-- member), once per schedule and day. Never changed or deleted.
CREATE TABLE fyniq.report_schedule_run (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  schedule_id     uuid        NOT NULL,
  ran_on          date        NOT NULL,
  period_label    text        NOT NULL,
  ledger_revision bigint      NOT NULL,
  sent            integer     NOT NULL,
  skipped         jsonb       NOT NULL DEFAULT '[]',
  ran_at          timestamptz NOT NULL,
  CONSTRAINT report_schedule_run_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT report_schedule_run_schedule_fkey FOREIGN KEY (organisation_id, schedule_id) REFERENCES fyniq.report_schedule (organisation_id, id),
  CONSTRAINT report_schedule_run_once UNIQUE (organisation_id, schedule_id, ran_on),
  CONSTRAINT report_schedule_run_counts CHECK (sent >= 0 AND jsonb_typeof(skipped) = 'array' AND jsonb_array_length(skipped) <= 50 AND ledger_revision >= 0)
);

-- +goose StatementBegin
-- A definition is written at version 1, changed only as its next version, and never deleted (it is archived instead).
CREATE FUNCTION fyniq.report_definition_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF NEW.version <> 1 THEN
      RAISE EXCEPTION 'A report definition is written at version 1' USING ERRCODE = 'FYR01', HINT = 'Save it; a change is its next version (FR-RPT-04).';
    END IF;
    RETURN NEW;
  END IF;
  IF TG_OP <> 'UPDATE' THEN
    RAISE EXCEPTION 'A report definition is never deleted or truncated' USING ERRCODE = 'FYR01', HINT = 'Archive it instead (FR-RPT-04).';
  END IF;
  IF NEW.id IS DISTINCT FROM OLD.id OR NEW.organisation_id IS DISTINCT FROM OLD.organisation_id OR NEW.owner_id IS DISTINCT FROM OLD.owner_id
     OR NEW.created_by IS DISTINCT FROM OLD.created_by OR NEW.created_at IS DISTINCT FROM OLD.created_at
     OR NEW.version <> OLD.version + 1 OR NEW.updated_at < OLD.updated_at OR OLD.archived_at IS NOT NULL THEN
    RAISE EXCEPTION 'A report definition changes only as its next version, and an archived one not at all' USING ERRCODE = 'FYR01',
      HINT = 'Read the definition and save the change as its next version (FR-RPT-04).';
  END IF;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER report_definition_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.report_definition
  FOR EACH ROW EXECUTE FUNCTION fyniq.report_definition_guard();
CREATE TRIGGER report_definition_no_truncate BEFORE TRUNCATE ON fyniq.report_definition
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.report_definition_guard();

-- +goose StatementBegin
-- A schedule is changed only as its next version and never deleted; its definition, owner and creation never change.
CREATE FUNCTION fyniq.report_schedule_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF NEW.version <> 1 THEN
      RAISE EXCEPTION 'A report schedule is written at version 1' USING ERRCODE = 'FYR02', HINT = 'Save it; a change is its next version (FR-RPT-07).';
    END IF;
    RETURN NEW;
  END IF;
  IF TG_OP <> 'UPDATE' THEN
    RAISE EXCEPTION 'A report schedule is never deleted or truncated' USING ERRCODE = 'FYR02', HINT = 'Switch it off instead (FR-RPT-07).';
  END IF;
  IF NEW.id IS DISTINCT FROM OLD.id OR NEW.organisation_id IS DISTINCT FROM OLD.organisation_id OR NEW.definition_id IS DISTINCT FROM OLD.definition_id
     OR NEW.created_by IS DISTINCT FROM OLD.created_by OR NEW.created_at IS DISTINCT FROM OLD.created_at OR NEW.version <> OLD.version + 1 THEN
    RAISE EXCEPTION 'A report schedule changes only as its next version, and never its report' USING ERRCODE = 'FYR02',
      HINT = 'Read the schedule and save the change as its next version (FR-RPT-07).';
  END IF;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER report_schedule_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.report_schedule
  FOR EACH ROW EXECUTE FUNCTION fyniq.report_schedule_guard();
CREATE TRIGGER report_schedule_no_truncate BEFORE TRUNCATE ON fyniq.report_schedule
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.report_schedule_guard();

-- +goose StatementBegin
CREATE FUNCTION fyniq.report_schedule_run_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'A recorded run of a report schedule is never changed or deleted' USING ERRCODE = 'FYR03', HINT = 'The next run is recorded as its own row (FR-RPT-07).';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER report_schedule_run_guard BEFORE UPDATE OR DELETE ON fyniq.report_schedule_run
  FOR EACH ROW EXECUTE FUNCTION fyniq.report_schedule_run_guard();
CREATE TRIGGER report_schedule_run_no_truncate BEFORE TRUNCATE ON fyniq.report_schedule_run
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.report_schedule_run_guard();

ALTER TABLE fyniq.report_definition ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.report_definition FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.report_definition USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.report_schedule ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.report_schedule FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.report_schedule USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.report_schedule_run ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.report_schedule_run FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.report_schedule_run USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

-- Every saved change is audited in the same transaction (AUD-01).
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.report_definition
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('report_definition', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.report_schedule
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('report_schedule', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.report_schedule_run
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('report_schedule_run', 'id');

REVOKE ALL ON fyniq.report_definition, fyniq.report_schedule, fyniq.report_schedule_run FROM PUBLIC;
GRANT SELECT ON fyniq.report_definition, fyniq.report_schedule, fyniq.report_schedule_run TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (name, description, definition, visibility, owner_id, created_by, created_at, updated_by, updated_at) ON fyniq.report_definition TO fyniq_api;
GRANT UPDATE (name, description, definition, visibility, updated_by, updated_at, archived_at, version) ON fyniq.report_definition TO fyniq_api;
GRANT INSERT (definition_id, cadence, weekday, day_of_month, period_rule, channel, recipients, slack_channel, active, next_run_on, created_by, created_at, updated_at)
  ON fyniq.report_schedule TO fyniq_api;
GRANT UPDATE (cadence, weekday, day_of_month, period_rule, channel, recipients, slack_channel, active, next_run_on, updated_at, version) ON fyniq.report_schedule TO fyniq_api;
-- The worker's send: it moves a schedule's dates on, and records the run; it changes nothing a person set.
GRANT UPDATE (last_run_on, next_run_on, updated_at, version) ON fyniq.report_schedule TO fyniq_worker;
GRANT INSERT (schedule_id, ran_on, period_label, ledger_revision, sent, skipped, ran_at) ON fyniq.report_schedule_run TO fyniq_worker;
