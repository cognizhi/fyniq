-- Nemura's members, their roles and dual control (PRD-002 NM-ROLE-01, NM-ROLE-04, NM-ROLE-05, NM-SEC-02; PRD-001 SOD-6).
--
-- A member is a person in an organisation. Roles are Nemura's own (Appendix B), assigned per organisation and dated: a grant
-- takes effect from its date and ends on its end date, and history is kept. The roles that carry pay or signing rights, Payroll
-- administrator and Owner, are never granted on one person's say: they need an approval from an Owner who is neither the one who
-- asked nor the one who receives it, and the database refuses a grant without it. Nobody approves their own request, change or
-- role. A role in Nemura never grants a right in Fyniq and the reverse: no table here refers to Fyniq's.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- ------------------------------------------------------------------ tenant: members
CREATE TABLE nemura.member (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  display_name    text        NOT NULL,
  created_at      timestamptz NOT NULL DEFAULT now(),
  deactivated_at  timestamptz,
  CONSTRAINT member_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT member_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES nemura.organisation (organisation_id),
  CONSTRAINT member_display_name_length CHECK (length(btrim(display_name)) BETWEEN 1 AND 200)
);

-- ------------------------------------------------------------------ tenant: approvals (dual control)
-- The version and the digest of what is approved are recorded from the start, so the approval's envelope that the other app can
-- verify (NM-INT-06) is a format on top of this table, not a migration.
CREATE TABLE nemura.approval_request (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  object_type     text        NOT NULL,
  object_id       uuid        NOT NULL,
  version         integer     NOT NULL DEFAULT 1,
  digest          bytea       NOT NULL,
  requested_by    uuid        NOT NULL,
  required_role   text        NOT NULL,
  state           text        NOT NULL DEFAULT 'pending',
  decided_by      uuid,
  decided_at      timestamptz,
  reason          text,
  expires_at      timestamptz NOT NULL,
  created_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT approval_request_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT approval_request_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES nemura.organisation (organisation_id),
  CONSTRAINT approval_request_requested_by_fkey FOREIGN KEY (organisation_id, requested_by)
    REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT approval_request_decided_by_fkey FOREIGN KEY (organisation_id, decided_by)
    REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT approval_request_object_type_format CHECK (object_type ~ '^[a-z][a-z0-9_]*$' AND length(object_type) <= 60),
  CONSTRAINT approval_request_version_positive CHECK (version > 0),
  CONSTRAINT approval_request_digest_sha256 CHECK (length(digest) = 32),
  CONSTRAINT approval_request_required_role_known CHECK (required_role IN ('owner', 'hr_admin', 'payroll_admin')),
  CONSTRAINT approval_request_state_known CHECK (state IN ('pending', 'approved', 'refused', 'expired')),
  -- a decision names who made it, and when; a pending or expired request names no one
  CONSTRAINT approval_request_decision_whole CHECK ((state IN ('approved', 'refused')) = (decided_by IS NOT NULL AND decided_at IS NOT NULL)),
  -- nobody approves their own request (NM-ROLE-05)
  CONSTRAINT approval_request_not_self CHECK (decided_by IS NULL OR decided_by <> requested_by),
  -- a refusal says why
  CONSTRAINT approval_request_refusal_reason CHECK (state <> 'refused' OR length(btrim(coalesce(reason, ''))) > 0),
  CONSTRAINT approval_request_reason_length CHECK (length(reason) <= 2000)
);
CREATE INDEX approval_request_object_idx ON nemura.approval_request (organisation_id, object_type, object_id);

-- ------------------------------------------------------------------ tenant: role assignments (effective-dated)
CREATE TABLE nemura.role_assignment (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  member_id       uuid        NOT NULL,
  role            text        NOT NULL,
  effective_from  date        NOT NULL,
  effective_to    date,
  granted_by      uuid,                    -- null only for the first Owner, made by first-run setup
  approval_id     uuid,
  created_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT role_assignment_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT role_assignment_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES nemura.organisation (organisation_id),
  CONSTRAINT role_assignment_member_fkey FOREIGN KEY (organisation_id, member_id)
    REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT role_assignment_granted_by_fkey FOREIGN KEY (organisation_id, granted_by)
    REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT role_assignment_approval_fkey FOREIGN KEY (organisation_id, approval_id)
    REFERENCES nemura.approval_request (organisation_id, id),
  CONSTRAINT role_assignment_role_known CHECK (role IN ('employee', 'line_manager', 'hr_officer', 'hr_admin', 'payroll_admin',
                                                         'recruiter', 'interviewer', 'administrator', 'owner')),
  CONSTRAINT role_assignment_dates CHECK (effective_to IS NULL OR effective_to > effective_from),
  CONSTRAINT role_assignment_once UNIQUE (organisation_id, member_id, role, effective_from),
  -- nobody gives themselves a role (NM-ROLE-05); only the first Owner has no granter
  CONSTRAINT role_assignment_not_self CHECK (granted_by IS NULL OR granted_by <> member_id),
  CONSTRAINT role_assignment_setup_is_owner CHECK (granted_by IS NOT NULL OR role = 'owner'),
  -- pay and signing rights need an approval (NM-ROLE-05)
  CONSTRAINT role_assignment_privileged_approved CHECK (role NOT IN ('payroll_admin', 'owner') OR granted_by IS NULL OR approval_id IS NOT NULL)
);
CREATE INDEX role_assignment_member_idx ON nemura.role_assignment (organisation_id, member_id, effective_from);

