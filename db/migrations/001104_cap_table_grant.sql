-- An Owner's grant of the full cap table to a Board or investor viewer (PRD-001 FR-EQ-24, owner's decision of 23 Nov 2026; p2-reporting-planning
-- task 3.2). By default a Board viewer reads their own holding only; the full cap table opens only while a grant stands. A grant names the
-- member and the reason, is made by an Owner, and is ended once by an Owner with a reason; it is never changed or deleted otherwise.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.cap_table_grant (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  user_id         uuid        NOT NULL,
  reason          text        NOT NULL,
  granted_by      text        NOT NULL,
  granted_at      timestamptz NOT NULL,
  revoked_by      text,
  revoked_at      timestamptz,
  revoke_reason   text,
  CONSTRAINT cap_table_grant_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT cap_table_grant_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT cap_table_grant_reason CHECK (length(btrim(reason)) BETWEEN 3 AND 500),
  CONSTRAINT cap_table_grant_by_form CHECK (granted_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$' AND (revoked_by IS NULL OR revoked_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')),
  CONSTRAINT cap_table_grant_revoked_form CHECK ((revoked_at IS NULL) = (revoked_by IS NULL) AND (revoked_at IS NULL) = (revoke_reason IS NULL)
    AND (revoke_reason IS NULL OR length(btrim(revoke_reason)) BETWEEN 3 AND 500))
);
-- One live grant per person.
CREATE UNIQUE INDEX cap_table_grant_one_live ON fyniq.cap_table_grant (organisation_id, user_id) WHERE revoked_at IS NULL;

-- +goose StatementBegin
CREATE FUNCTION fyniq.cap_table_grant_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF NEW.revoked_at IS NOT NULL THEN
      RAISE EXCEPTION 'A cap table grant starts live' USING ERRCODE = 'FYR09', HINT = 'Grant it, then revoke it (FR-EQ-24).';
    END IF;
    RETURN NEW;
  END IF;
  IF TG_OP <> 'UPDATE' THEN
    RAISE EXCEPTION 'A cap table grant is never deleted or truncated' USING ERRCODE = 'FYR09', HINT = 'Revoke it instead (FR-EQ-24).';
  END IF;
  IF OLD.revoked_at IS NOT NULL OR NEW.revoked_at IS NULL
     OR (NEW.id, NEW.organisation_id, NEW.user_id, NEW.reason, NEW.granted_by, NEW.granted_at) IS DISTINCT FROM (OLD.id, OLD.organisation_id, OLD.user_id, OLD.reason, OLD.granted_by, OLD.granted_at) THEN
    RAISE EXCEPTION 'A cap table grant changes only by being revoked, once' USING ERRCODE = 'FYR09', HINT = 'Grant it again to open the cap table again (FR-EQ-24).';
  END IF;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER cap_table_grant_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.cap_table_grant FOR EACH ROW EXECUTE FUNCTION fyniq.cap_table_grant_guard();
CREATE TRIGGER cap_table_grant_no_truncate BEFORE TRUNCATE ON fyniq.cap_table_grant FOR EACH STATEMENT EXECUTE FUNCTION fyniq.cap_table_grant_guard();

ALTER TABLE fyniq.cap_table_grant ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.cap_table_grant FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.cap_table_grant USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.cap_table_grant
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('cap_table_grant', 'id');

REVOKE ALL ON fyniq.cap_table_grant FROM PUBLIC;
GRANT SELECT ON fyniq.cap_table_grant TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (user_id, reason, granted_by, granted_at) ON fyniq.cap_table_grant TO fyniq_api;
GRANT UPDATE (revoked_by, revoked_at, revoke_reason) ON fyniq.cap_table_grant TO fyniq_api;
