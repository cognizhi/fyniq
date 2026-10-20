-- Team modes and segregation-of-duties overrides (PRD-001 §6.3, §6.4, FR-ADM-04, rev 0.42; p1-approval-engine task
-- 3.1, p1-users-roles task 1.3).
--
-- The organisation's team mode is versioned, approval-gated data: Founder mode until the first switch, which an
-- Administrator asks for and an Owner approves (dual control, "Users, roles, team mode" in Appendix D). A version
-- exists only with the approved team_mode.change request that holds exactly its mode and number, as the next
-- version; none is ever changed or deleted (FY108). A switch voids every pending approval request of the
-- organisation: each was asked for under the old mode, which its envelope records.
--
-- In Standard and Enterprise mode a conflict of §6.3 is blocked unless it is overridden with a reason and a second
-- approver, and reported. An override is kept for one version of the team mode, so a later switch lists the conflict
-- again. It exists only with the approved request that holds exactly it: the switch that asked for it, or the role
-- change that made the conflict (FY109); it is never changed or deleted.
--
-- The request table's object check is widened NOT VALID and validated by 000044 (squawk: constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.team_mode (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  version         integer     NOT NULL,
  mode            text        NOT NULL,
  request_id      uuid        NOT NULL,
  created_by      uuid        NOT NULL,
  created_at      timestamptz NOT NULL,
  CONSTRAINT team_mode_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT team_mode_version_once UNIQUE (organisation_id, version),
  CONSTRAINT team_mode_request_once UNIQUE (organisation_id, request_id),
  CONSTRAINT team_mode_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT team_mode_request_fkey FOREIGN KEY (organisation_id, request_id)
    REFERENCES fyniq.approval_request (organisation_id, id),
  CONSTRAINT team_mode_created_by_fkey FOREIGN KEY (created_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT team_mode_mode_known CHECK (mode IN ('founder', 'standard', 'enterprise')),
  CONSTRAINT team_mode_version_range CHECK (version BETWEEN 1 AND 1000000)
);

-- A version is added once, as the next, by the approved request that holds exactly it; it never changes.
-- +goose StatementBegin
CREATE FUNCTION fyniq.team_mode_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  r      fyniq.approval_request;
  latest integer;
BEGIN
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'A version of the team mode is never changed or deleted' USING ERRCODE = 'FY108',
      HINT = 'Switch the team mode: the switch is approved like any change (§6.4).';
  END IF;
  SELECT * INTO r FROM fyniq.approval_request q WHERE q.organisation_id = NEW.organisation_id AND q.id = NEW.request_id;
  IF r.status IS DISTINCT FROM 'approved' OR r.object_type <> 'team_mode' OR r.operation <> 'team_mode.change'
     OR r.requested_by <> NEW.created_by
     OR (r.change -> 'execute' ->> 'mode') IS DISTINCT FROM NEW.mode
     OR (r.change -> 'execute' ->> 'version') IS DISTINCT FROM NEW.version::text THEN
    RAISE EXCEPTION 'A version of the team mode exists only with the approved request that holds exactly it'
      USING ERRCODE = 'FY108', HINT = 'Ask for the switch and have another member approve it (§6.4, FR-ADM-04).';
  END IF;
  SELECT max(m.version) INTO latest FROM fyniq.team_mode m WHERE m.organisation_id = NEW.organisation_id;
  IF NEW.version <> coalesce(latest, 0) + 1 THEN
    RAISE EXCEPTION 'The next version of the team mode is %, not %', coalesce(latest, 0) + 1, NEW.version
      USING ERRCODE = 'FY108', HINT = 'Read the team mode again and ask for the switch from its latest version.';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER team_mode_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.team_mode
  FOR EACH ROW EXECUTE FUNCTION fyniq.team_mode_guard();

-- A switch voids every pending request of the organisation; the unit of work that added the version audits the voids
-- (require_audit). It runs as the function's owner, like approval_void_pending_type.
-- +goose StatementBegin
CREATE FUNCTION fyniq.approval_void_pending_all(p_reason text) RETURNS void
LANGUAGE sql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  UPDATE fyniq.approval_request r
     SET status = 'void', decided_at = now(), status_reason = p_reason, version = r.version + 1,
         voided_xact = pg_current_xact_id()::text::bigint
   WHERE r.organisation_id = fyniq.current_org() AND r.status = 'pending';
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION fyniq.team_mode_voids() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  PERFORM fyniq.approval_void_pending_all('the team mode changed after the request');
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER team_mode_voids AFTER INSERT ON fyniq.team_mode
  FOR EACH ROW EXECUTE FUNCTION fyniq.team_mode_voids();

ALTER TABLE fyniq.team_mode ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.team_mode FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.team_mode
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.team_mode
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('team_mode', 'id');

-- ------------------------------------------------------------------ overrides of conflicts

