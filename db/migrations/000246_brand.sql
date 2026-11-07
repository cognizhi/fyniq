-- The organisation's logo and brand (PRD-001 FR-ORG-09; rev 0.200; p1-organisation-chart task 2.4): each change is a
-- new version, never changed after: the logo for light backgrounds, for dark ones and the square icon (each a cleaned SVG
-- or a PNG written again, kept in the blob store under its SHA-256), and the accent colour. The version in force is the
-- latest; a document keeps the version it was issued with.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.brand_version (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  version         integer     NOT NULL,
  light_type      text,
  light_sha256    bytea,
  light_size      integer,
  dark_type       text,
  dark_sha256     bytea,
  dark_size       integer,
  icon_type       text,
  icon_sha256     bytea,
  icon_size       integer,
  accent          text,
  cleaned         jsonb       NOT NULL DEFAULT '{}',
  created_by      uuid        NOT NULL,
  created_at      timestamptz NOT NULL,
  CONSTRAINT brand_version_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT brand_version_number UNIQUE (organisation_id, version),
  CONSTRAINT brand_version_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT brand_version_created_by_fkey FOREIGN KEY (created_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT brand_version_positive CHECK (version > 0),
  CONSTRAINT brand_version_types CHECK (coalesce(light_type, 'image/png') IN ('image/svg+xml', 'image/png')
    AND coalesce(dark_type, 'image/png') IN ('image/svg+xml', 'image/png') AND coalesce(icon_type, 'image/png') IN ('image/svg+xml', 'image/png')),
  CONSTRAINT brand_version_files_whole CHECK ((light_type IS NULL) = (light_sha256 IS NULL) AND (light_sha256 IS NULL) = (light_size IS NULL)
    AND (dark_type IS NULL) = (dark_sha256 IS NULL) AND (dark_sha256 IS NULL) = (dark_size IS NULL)
    AND (icon_type IS NULL) = (icon_sha256 IS NULL) AND (icon_sha256 IS NULL) = (icon_size IS NULL)),
  CONSTRAINT brand_version_hashes CHECK (coalesce(length(light_sha256), 32) = 32 AND coalesce(length(dark_sha256), 32) = 32
    AND coalesce(length(icon_sha256), 32) = 32),
  CONSTRAINT brand_version_sizes CHECK (coalesce(light_size, 1) BETWEEN 1 AND 2097152 AND coalesce(dark_size, 1) BETWEEN 1 AND 2097152
    AND coalesce(icon_size, 1) BETWEEN 1 AND 2097152),
  CONSTRAINT brand_version_accent CHECK (accent IS NULL OR accent ~ '^#[0-9A-F]{6}$'),
  CONSTRAINT brand_version_cleaned_object CHECK (jsonb_typeof(cleaned) = 'object')
);
ALTER TABLE fyniq.brand_version ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.brand_version FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.brand_version
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.brand_version
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('brand_version', 'id');

-- +goose StatementBegin
-- A brand version is never changed or removed, and the next one is numbered after the last (FY282).
CREATE FUNCTION fyniq.brand_version_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'A brand version is kept as it was saved' USING ERRCODE = 'FY282';
  END IF;
  PERFORM pg_advisory_xact_lock(hashtextextended('fyniq.brand:' || NEW.organisation_id::text, 0));
  IF NEW.version <> coalesce((SELECT max(b.version) FROM fyniq.brand_version b WHERE b.organisation_id = NEW.organisation_id), 0) + 1 THEN
    RAISE EXCEPTION 'The brand changed since version %', NEW.version - 1 USING ERRCODE = 'FY283',
      HINT = 'Read the brand again and make the change on what it is now.';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER brand_version_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.brand_version
  FOR EACH ROW EXECUTE FUNCTION fyniq.brand_version_guard();

GRANT SELECT, INSERT ON fyniq.brand_version TO fyniq_api;
GRANT SELECT ON fyniq.brand_version TO fyniq_worker, fyniq_executor, fyniq_readonly;
