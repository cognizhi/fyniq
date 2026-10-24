-- Announcement banners (PRD-001 FR-NTF-03, rev 0.112; p1-templates-email task 3.2): the Owner or Administrator
-- publishes a banner to their organisation, a Super admin to everyone; each has a message and a link, a severity, an
-- audience (everyone, or roles), a start and an end, and whether it can be dismissed (never a critical one). A banner
-- never changes but by being withdrawn early, once (FY190); a person's dismissal is kept on the server, never changes
-- (FY191), and is refused for a banner that cannot be dismissed (FY192). The installation's banners are a global table,
-- published and withdrawn only by an active Super admin through SECURITY DEFINER functions (FY085 otherwise).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.announcement (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  message         text        NOT NULL,
  link            text,                  -- a path in the web app, or an https address
  severity        text        NOT NULL,
  roles           text[]      NOT NULL DEFAULT '{}',  -- empty: everyone
  starts_at       timestamptz NOT NULL,
  ends_at         timestamptz NOT NULL,
  dismissible     boolean     NOT NULL,
  published_by    text        NOT NULL,
  published_at    timestamptz NOT NULL,
  withdrawn_at    timestamptz,
  withdrawn_by    text,
  CONSTRAINT announcement_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT announcement_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT announcement_message_length CHECK (length(message) BETWEEN 1 AND 300),
  CONSTRAINT announcement_link_form CHECK (link ~ '^(/[A-Za-z0-9/_?=&.#-]*|https://[!-~]+)$' AND length(link) <= 300),
  CONSTRAINT announcement_severity_known CHECK (severity IN ('info', 'warning', 'critical')),
  CONSTRAINT announcement_roles_form CHECK (cardinality(roles) <= 20 AND array_to_string(roles, ',') ~ '^([a-z_]{1,40}(,[a-z_]{1,40})*)?$'),
  CONSTRAINT announcement_window CHECK (ends_at > starts_at),
  CONSTRAINT announcement_critical_stays CHECK (severity <> 'critical' OR NOT dismissible),
  CONSTRAINT announcement_by_format CHECK (published_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$'
    AND withdrawn_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$'),
  CONSTRAINT announcement_withdrawn_whole CHECK ((withdrawn_at IS NULL) = (withdrawn_by IS NULL))
);
CREATE INDEX announcement_ends ON fyniq.announcement (organisation_id, ends_at);

CREATE TABLE fyniq.installation_announcement (
  id           uuid        NOT NULL DEFAULT uuidv7(),
  message      text        NOT NULL,
  link         text,
  severity     text        NOT NULL,
  roles        text[]      NOT NULL DEFAULT '{}',
  starts_at    timestamptz NOT NULL,
  ends_at      timestamptz NOT NULL,
  dismissible  boolean     NOT NULL,
  published_by uuid        NOT NULL,  -- the Super admin
  published_at timestamptz NOT NULL,
  withdrawn_at timestamptz,
  withdrawn_by uuid,
  CONSTRAINT installation_announcement_pkey PRIMARY KEY (id),
  CONSTRAINT installation_announcement_by_fkey FOREIGN KEY (published_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT installation_announcement_withdrawn_by_fkey FOREIGN KEY (withdrawn_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT installation_announcement_message_length CHECK (length(message) BETWEEN 1 AND 300),
  CONSTRAINT installation_announcement_link_form CHECK (link ~ '^(/[A-Za-z0-9/_?=&.#-]*|https://[!-~]+)$' AND length(link) <= 300),
  CONSTRAINT installation_announcement_severity_known CHECK (severity IN ('info', 'warning', 'critical')),
  CONSTRAINT installation_announcement_roles_form CHECK (cardinality(roles) <= 20
    AND array_to_string(roles, ',') ~ '^([a-z_]{1,40}(,[a-z_]{1,40})*)?$'),
  CONSTRAINT installation_announcement_window CHECK (ends_at > starts_at),
  CONSTRAINT installation_announcement_critical_stays CHECK (severity <> 'critical' OR NOT dismissible),
  CONSTRAINT installation_announcement_withdrawn_whole CHECK ((withdrawn_at IS NULL) = (withdrawn_by IS NULL))
);
REVOKE ALL ON fyniq.installation_announcement FROM PUBLIC;

CREATE TABLE fyniq.announcement_dismissal (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  announcement_id uuid        NOT NULL,
  installation    boolean     NOT NULL,  -- the installation's banner, not the organisation's
  user_id         uuid        NOT NULL,
  dismissed_at    timestamptz NOT NULL,
  CONSTRAINT announcement_dismissal_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT announcement_dismissal_member_fkey FOREIGN KEY (organisation_id, user_id)
    REFERENCES fyniq.user_membership (organisation_id, user_id),
  CONSTRAINT announcement_dismissal_once UNIQUE (organisation_id, user_id, announcement_id)
);

-- A banner changes only in being withdrawn, once (FY190); a dismissal never changes or goes (FY191) and is only of a
-- banner that can be dismissed (FY192).
-- +goose StatementBegin
CREATE FUNCTION fyniq.announcement_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR OLD.withdrawn_at IS NOT NULL OR NEW.withdrawn_at IS NULL
     OR (NEW.message, NEW.link, NEW.severity, NEW.roles, NEW.starts_at, NEW.ends_at, NEW.dismissible, NEW.published_by, NEW.published_at)
        IS DISTINCT FROM (OLD.message, OLD.link, OLD.severity, OLD.roles, OLD.starts_at, OLD.ends_at, OLD.dismissible, OLD.published_by,
                          OLD.published_at) THEN
    RAISE EXCEPTION 'A banner changes only in being withdrawn, once' USING ERRCODE = 'FY190',
      HINT = 'Withdraw it and publish another (FR-NTF-03).';
  END IF;
  RETURN NEW;
END
$$;

CREATE FUNCTION fyniq.announcement_dismissal_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'A dismissal never changes or goes' USING ERRCODE = 'FY191', HINT = 'It is kept for its person (FR-NTF-03).';
END
$$;

-- Reads the installation's banners, a global table, so it runs as its owner.
CREATE FUNCTION fyniq.announcement_dismissible() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF (NOT NEW.installation AND NOT EXISTS (SELECT 1 FROM fyniq.announcement a
                                            WHERE a.organisation_id = NEW.organisation_id AND a.id = NEW.announcement_id AND a.dismissible))
     OR (NEW.installation AND NOT EXISTS (SELECT 1 FROM fyniq.installation_announcement a WHERE a.id = NEW.announcement_id AND a.dismissible)) THEN
    RAISE EXCEPTION 'Banner % cannot be dismissed', NEW.announcement_id USING ERRCODE = 'FY192',
      HINT = 'A critical banner, or one published as not dismissible, stays until it ends (FR-NTF-03).';
  END IF;
  RETURN NEW;
END
$$;

-- An active Super admin publishes a banner to everyone; it returns its ID.
CREATE FUNCTION fyniq.publish_installation_announcement(p_by uuid, p_message text, p_link text, p_severity text, p_roles text[],
  p_starts timestamptz, p_ends timestamptz, p_dismissible boolean, p_at timestamptz) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  out uuid;
BEGIN
  PERFORM fyniq.require_super_admin(p_by);
  INSERT INTO fyniq.installation_announcement (message, link, severity, roles, starts_at, ends_at, dismissible, published_by, published_at)
  VALUES (p_message, p_link, p_severity, coalesce(p_roles, '{}'), p_starts, p_ends, p_dismissible, p_by, p_at)
  RETURNING id INTO out;
  RETURN out;
END
$$;

-- An active Super admin withdraws one of the installation's banners, once.
CREATE FUNCTION fyniq.withdraw_installation_announcement(p_by uuid, p_id uuid, p_at timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  PERFORM fyniq.require_super_admin(p_by);
  UPDATE fyniq.installation_announcement SET withdrawn_at = p_at, withdrawn_by = p_by WHERE id = p_id;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'No banner %', p_id USING ERRCODE = 'P0002';
  END IF;
END
$$;

-- The installation's banners, newest first, for everyone's banners and the Super admin console.
CREATE FUNCTION fyniq.installation_announcements() RETURNS TABLE (id uuid, message text, link text, severity text, roles text[],
  starts_at timestamptz, ends_at timestamptz, dismissible boolean, published_by uuid, published_at timestamptz, withdrawn_at timestamptz,
  withdrawn_by uuid)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT a.id, a.message, a.link, a.severity, a.roles, a.starts_at, a.ends_at, a.dismissible, a.published_by, a.published_at,
         a.withdrawn_at, a.withdrawn_by
    FROM fyniq.installation_announcement a ORDER BY a.published_at DESC, a.id DESC
$$;
-- +goose StatementEnd
CREATE TRIGGER announcement_guard BEFORE UPDATE OR DELETE ON fyniq.announcement
  FOR EACH ROW EXECUTE FUNCTION fyniq.announcement_guard();
CREATE TRIGGER installation_announcement_guard BEFORE UPDATE OR DELETE ON fyniq.installation_announcement
  FOR EACH ROW EXECUTE FUNCTION fyniq.announcement_guard();
CREATE TRIGGER announcement_dismissal_guard BEFORE UPDATE OR DELETE ON fyniq.announcement_dismissal
  FOR EACH ROW EXECUTE FUNCTION fyniq.announcement_dismissal_guard();
CREATE TRIGGER announcement_dismissible BEFORE INSERT ON fyniq.announcement_dismissal
  FOR EACH ROW EXECUTE FUNCTION fyniq.announcement_dismissible();
CREATE TRIGGER announcement_no_truncate BEFORE TRUNCATE ON fyniq.announcement
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();
CREATE TRIGGER installation_announcement_no_truncate BEFORE TRUNCATE ON fyniq.installation_announcement
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();
CREATE TRIGGER announcement_dismissal_no_truncate BEFORE TRUNCATE ON fyniq.announcement_dismissal
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();

ALTER TABLE fyniq.announcement ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.announcement FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.announcement
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.announcement_dismissal ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.announcement_dismissal FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.announcement_dismissal
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

-- Publishing and withdrawing are audited (the installation's in the installation organisation's trail, by the API).
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.announcement
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('announcement', 'id');

GRANT SELECT ON fyniq.announcement, fyniq.announcement_dismissal TO fyniq_api, fyniq_readonly;
GRANT INSERT (message, link, severity, roles, starts_at, ends_at, dismissible, published_by, published_at) ON fyniq.announcement TO fyniq_api;
GRANT UPDATE (withdrawn_at, withdrawn_by) ON fyniq.announcement TO fyniq_api;
GRANT INSERT (announcement_id, installation, user_id, dismissed_at) ON fyniq.announcement_dismissal TO fyniq_api;
REVOKE ALL ON FUNCTION fyniq.publish_installation_announcement(uuid, text, text, text, text[], timestamptz, timestamptz, boolean, timestamptz)
  FROM PUBLIC;
REVOKE ALL ON FUNCTION fyniq.withdraw_installation_announcement(uuid, uuid, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION fyniq.installation_announcements() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.publish_installation_announcement(uuid, text, text, text, text[], timestamptz, timestamptz, boolean, timestamptz)
  TO fyniq_api;
GRANT EXECUTE ON FUNCTION fyniq.withdraw_installation_announcement(uuid, uuid, timestamptz) TO fyniq_api;
GRANT EXECUTE ON FUNCTION fyniq.installation_announcements() TO fyniq_api;
