-- When a person is away (PRD-001 FR-PPL-06; PRD-002 Appendix E leave.taken.changed; p1-nemura-fyniq-link design D27; needs a human review: a new scope's effect and a new table of personal data).
--
-- people_leave is what Nemura says of approved leave: the person, the dates, whether the first and last day are half days, and the days, and nothing of the leave's type, reason or balance (Appendix E lists no
-- more, and the API refuses a field it does not list). It informs, and is never a grant or a posting: the Payroll page and the approvals show who is away, and no money moves on it. An entry is replaced by a newer
-- (epoch, version) of the same entry, and a cancellation is a newer version with cancelled set, so an older word arriving late changes nothing.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.people_leave (
  organisation_id uuid         NOT NULL DEFAULT fyniq.current_org(),
  id              uuid         NOT NULL DEFAULT uuidv7(),
  entry_id        uuid         NOT NULL,   -- Nemura's leave entry
  person_id       uuid         NOT NULL,
  start_date      date         NOT NULL,
  end_date        date         NOT NULL,
  start_half      boolean      NOT NULL DEFAULT false,
  end_half        boolean      NOT NULL DEFAULT false,
  days            numeric(8,2) NOT NULL,
  cancelled       boolean      NOT NULL DEFAULT false,
  version         bigint       NOT NULL,   -- the entry's version in Nemura
  epoch           integer      NOT NULL,   -- the link's epoch it came in
  received_at     timestamptz  NOT NULL,
  CONSTRAINT people_leave_pkey PRIMARY KEY (organisation_id, entry_id),
  CONSTRAINT people_leave_id_key UNIQUE (organisation_id, id),
  CONSTRAINT people_leave_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT people_leave_dates CHECK (end_date >= start_date AND end_date - start_date < 366),
  CONSTRAINT people_leave_days CHECK (days <> 'NaN' AND days > 0 AND days <= 366),
  CONSTRAINT people_leave_version CHECK (version >= 1 AND epoch >= 1)
);
CREATE INDEX people_leave_between ON fyniq.people_leave (organisation_id, start_date, end_date);

-- +goose StatementBegin
CREATE FUNCTION fyniq.people_leave_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR NEW.entry_id IS DISTINCT FROM OLD.entry_id OR NEW.person_id IS DISTINCT FROM OLD.person_id OR (NEW.epoch, NEW.version) <= (OLD.epoch, OLD.version) THEN
    RAISE EXCEPTION 'Leave Nemura told Fyniq of is replaced by a newer version of the same entry, never changed back or deleted' USING ERRCODE = 'FY900',
      HINT = 'A cancellation is a newer version with cancelled set (FR-PPL-15).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER people_leave_guard BEFORE UPDATE OR DELETE ON fyniq.people_leave
  FOR EACH ROW EXECUTE FUNCTION fyniq.people_leave_guard();
CREATE TRIGGER people_leave_no_truncate BEFORE TRUNCATE ON fyniq.people_leave
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.people_leave
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('people_leave', 'id');

ALTER TABLE fyniq.people_leave ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.people_leave FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.people_leave
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

REVOKE ALL ON fyniq.people_leave FROM PUBLIC;
GRANT SELECT ON fyniq.people_leave TO fyniq_api, fyniq_worker, fyniq_readonly;
-- It arrives through the inbox, which the API runs; the worker only reads it.
GRANT INSERT (id, entry_id, person_id, start_date, end_date, start_half, end_half, days, cancelled, version, epoch, received_at),
      UPDATE (start_date, end_date, start_half, end_half, days, cancelled, version, epoch, received_at) ON fyniq.people_leave TO fyniq_api;
