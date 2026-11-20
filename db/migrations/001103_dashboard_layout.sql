-- Configurable dashboards (PRD-001 FR-VIZ-09, FR-KPI-05, rev 0.540; p2-reporting-planning task 4.1, design D9).
--
-- A layout is the tiles a role or a person arranged: a list of KPI keys placed on a grid, with no figure in it. A role's layout (set by an
-- Owner or Administrator) is shown to people holding that role who have arranged none of their own; a person's own wins. A layout changes
-- only as its next version and is never deleted: clearing one saves an empty list, which means "use the role's, or the default".

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.dashboard_layout (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  scope           text        NOT NULL,
  role            text,                    -- the role, for a role's layout
  user_id         uuid,                    -- the person, for their own
  tiles           jsonb       NOT NULL,
  version         integer     NOT NULL DEFAULT 1,
  updated_by      text        NOT NULL,
  updated_at      timestamptz NOT NULL,
  CONSTRAINT dashboard_layout_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT dashboard_layout_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT dashboard_layout_scope CHECK ((scope = 'role' AND role IS NOT NULL AND user_id IS NULL) OR (scope = 'user' AND user_id IS NOT NULL AND role IS NULL)),
  CONSTRAINT dashboard_layout_role_known CHECK (role IS NULL OR role ~ '^[a-z_]{3,40}$'),
  CONSTRAINT dashboard_layout_tiles CHECK (jsonb_typeof(tiles) = 'array' AND jsonb_array_length(tiles) <= 24),
  CONSTRAINT dashboard_layout_by_form CHECK (updated_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$'),
  CONSTRAINT dashboard_layout_version CHECK (version >= 1)
);
-- One layout per role, and one per person.
CREATE UNIQUE INDEX dashboard_layout_role_once ON fyniq.dashboard_layout (organisation_id, role) WHERE scope = 'role';
CREATE UNIQUE INDEX dashboard_layout_user_once ON fyniq.dashboard_layout (organisation_id, user_id) WHERE scope = 'user';

-- +goose StatementBegin
CREATE FUNCTION fyniq.dashboard_layout_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF NEW.version <> 1 THEN
      RAISE EXCEPTION 'A dashboard layout is written at version 1' USING ERRCODE = 'FYR08', HINT = 'Save it; a change is its next version (FR-VIZ-09).';
    END IF;
    RETURN NEW;
  END IF;
  IF TG_OP <> 'UPDATE' THEN
    RAISE EXCEPTION 'A dashboard layout is never deleted or truncated' USING ERRCODE = 'FYR08', HINT = 'Save an empty layout to clear it (FR-VIZ-09).';
  END IF;
  IF NEW.id IS DISTINCT FROM OLD.id OR NEW.organisation_id IS DISTINCT FROM OLD.organisation_id OR NEW.scope IS DISTINCT FROM OLD.scope
     OR NEW.role IS DISTINCT FROM OLD.role OR NEW.user_id IS DISTINCT FROM OLD.user_id OR NEW.version <> OLD.version + 1 OR NEW.updated_at < OLD.updated_at THEN
    RAISE EXCEPTION 'A dashboard layout changes only as its next version, and never its role or person' USING ERRCODE = 'FYR08',
      HINT = 'Read the layout and save the change as its next version (FR-VIZ-09).';
  END IF;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER dashboard_layout_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.dashboard_layout FOR EACH ROW EXECUTE FUNCTION fyniq.dashboard_layout_guard();
CREATE TRIGGER dashboard_layout_no_truncate BEFORE TRUNCATE ON fyniq.dashboard_layout FOR EACH STATEMENT EXECUTE FUNCTION fyniq.dashboard_layout_guard();

ALTER TABLE fyniq.dashboard_layout ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.dashboard_layout FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.dashboard_layout USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.dashboard_layout
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('dashboard_layout', 'id');

REVOKE ALL ON fyniq.dashboard_layout FROM PUBLIC;
GRANT SELECT ON fyniq.dashboard_layout TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (scope, role, user_id, tiles, updated_by, updated_at) ON fyniq.dashboard_layout TO fyniq_api;
GRANT UPDATE (tiles, updated_by, updated_at, version) ON fyniq.dashboard_layout TO fyniq_api;
