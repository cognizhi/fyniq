-- The people record (PRD-002 NM-PPL-01, NM-PPL-06, NM-PPL-08; p1-nemura-records-packs slice 1).
--
-- A person is the HR record: every employee, contractor and intern, with or without a login (a member is the login, and at most one
-- member belongs to a person). Job, manager, department, worksite, employment type and legal classification are effective-dated history:
-- a change ends one row and starts the next, never an edit. The reporting line that decides what a manager may see stays on members
-- (migration 000005): a manager needs a login to act, so the API keeps that line in step when both people have one (design D1).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE nemura.person (
  organisation_id  uuid        NOT NULL DEFAULT nemura.current_org(),
  id               uuid        NOT NULL DEFAULT uuidv7(),
  member_id        uuid,
  legal_name       text        NOT NULL,
  preferred_name   text,
  work_email       text,
  phone            text,
  employment_start date        NOT NULL,
  probation_end    date,
  leaving_date     date,
  leaving_reason   text,
  created_at       timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT person_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT person_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES nemura.organisation (organisation_id),
  CONSTRAINT person_member_fkey FOREIGN KEY (organisation_id, member_id) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT person_member_key UNIQUE (organisation_id, member_id),
  CONSTRAINT person_legal_name_length CHECK (length(btrim(legal_name)) BETWEEN 1 AND 200),
  CONSTRAINT person_preferred_name_length CHECK (preferred_name IS NULL OR length(btrim(preferred_name)) BETWEEN 1 AND 200),
  CONSTRAINT person_work_email_shape CHECK (work_email IS NULL OR (length(work_email) <= 254 AND work_email ~ '^[^@\s]+@[^@\s]+$')),
  CONSTRAINT person_phone_length CHECK (phone IS NULL OR length(phone) <= 40),
  CONSTRAINT person_probation_after_start CHECK (probation_end IS NULL OR probation_end >= employment_start),
  CONSTRAINT person_leaving_after_start CHECK (leaving_date IS NULL OR leaving_date >= employment_start),
  CONSTRAINT person_leaving_reason_length CHECK (leaving_reason IS NULL OR length(leaving_reason) <= 500)
);
CREATE INDEX person_name_idx ON nemura.person (organisation_id, lower(legal_name));

-- A person's facts that may change are edited in place only where they are not history (name, contact, probation, leaving); who they
-- are linked to as a login is set once. Nothing is deleted.
-- +goose StatementBegin
CREATE FUNCTION nemura.person_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR NEW.employment_start IS DISTINCT FROM OLD.employment_start OR NEW.created_at IS DISTINCT FROM OLD.created_at
     OR (OLD.member_id IS NOT NULL AND NEW.member_id IS DISTINCT FROM OLD.member_id) THEN
    RAISE EXCEPTION 'A person is never deleted, and their start date and login are set once' USING ERRCODE = 'NM120';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER person_guard BEFORE UPDATE OR DELETE ON nemura.person FOR EACH ROW EXECUTE FUNCTION nemura.person_guard();

-- ------------------------------------------------------------------ the job, effective-dated
CREATE TABLE nemura.person_job (
  organisation_id   uuid        NOT NULL DEFAULT nemura.current_org(),
  id                uuid        NOT NULL DEFAULT uuidv7(),
  person_id         uuid        NOT NULL,
  effective_from    date        NOT NULL,
  effective_to      date,
  job_title         text        NOT NULL,
  department        text,
  worksite          text        NOT NULL DEFAULT 'SG',
  employment_type   text        NOT NULL,
  manager_person_id uuid,
  created_at        timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT person_job_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT person_job_person_fkey FOREIGN KEY (organisation_id, person_id) REFERENCES nemura.person (organisation_id, id),
  CONSTRAINT person_job_manager_fkey FOREIGN KEY (organisation_id, manager_person_id) REFERENCES nemura.person (organisation_id, id),
  CONSTRAINT person_job_dates CHECK (effective_to IS NULL OR effective_to > effective_from),
  CONSTRAINT person_job_title_length CHECK (length(btrim(job_title)) BETWEEN 1 AND 200),
  CONSTRAINT person_job_department_length CHECK (department IS NULL OR length(btrim(department)) BETWEEN 1 AND 200),
  CONSTRAINT person_job_worksite_shape CHECK (worksite ~ '^[A-Z]{2}(-[A-Z0-9]{1,8})?$'),
  CONSTRAINT person_job_type_known CHECK (employment_type IN ('full_time', 'part_time', 'contract', 'intern')),
  CONSTRAINT person_job_not_own_manager CHECK (manager_person_id IS DISTINCT FROM person_id)
);
CREATE INDEX person_job_person_idx ON nemura.person_job (organisation_id, person_id, effective_from);