-- A grant of pay or signing rights stands only on an approved request of this very grant: for this member, decided by an Owner
-- (in force on the day) who is not the one who asked, not the one who receives it, and not the one who grants.
-- +goose StatementBegin
CREATE FUNCTION nemura.role_assignment_check() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  -- Only first-run setup grants with no granter, and only the organisation's first Owner.
  IF NEW.granted_by IS NULL AND EXISTS (SELECT 1 FROM nemura.role_assignment o WHERE o.organisation_id = NEW.organisation_id AND o.role = 'owner') THEN
    RAISE EXCEPTION 'The organisation already has an Owner: another Owner is granted by an Owner and approved by a second Owner'
      USING ERRCODE = 'NM080', HINT = 'First-run setup makes the first Owner, once (NM-ROLE-05).';
  END IF;
  IF NEW.role IN ('payroll_admin', 'owner') AND NEW.granted_by IS NOT NULL THEN
    IF NOT EXISTS (
      SELECT 1 FROM nemura.approval_request a
       WHERE a.organisation_id = NEW.organisation_id AND a.id = NEW.approval_id
         AND a.state = 'approved' AND a.object_type = 'role_assignment' AND a.object_id = NEW.member_id
         AND a.decided_by <> NEW.member_id AND a.decided_by <> NEW.granted_by AND a.requested_by <> NEW.member_id
         AND EXISTS (SELECT 1 FROM nemura.role_assignment o
                      WHERE o.organisation_id = a.organisation_id AND o.member_id = a.decided_by AND o.role = 'owner'
                        AND o.effective_from <= NEW.effective_from AND (o.effective_to IS NULL OR o.effective_to > NEW.effective_from))) THEN
      RAISE EXCEPTION 'The % role needs an approval from an Owner who is not the one asking, granting or receiving it', NEW.role
        USING ERRCODE = 'NM080', HINT = 'Ask for the approval first (NM-ROLE-05).';
    END IF;
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER role_assignment_check BEFORE INSERT ON nemura.role_assignment
  FOR EACH ROW EXECUTE FUNCTION nemura.role_assignment_check();

-- A grant is history: only its end date may be set, once, and nothing is deleted.
-- +goose StatementBegin
CREATE FUNCTION nemura.role_assignment_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR NEW.member_id IS DISTINCT FROM OLD.member_id OR NEW.role IS DISTINCT FROM OLD.role
     OR NEW.effective_from IS DISTINCT FROM OLD.effective_from OR NEW.granted_by IS DISTINCT FROM OLD.granted_by
     OR NEW.approval_id IS DISTINCT FROM OLD.approval_id OR (OLD.effective_to IS NOT NULL AND NEW.effective_to IS DISTINCT FROM OLD.effective_to) THEN
    RAISE EXCEPTION 'A role assignment is history: it can only be ended, once' USING ERRCODE = 'NM081';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER role_assignment_guard BEFORE UPDATE OR DELETE ON nemura.role_assignment
  FOR EACH ROW EXECUTE FUNCTION nemura.role_assignment_guard();

-- A request's decision is made once, by someone else, and never changed; nothing about what it asks changes.
-- +goose StatementBegin
CREATE FUNCTION nemura.approval_request_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR NEW.object_type IS DISTINCT FROM OLD.object_type OR NEW.object_id IS DISTINCT FROM OLD.object_id
     OR NEW.version IS DISTINCT FROM OLD.version OR NEW.digest IS DISTINCT FROM OLD.digest
     OR NEW.requested_by IS DISTINCT FROM OLD.requested_by OR NEW.required_role IS DISTINCT FROM OLD.required_role
     OR NEW.expires_at IS DISTINCT FROM OLD.expires_at OR OLD.state <> 'pending' THEN
    RAISE EXCEPTION 'An approval request is decided once and never changed' USING ERRCODE = 'NM082';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER approval_request_guard BEFORE UPDATE OR DELETE ON nemura.approval_request
  FOR EACH ROW EXECUTE FUNCTION nemura.approval_request_guard();

-- Nothing here is deleted: a member is deactivated, a grant ended, a request decided.
-- +goose StatementBegin
CREATE FUNCTION nemura.members_no_delete() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'Members are deactivated, not deleted' USING ERRCODE = 'NM081';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER member_no_delete BEFORE DELETE ON nemura.member
  FOR EACH ROW EXECUTE FUNCTION nemura.members_no_delete();

-- ------------------------------------------------------------------ AUD-08: no change without its event
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.member
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('member', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.role_assignment
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('role_assignment', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.approval_request
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('approval_request', 'id');

-- ------------------------------------------------------------------ row-level security
ALTER TABLE nemura.member ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.member FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.member
  USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.approval_request ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.approval_request FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.approval_request
  USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.role_assignment ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.role_assignment FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.role_assignment
  USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());

-- ------------------------------------------------------------------ grants (db/README.md)
GRANT SELECT ON nemura.member, nemura.role_assignment, nemura.approval_request TO nemura_api, nemura_worker, nemura_executor, nemura_readonly;
GRANT INSERT (id, display_name), UPDATE (deactivated_at) ON nemura.member TO nemura_api;
GRANT INSERT (id, member_id, role, effective_from, granted_by, approval_id), UPDATE (effective_to)
  ON nemura.role_assignment TO nemura_api;
GRANT INSERT (id, object_type, object_id, version, digest, requested_by, required_role, expires_at),
      UPDATE (state, decided_by, decided_at, reason)
  ON nemura.approval_request TO nemura_api;
