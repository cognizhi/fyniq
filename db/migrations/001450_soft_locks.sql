-- Soft locks (PRD-001 FR-GL-09; p2-mobile-approvals-automation design D6, rev 0.623).
--
-- The period's status already allows soft_locked; nothing set it. The hard close (INV-03) stays the database's and is unchanged: journal_entry_assign refuses only a
-- closed period. A soft lock is a weaker state the domain enforces (FR-GL-09): a posting into a soft-locked period, or of a locked module into a period, is refused
-- unless its person holds a live PASS of the period: a person with the Accountant role made it with a reason and a second factor, it lasts at most 8 hours, and every
-- posting made under it is kept in the exceptions report. This migration adds the three tables the domain reads and writes; no existing constraint changes.
--
--   period_module_lock    which module (ar, ap, banking, payroll) of which period is locked, apart from the period's own status
--   soft_lock_pass        a person's exception for a period: reason, expiry; made once, never changed or deleted
--   soft_lock_exception   one row per posting made under a pass; made once, never changed or deleted

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.period_module_lock (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  period_id       uuid        NOT NULL,
  module          text        NOT NULL,
  locked          boolean     NOT NULL,
  version         integer     NOT NULL DEFAULT 1,
  changed_by      uuid        NOT NULL,
  changed_at      timestamptz NOT NULL,
  CONSTRAINT period_module_lock_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT period_module_lock_one UNIQUE (organisation_id, period_id, module),
  CONSTRAINT period_module_lock_period_fkey FOREIGN KEY (organisation_id, period_id) REFERENCES fyniq.period (organisation_id, id),
  CONSTRAINT period_module_lock_user_fkey FOREIGN KEY (changed_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT period_module_lock_module_known CHECK (module IN ('ar', 'ap', 'banking', 'payroll')),
  CONSTRAINT period_module_lock_version_positive CHECK (version >= 1)
);

CREATE TABLE fyniq.soft_lock_pass (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  period_id       uuid        NOT NULL,
  user_id         uuid        NOT NULL,
  reason          text        NOT NULL,
  created_at      timestamptz NOT NULL,
  expires_at      timestamptz NOT NULL,
  CONSTRAINT soft_lock_pass_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT soft_lock_pass_period_fkey FOREIGN KEY (organisation_id, period_id) REFERENCES fyniq.period (organisation_id, id),
  CONSTRAINT soft_lock_pass_user_fkey FOREIGN KEY (user_id) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT soft_lock_pass_reason_length CHECK (length(btrim(reason)) BETWEEN 10 AND 500),
  CONSTRAINT soft_lock_pass_short CHECK (expires_at > created_at AND expires_at <= created_at + interval '8 hours')
);
CREATE INDEX soft_lock_pass_live ON fyniq.soft_lock_pass (organisation_id, period_id, user_id, expires_at);

CREATE TABLE fyniq.soft_lock_exception (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  pass_id         uuid        NOT NULL,
  period_id       uuid        NOT NULL,
  entry_id        uuid        NOT NULL,   -- no foreign key: the purge after retention (AUD-10) may remove the entry, and the report keeps its line
  user_id         uuid        NOT NULL,
  scope           text        NOT NULL,   -- period, or the locked module
  source_type     text        NOT NULL,
  posting_date    date        NOT NULL,
  reason          text        NOT NULL,
  created_at      timestamptz NOT NULL,
  CONSTRAINT soft_lock_exception_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT soft_lock_exception_pass_fkey FOREIGN KEY (organisation_id, pass_id) REFERENCES fyniq.soft_lock_pass (organisation_id, id),
  CONSTRAINT soft_lock_exception_period_fkey FOREIGN KEY (organisation_id, period_id) REFERENCES fyniq.period (organisation_id, id),
  CONSTRAINT soft_lock_exception_user_fkey FOREIGN KEY (user_id) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT soft_lock_exception_scope_known CHECK (scope IN ('period', 'ar', 'ap', 'banking', 'payroll')),
  CONSTRAINT soft_lock_exception_entry_once UNIQUE (organisation_id, entry_id)
);
CREATE INDEX soft_lock_exception_recent ON fyniq.soft_lock_exception (organisation_id, created_at DESC, id);

-- A pass and an exception are made once and never change or go (FYL01); a module lock keeps its identity (FYL02).
-- +goose StatementBegin
CREATE FUNCTION fyniq.soft_lock_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_TABLE_NAME = 'period_module_lock' AND TG_OP = 'UPDATE' THEN
    IF (NEW.organisation_id, NEW.id, NEW.period_id, NEW.module) IS DISTINCT FROM (OLD.organisation_id, OLD.id, OLD.period_id, OLD.module)
       OR NEW.version <> OLD.version + 1 THEN
      RAISE EXCEPTION 'A module lock of a period changes only as its next version, and never its period or module' USING ERRCODE = 'FYL02',
        HINT = 'Read the lock again (FR-GL-09).';
    END IF;
    RETURN NEW;
  END IF;
  IF TG_TABLE_NAME = 'period_module_lock' THEN
    RAISE EXCEPTION 'A module lock of a period is kept: it is released, never deleted' USING ERRCODE = 'FYL02', HINT = 'Release the lock instead (FR-GL-09).';
  END IF;
  RAISE EXCEPTION 'A soft-lock pass and the postings made under it are kept as they were made' USING ERRCODE = 'FYL01',
    HINT = 'Make a new pass if one is needed; the exceptions report is a record (FR-GL-09).';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER soft_lock_guard BEFORE UPDATE OR DELETE ON fyniq.period_module_lock FOR EACH ROW EXECUTE FUNCTION fyniq.soft_lock_guard();
CREATE TRIGGER soft_lock_guard BEFORE UPDATE OR DELETE ON fyniq.soft_lock_pass FOR EACH ROW EXECUTE FUNCTION fyniq.soft_lock_guard();
CREATE TRIGGER soft_lock_guard BEFORE UPDATE OR DELETE ON fyniq.soft_lock_exception FOR EACH ROW EXECUTE FUNCTION fyniq.soft_lock_guard();
CREATE TRIGGER soft_lock_no_truncate BEFORE TRUNCATE ON fyniq.period_module_lock FOR EACH STATEMENT EXECUTE FUNCTION fyniq.soft_lock_guard();
CREATE TRIGGER soft_lock_no_truncate BEFORE TRUNCATE ON fyniq.soft_lock_pass FOR EACH STATEMENT EXECUTE FUNCTION fyniq.soft_lock_guard();
CREATE TRIGGER soft_lock_no_truncate BEFORE TRUNCATE ON fyniq.soft_lock_exception FOR EACH STATEMENT EXECUTE FUNCTION fyniq.soft_lock_guard();

ALTER TABLE fyniq.period_module_lock ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.period_module_lock FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.period_module_lock USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.soft_lock_pass ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.soft_lock_pass FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.soft_lock_pass USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.soft_lock_exception ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.soft_lock_exception FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.soft_lock_exception USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.period_module_lock
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('period_module_lock', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.soft_lock_pass
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('soft_lock_pass', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.soft_lock_exception
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('soft_lock_exception', 'id');

-- Every role that posts reads the locks (the worker and the executor post too, and are refused by them); only the API writes, and only the API's posting records an
-- exception, which only a person's pass allows.
REVOKE ALL ON fyniq.period_module_lock, fyniq.soft_lock_pass, fyniq.soft_lock_exception FROM PUBLIC;
GRANT SELECT ON fyniq.period_module_lock, fyniq.soft_lock_pass, fyniq.soft_lock_exception TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
GRANT INSERT (period_id, module, locked, changed_by, changed_at) ON fyniq.period_module_lock TO fyniq_api;
GRANT UPDATE (locked, version, changed_by, changed_at) ON fyniq.period_module_lock TO fyniq_api;
GRANT INSERT (period_id, user_id, reason, created_at, expires_at) ON fyniq.soft_lock_pass TO fyniq_api;
GRANT INSERT (pass_id, period_id, entry_id, user_id, scope, source_type, posting_date, reason, created_at) ON fyniq.soft_lock_exception TO fyniq_api;