-- A row of history never changes except to be ended, once; and no two rows of one person overlap.
-- +goose StatementBegin
CREATE FUNCTION nemura.person_job_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR (TG_OP = 'UPDATE' AND (NEW.person_id IS DISTINCT FROM OLD.person_id OR NEW.effective_from IS DISTINCT FROM OLD.effective_from
     OR NEW.job_title IS DISTINCT FROM OLD.job_title OR NEW.department IS DISTINCT FROM OLD.department OR NEW.worksite IS DISTINCT FROM OLD.worksite
     OR NEW.employment_type IS DISTINCT FROM OLD.employment_type OR NEW.manager_person_id IS DISTINCT FROM OLD.manager_person_id
     OR (OLD.effective_to IS NOT NULL AND NEW.effective_to IS DISTINCT FROM OLD.effective_to))) THEN
    RAISE EXCEPTION 'A job record is history: it can only be ended, once' USING ERRCODE = 'NM121';
  END IF;
  IF EXISTS (SELECT 1 FROM nemura.person_job j
              WHERE j.organisation_id = NEW.organisation_id AND j.person_id = NEW.person_id AND j.id <> NEW.id
                AND daterange(j.effective_from, coalesce(j.effective_to, 'infinity'), '[)') && daterange(NEW.effective_from, coalesce(NEW.effective_to, 'infinity'), '[)')) THEN
    RAISE EXCEPTION 'A person has one job record at a time' USING ERRCODE = 'NM122';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER person_job_guard BEFORE INSERT OR UPDATE OR DELETE ON nemura.person_job FOR EACH ROW EXECUTE FUNCTION nemura.person_job_guard();

-- ------------------------------------------------------------------ the legal classification, effective-dated (NM-PPL-08)
CREATE TABLE nemura.person_classification (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  person_id       uuid        NOT NULL,
  effective_from  date        NOT NULL,
  effective_to    date,
  contract_type   text        NOT NULL,
  ea_covered      boolean     NOT NULL,
  part_iv_class   text        NOT NULL DEFAULT 'none',
  residency       text        NOT NULL,
  pr_start_date   date,
  birth_date      date,
  created_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT person_classification_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT person_classification_person_fkey FOREIGN KEY (organisation_id, person_id) REFERENCES nemura.person (organisation_id, id),
  CONSTRAINT person_classification_dates CHECK (effective_to IS NULL OR effective_to > effective_from),
  CONSTRAINT person_classification_contract_known CHECK (contract_type IN ('service', 'for_service')),
  CONSTRAINT person_classification_part_iv_known CHECK (part_iv_class IN ('none', 'workman', 'non_workman')),
  CONSTRAINT person_classification_residency_known CHECK (residency IN ('citizen', 'permanent_resident', 'foreigner')),
  -- a contractor gets no statutory employee entitlement: the Employment Act does not cover a contract for service
  CONSTRAINT person_classification_contractor_not_covered CHECK (contract_type = 'service' OR NOT ea_covered),
  CONSTRAINT person_classification_part_iv_needs_cover CHECK (part_iv_class = 'none' OR ea_covered),
  CONSTRAINT person_classification_pr_start_for_pr CHECK ((residency = 'permanent_resident') = (pr_start_date IS NOT NULL))
);
CREATE INDEX person_classification_person_idx ON nemura.person_classification (organisation_id, person_id, effective_from);

-- +goose StatementBegin
CREATE FUNCTION nemura.person_classification_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR (TG_OP = 'UPDATE' AND (NEW.person_id IS DISTINCT FROM OLD.person_id OR NEW.effective_from IS DISTINCT FROM OLD.effective_from
     OR NEW.contract_type IS DISTINCT FROM OLD.contract_type OR NEW.ea_covered IS DISTINCT FROM OLD.ea_covered OR NEW.part_iv_class IS DISTINCT FROM OLD.part_iv_class
     OR NEW.residency IS DISTINCT FROM OLD.residency OR NEW.pr_start_date IS DISTINCT FROM OLD.pr_start_date OR NEW.birth_date IS DISTINCT FROM OLD.birth_date
     OR (OLD.effective_to IS NOT NULL AND NEW.effective_to IS DISTINCT FROM OLD.effective_to))) THEN
    RAISE EXCEPTION 'A classification is history: it can only be ended, once' USING ERRCODE = 'NM123';
  END IF;
  IF EXISTS (SELECT 1 FROM nemura.person_classification c
              WHERE c.organisation_id = NEW.organisation_id AND c.person_id = NEW.person_id AND c.id <> NEW.id
                AND daterange(c.effective_from, coalesce(c.effective_to, 'infinity'), '[)') && daterange(NEW.effective_from, coalesce(NEW.effective_to, 'infinity'), '[)')) THEN
    RAISE EXCEPTION 'A person has one classification at a time' USING ERRCODE = 'NM124';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER person_classification_guard BEFORE INSERT OR UPDATE OR DELETE ON nemura.person_classification
  FOR EACH ROW EXECUTE FUNCTION nemura.person_classification_guard();

-- ------------------------------------------------------------------ audit, row-level security, grants
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.person
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('person', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.person_job
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('person_job', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.person_classification
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('person_classification', 'id');

ALTER TABLE nemura.person ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.person FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.person USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.person_job ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.person_job FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.person_job USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.person_classification ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.person_classification FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.person_classification USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());

GRANT SELECT ON nemura.person, nemura.person_job, nemura.person_classification TO nemura_api, nemura_worker, nemura_readonly;
GRANT INSERT (id, member_id, legal_name, preferred_name, work_email, phone, employment_start, probation_end, leaving_date, leaving_reason),
      UPDATE (member_id, legal_name, preferred_name, work_email, phone, probation_end, leaving_date, leaving_reason) ON nemura.person TO nemura_api;
GRANT INSERT (id, person_id, effective_from, job_title, department, worksite, employment_type, manager_person_id), UPDATE (effective_to) ON nemura.person_job TO nemura_api;
GRANT INSERT (id, person_id, effective_from, contract_type, ea_covered, part_iv_class, residency, pr_start_date, birth_date), UPDATE (effective_to) ON nemura.person_classification TO nemura_api;
