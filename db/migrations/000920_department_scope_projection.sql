-- The Approver's department scope, narrowed by the people projection and only narrowed (PRD-001 FR-ADM-01, FR-PPL-06; p1-nemura-fyniq-link design D23; proposed 21 Nov 2026, the owner did not object: needs a
-- human review, as it changes how a right is scoped). projection_department(user) is the one place the projection's word is read: the department value of the person the user was mapped to, while the link is active
-- and the person is not removed; the API's approverScope reads the same function, so the API and the database cannot disagree. dept_scope() then intersects a scope that has values with it.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- +goose StatementBegin
CREATE FUNCTION fyniq.projection_department(p_user uuid) RETURNS uuid
LANGUAGE sql STABLE
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT p.department_value_id FROM fyniq.person_projection p
   WHERE p.user_id = p_user AND NOT p.removed AND p.department_value_id IS NOT NULL
     AND EXISTS (SELECT 1 FROM fyniq.people_link l WHERE l.state = 'active')
   LIMIT 1
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION fyniq.projection_department(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.projection_department(uuid) TO fyniq_api;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.dept_scope() RETURNS uuid[]
LANGUAGE plpgsql STABLE
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  who   text := nullif(current_setting('app.user_id', true), '');
  roles text[];
  vals  uuid[];
  dept  uuid;
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
  -- The people projection only narrows (p1-nemura-fyniq-link design D23, owner to confirm): a scope that has values and a user mapped to a person of the active link whose department is a department value
  -- becomes that one department when it is among them, and nothing when it is not. No scope, no mapping, no department and no active link leave the scope as it is; it never adds a department.
  dept := fyniq.projection_department(who::uuid);
  IF dept IS NOT NULL AND cardinality(vals) > 0 THEN
    vals := CASE WHEN dept = ANY (vals) THEN ARRAY[dept] ELSE '{}'::uuid[] END;
  END IF;
  RETURN vals;
END
$$;
-- +goose StatementEnd