CREATE TABLE fyniq.sod_override (
  organisation_id   uuid        NOT NULL DEFAULT fyniq.current_org(),
  id                uuid        NOT NULL DEFAULT uuidv7(),
  team_mode_version integer     NOT NULL,
  user_id           uuid        NOT NULL,
  conflict          text        NOT NULL,
  reason            text        NOT NULL,
  request_id        uuid        NOT NULL,
  created_by        uuid        NOT NULL,
  created_at        timestamptz NOT NULL,
  CONSTRAINT sod_override_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT sod_override_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT sod_override_team_mode_fkey FOREIGN KEY (organisation_id, team_mode_version)
    REFERENCES fyniq.team_mode (organisation_id, version),
  CONSTRAINT sod_override_request_fkey FOREIGN KEY (organisation_id, request_id)
    REFERENCES fyniq.approval_request (organisation_id, id),
  CONSTRAINT sod_override_user_fkey FOREIGN KEY (user_id) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT sod_override_created_by_fkey FOREIGN KEY (created_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT sod_override_conflict_known CHECK (conflict ~ '^SOD-([1-9]|10)$'),
  CONSTRAINT sod_override_reason_length CHECK (length(btrim(reason)) BETWEEN 1 AND 500),
  CONSTRAINT sod_override_once UNIQUE (organisation_id, request_id, user_id, conflict)
);

CREATE INDEX sod_override_lookup ON fyniq.sod_override (organisation_id, team_mode_version, user_id, conflict);

-- An override is added for the team mode in force, by the approved request that holds exactly it: the switch to that
-- version, or a role change made under it; it never changes.
-- +goose StatementBegin
CREATE FUNCTION fyniq.sod_override_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  r      fyniq.approval_request;
  latest integer;
  held   jsonb;
BEGIN
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'An override of a conflict is never changed or deleted' USING ERRCODE = 'FY109',
      HINT = 'A switch of the team mode starts again from the conflicts it lists (§6.4).';
  END IF;
  SELECT max(m.version) INTO latest FROM fyniq.team_mode m WHERE m.organisation_id = NEW.organisation_id;
  SELECT * INTO r FROM fyniq.approval_request q WHERE q.organisation_id = NEW.organisation_id AND q.id = NEW.request_id;
  held := jsonb_build_object('user_id', NEW.user_id::text, 'conflict', NEW.conflict, 'reason', NEW.reason);
  IF NEW.team_mode_version IS DISTINCT FROM latest
     OR r.status IS DISTINCT FROM 'approved' OR r.requested_by <> NEW.created_by
     OR NOT ((r.operation = 'team_mode.change' AND (r.change -> 'execute' ->> 'version') = NEW.team_mode_version::text
              AND (r.change -> 'execute' -> 'overrides') @> jsonb_build_array(held))
          OR (r.operation = 'role.assign' AND (r.change -> 'execute' -> 'sod_override') = held)) THEN
    RAISE EXCEPTION 'An override of a conflict exists only for the team mode in force, with the approved request that holds exactly it'
      USING ERRCODE = 'FY109', HINT = 'Ask for the override with its reason and have another member approve it (§6.4).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER sod_override_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.sod_override
  FOR EACH ROW EXECUTE FUNCTION fyniq.sod_override_guard();

ALTER TABLE fyniq.sod_override ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.sod_override FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.sod_override
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.sod_override
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('sod_override', 'id');

-- Only the API adds a version or an override, when it executes an approved change; every role reads. Nobody changes
-- or deletes.
GRANT SELECT ON fyniq.team_mode, fyniq.sod_override TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
GRANT INSERT (version, mode, request_id, created_by, created_at) ON fyniq.team_mode TO fyniq_api;
GRANT INSERT (team_mode_version, user_id, conflict, reason, request_id, created_by, created_at) ON fyniq.sod_override TO fyniq_api;
REVOKE ALL ON FUNCTION fyniq.approval_void_pending_all(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.approval_void_pending_all(text) TO fyniq_api;

-- ------------------------------------------------------------------ the engine knows the switch

ALTER TABLE fyniq.approval_request DROP CONSTRAINT approval_request_object_known;
ALTER TABLE fyniq.approval_request ADD CONSTRAINT approval_request_object_known CHECK (
  (object_type, operation) IN (('organisation_setting', 'organisation_setting.change'), ('membership', 'role.assign'),
                               ('membership', 'role.revoke'), ('member_invitation', 'member_invitation.create'),
                               ('approval_policy', 'approval_policy.change'), ('statutory_pack', 'statutory_pack.activate'),
                               ('team_mode', 'team_mode.change'))) NOT VALID;
ALTER TABLE fyniq.approval_policy DROP CONSTRAINT approval_policy_object_known;
ALTER TABLE fyniq.approval_policy ADD CONSTRAINT approval_policy_object_known CHECK (
  object_type IN ('organisation_setting', 'membership', 'member_invitation', 'statutory_pack', 'team_mode')) NOT VALID;

