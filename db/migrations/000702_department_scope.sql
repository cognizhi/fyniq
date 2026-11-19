-- Department scopes (PRD-001 FR-ADM-01, rev 0.253 and the owner's decisions of 20 Nov 2026 [owner to confirm]; p1-users-roles
-- tasks 1.2 and 2.1). A role a member holds can be narrowed to values of the department dimension: a view, change or approval
-- Appendix D qualifies by department (the Approver's "dept" cells) then reaches only the records, lines and budgets of those
-- values, and a role with no department scope reaches none of a dept-qualified cell. Entity scopes come with multi-entity (P3).
--
-- This migration is the database's half, restrictive only: it never widens a tenant policy, it ANDs a second one. A person's
-- scope is read from their live role assignments at each statement. They are department-restricted when they hold the
-- Approver role and no role besides Employee: every other role has its own unqualified or differently qualified cells, which
-- the API's policy table decides. A restricted person reaches the journal lines and balances whose dimension set holds one of
-- their scope's values, and, with no scope row, none. The restriction binds the API's role only, for requests that name
-- their person (app.user_id) and are reading (app.dept_reads, set only for an operation that needs the View right, and for the AI,
-- which never writes): a write, an approval or a posting runs the engine on the whole books, since a posting trigger or an
-- approval's envelope that read a filtered cache would corrupt it. The worker and the executor, which act on their own fenced
-- grants (golden rule 5), and migrations and the read-only role stay unfiltered.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- A scope belongs to one assignment of the Approver role: the narrowing is fixed when the role is given, and an assignment is
-- revoked, never changed, so a changed scope is a new assignment. Nothing here is updated or deleted.
CREATE TABLE fyniq.role_assignment_scope (
  organisation_id    uuid        NOT NULL DEFAULT fyniq.current_org(),
  assignment_id      uuid        NOT NULL,
  dimension_value_id uuid        NOT NULL,
  created_at         timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT role_assignment_scope_pkey PRIMARY KEY (organisation_id, assignment_id, dimension_value_id),
  CONSTRAINT role_assignment_scope_assignment_fkey FOREIGN KEY (organisation_id, assignment_id)
    REFERENCES fyniq.role_assignment (organisation_id, id),
  CONSTRAINT role_assignment_scope_value_fkey FOREIGN KEY (organisation_id, dimension_value_id)
    REFERENCES fyniq.dimension_value (organisation_id, id)
);

-- +goose StatementBegin
CREATE FUNCTION fyniq.role_assignment_scope_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  r text;
  k text;
BEGIN
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'a department scope never changes or goes: give the role again with the scope it should have' USING ERRCODE = 'FY700';
  END IF;
  SELECT role INTO r FROM fyniq.role_assignment WHERE organisation_id = NEW.organisation_id AND id = NEW.assignment_id AND revoked_at IS NULL;
  IF r IS DISTINCT FROM 'approver' THEN
    RAISE EXCEPTION 'only a live Approver role is narrowed to departments' USING ERRCODE = 'FY701',
      HINT = 'Appendix D qualifies by department only the Approver''s cells (FR-ADM-01).';
  END IF;
  SELECT d.kind INTO k FROM fyniq.dimension_value v JOIN fyniq.dimension d ON d.organisation_id = v.organisation_id AND d.id = v.dimension_id
    WHERE v.organisation_id = NEW.organisation_id AND v.id = NEW.dimension_value_id;
  IF k IS DISTINCT FROM 'department' THEN
    RAISE EXCEPTION 'a scope names values of the department dimension' USING ERRCODE = 'FY702';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER role_assignment_scope_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.role_assignment_scope
  FOR EACH ROW EXECUTE FUNCTION fyniq.role_assignment_scope_guard();
CREATE TRIGGER role_assignment_scope_no_truncate BEFORE TRUNCATE ON fyniq.role_assignment_scope
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.role_assignment_scope
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('role_assignment_scope', 'assignment_id');

ALTER TABLE fyniq.role_assignment_scope ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.role_assignment_scope FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.role_assignment_scope
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

GRANT SELECT ON fyniq.role_assignment_scope TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (assignment_id, dimension_value_id) ON fyniq.role_assignment_scope TO fyniq_api;

-- The department values a person is restricted to, or NULL when no department restriction applies to them: the request names
-- no person or is no read, or they hold a role besides Approver and Employee, or none of them is Approver. Restricted with no scope row is
-- the empty array: no scope reaches none (the owner's decision of 12 Nov 2026).
-- +goose StatementBegin
CREATE FUNCTION fyniq.dept_scope() RETURNS uuid[]
LANGUAGE plpgsql STABLE
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  who   text := nullif(current_setting('app.user_id', true), '');
  roles text[];
  vals  uuid[];
BEGIN
  IF who IS NULL OR current_setting('app.dept_reads', true) IS DISTINCT FROM 'on' THEN
    RETURN NULL;
  END IF;
  SELECT coalesce(array_agg(DISTINCT ra.role), '{}') INTO roles
    FROM fyniq.role_assignment ra
    JOIN fyniq.user_membership m ON m.organisation_id = ra.organisation_id AND m.id = ra.membership_id
    WHERE m.user_id = who::uuid AND ra.revoked_at IS NULL AND (ra.expires_at IS NULL OR ra.expires_at > now());
  IF NOT ('approver' = ANY(roles)) OR EXISTS (SELECT 1 FROM unnest(roles) r WHERE r NOT IN ('approver', 'employee')) THEN
    RETURN NULL;
  END IF;
  SELECT coalesce(array_agg(DISTINCT s.dimension_value_id), '{}') INTO vals
    FROM fyniq.role_assignment_scope s
    JOIN fyniq.role_assignment ra ON ra.organisation_id = s.organisation_id AND ra.id = s.assignment_id
    JOIN fyniq.user_membership m ON m.organisation_id = ra.organisation_id AND m.id = ra.membership_id
    WHERE m.user_id = who::uuid AND ra.role = 'approver' AND ra.revoked_at IS NULL AND (ra.expires_at IS NULL OR ra.expires_at > now());
  RETURN vals;
END
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION fyniq.dept_scope() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.dept_scope() TO fyniq_api;

-- The second, restrictive policy: a row is read only where the person has no department restriction, or its dimension set
-- holds one of their scope's values. A line or a balance with no dimension set is in no department. It ANDs with the tenant
-- policy and binds the API's role alone.
CREATE POLICY department_scope ON fyniq.journal_line AS RESTRICTIVE FOR SELECT TO fyniq_api
  USING ((SELECT fyniq.dept_scope()) IS NULL
         OR dimension_set_id IN (SELECT ds.id FROM fyniq.dimension_set ds WHERE ds.value_ids && (SELECT fyniq.dept_scope())));
CREATE POLICY department_scope ON fyniq.balance AS RESTRICTIVE FOR SELECT TO fyniq_api
  USING ((SELECT fyniq.dept_scope()) IS NULL
         OR dimension_set_id IN (SELECT ds.id FROM fyniq.dimension_set ds WHERE ds.value_ids && (SELECT fyniq.dept_scope())));
