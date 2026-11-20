-- Groups of entities (PRD-001 FR-ENT-04; p3-multi-entity design D2 and D3, rev 0.625).
--
-- An entity is an organisation, and a group is organisations linked by the consent of BOTH Owners, read-only and narrow. Four parts:
--   * fyniq.group_invitation: a GLOBAL registry (like the practice registry, read by no application role) of one-time codes, stored as hashes, valid 24 hours;
--   * fyniq.group_entity: the parent's row for each entity it invited (invited, active, ended), in the parent's tenant;
--   * fyniq.group_membership: the entity's consent row, in the entity's tenant: which group, what it may read (scopes), when and by whom;
--   * five security-definer functions, owned by the migrator with a fixed search_path, executable by the API role ONLY, that are the only cross-tenant surface:
--       register_group_invitation  (the parent's Owner; writes the registry row)
--       accept_group_invitation    (the entity's Owner; the only cross-tenant WRITE: it activates the parent's row and creates the entity's consent row, once)
--       end_group_link / withdraw_group_invitation (either Owner; ends both rows, so the next read is refused)
--       group_entity_figures / group_entity_periods (the parent's read: fixed columns, below)
-- Every function takes the caller's organisation from fyniq.current_org() (never from a parameter), checks the acting person's own live Owner role there, switches
-- app.org_id only to read or write the named rows, and restores it on every exit. A read needs BOTH rows active. What the figures function returns is the grant, and is
-- fixed: per account of the entity (code, name, type) and month, the net base amount, with the entity's base currency; nothing else of the entity is readable, and no
-- application role has any write on another tenant. The caller writes the audit events of both organisations in the same transaction (AUD-01).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.group_invitation (
  id            uuid        NOT NULL DEFAULT uuidv7(),
  kind          text        NOT NULL,
  code_hash     bytea       NOT NULL,
  parent_org    uuid        NOT NULL,  -- the parent: an ID only, like parent_row, so no foreign key reaches into a tenant's row
  parent_row    uuid        NOT NULL,  -- the group_entity (or firm_client) row in the parent's tenant: an ID only, since a foreign key from a global table would bypass its row-level security
  created_by    uuid        NOT NULL,
  created_at    timestamptz NOT NULL,
  expires_at    timestamptz NOT NULL,
  state         text        NOT NULL DEFAULT 'open',
  consumed_org  uuid,
  consumed_at   timestamptz,
  CONSTRAINT group_invitation_pkey PRIMARY KEY (id),
  CONSTRAINT group_invitation_code_key UNIQUE (code_hash),
  CONSTRAINT group_invitation_user_fkey FOREIGN KEY (created_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT group_invitation_kind_known CHECK (kind IN ('group', 'firm')),
  CONSTRAINT group_invitation_hash_length CHECK (length(code_hash) = 32),
  CONSTRAINT group_invitation_state_known CHECK (state IN ('open', 'consumed', 'withdrawn')),
  CONSTRAINT group_invitation_24_hours CHECK (expires_at > created_at AND expires_at <= created_at + interval '24 hours'),
  CONSTRAINT group_invitation_consumed_whole CHECK ((state = 'consumed') = (consumed_org IS NOT NULL) AND (consumed_org IS NULL) = (consumed_at IS NULL))
);
CREATE UNIQUE INDEX group_invitation_one_open ON fyniq.group_invitation (parent_row) WHERE state = 'open';
REVOKE ALL ON fyniq.group_invitation FROM PUBLIC;

-- ------------------------------------------------------------------ tenant: the parent's rows
CREATE TABLE fyniq.group_entity (
  organisation_id  uuid        NOT NULL DEFAULT fyniq.current_org(),
  id               uuid        NOT NULL DEFAULT uuidv7(),
  label            text        NOT NULL,  -- what the parent calls the entity
  entity_org       uuid,                  -- the entity's organisation, set when it accepts: an ID only (no foreign key across tenants)
  entity_name      text,                  -- its name and base currency when it accepted
  entity_currency  text,
  state            text        NOT NULL DEFAULT 'invited',
  invited_by       uuid        NOT NULL,
  invited_at       timestamptz NOT NULL,
  linked_by        uuid,
  linked_at        timestamptz,
  ended_by         uuid,
  ended_at         timestamptz,
  ended_reason     text,
  version          integer     NOT NULL DEFAULT 1,
  CONSTRAINT group_entity_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT group_entity_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT group_entity_inviter_fkey FOREIGN KEY (invited_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT group_entity_label_length CHECK (length(btrim(label)) BETWEEN 1 AND 100),
  CONSTRAINT group_entity_state_known CHECK (state IN ('invited', 'active', 'ended')),
  CONSTRAINT group_entity_not_itself CHECK (entity_org IS DISTINCT FROM organisation_id),
  CONSTRAINT group_entity_linked_whole CHECK ((entity_org IS NULL) = (linked_at IS NULL) AND (entity_org IS NULL) = (linked_by IS NULL)
    AND (entity_org IS NULL) = (entity_name IS NULL) AND (entity_org IS NULL) = (entity_currency IS NULL)),
  CONSTRAINT group_entity_active_linked CHECK (state <> 'active' OR entity_org IS NOT NULL),
  CONSTRAINT group_entity_invited_unlinked CHECK (state <> 'invited' OR entity_org IS NULL),
  CONSTRAINT group_entity_ended_whole CHECK ((state = 'ended') = (ended_at IS NOT NULL) AND (state = 'ended') = (ended_by IS NOT NULL) AND (state = 'ended') = (ended_reason IS NOT NULL)),
  CONSTRAINT group_entity_reason_length CHECK (length(btrim(ended_reason)) BETWEEN 1 AND 500),
  CONSTRAINT group_entity_version_positive CHECK (version >= 1)
);
-- One live link to an organisation at a time.
CREATE UNIQUE INDEX group_entity_one_live ON fyniq.group_entity (organisation_id, entity_org) WHERE state = 'active';

-- ------------------------------------------------------------------ tenant: the entity's consent
CREATE TABLE fyniq.group_membership (
  organisation_id  uuid        NOT NULL DEFAULT fyniq.current_org(),
  id               uuid        NOT NULL DEFAULT uuidv7(),
  parent_org       uuid        NOT NULL,   -- the group's parent: an ID only
  parent_name      text        NOT NULL,   -- its name when this organisation accepted
  parent_row       uuid        NOT NULL,   -- the parent's group_entity row
  scopes           text[]      NOT NULL DEFAULT ARRAY['figures', 'intercompany'],
  state            text        NOT NULL DEFAULT 'active',
  accepted_by      uuid        NOT NULL,
  accepted_at      timestamptz NOT NULL,
  ended_by         uuid,
  ended_at         timestamptz,
  ended_reason     text,
  version          integer     NOT NULL DEFAULT 1,
  CONSTRAINT group_membership_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT group_membership_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT group_membership_acceptor_fkey FOREIGN KEY (accepted_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT group_membership_scopes_known CHECK (scopes <@ ARRAY['figures', 'intercompany'] AND cardinality(scopes) >= 1),
  CONSTRAINT group_membership_state_known CHECK (state IN ('active', 'ended')),
  CONSTRAINT group_membership_not_itself CHECK (parent_org <> organisation_id),
  CONSTRAINT group_membership_ended_whole CHECK ((state = 'ended') = (ended_at IS NOT NULL) AND (state = 'ended') = (ended_by IS NOT NULL) AND (state = 'ended') = (ended_reason IS NOT NULL)),
  CONSTRAINT group_membership_reason_length CHECK (length(btrim(ended_reason)) BETWEEN 1 AND 500),
  CONSTRAINT group_membership_version_positive CHECK (version >= 1)
);
-- An organisation belongs to one group at a time.
CREATE UNIQUE INDEX group_membership_one_live ON fyniq.group_membership (organisation_id) WHERE state = 'active';

-- Never deleted; identity columns never change; a state moves forward only: invited to active or ended, active to ended, and ended is final (FYE11).
-- +goose StatementBegin
CREATE FUNCTION fyniq.group_row_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP IN ('DELETE', 'TRUNCATE') THEN
    RAISE EXCEPTION 'A link of a group is kept: it is ended with a reason, never deleted' USING ERRCODE = 'FYE11', HINT = 'End the link instead (FR-ENT-04).';
  END IF;
  IF OLD.state = 'ended' OR NEW.version <> OLD.version + 1 THEN
    RAISE EXCEPTION 'A link of a group moves forward one version at a time, and an ended one not at all' USING ERRCODE = 'FYE11', HINT = 'Make a new invitation (FR-ENT-04).';
  END IF;
  IF TG_TABLE_NAME = 'group_entity' THEN
    IF (NEW.organisation_id, NEW.id, NEW.label, NEW.invited_by, NEW.invited_at) IS DISTINCT FROM (OLD.organisation_id, OLD.id, OLD.label, OLD.invited_by, OLD.invited_at)
       OR (OLD.entity_org IS NOT NULL AND NEW.entity_org IS DISTINCT FROM OLD.entity_org) THEN
      RAISE EXCEPTION 'The identity of a link of a group never changes' USING ERRCODE = 'FYE11', HINT = 'A link is ended, never deleted or reopened (FR-ENT-04).';
    END IF;
    IF OLD.state = 'active' AND NEW.state <> 'ended' THEN
      RAISE EXCEPTION 'An active link is only ended' USING ERRCODE = 'FYE11', HINT = 'A link is ended, never deleted or reopened (FR-ENT-04).';
    END IF;
  ELSE
    IF (NEW.organisation_id, NEW.id, NEW.parent_org, NEW.parent_name, NEW.parent_row, NEW.scopes, NEW.accepted_by, NEW.accepted_at)
       IS DISTINCT FROM (OLD.organisation_id, OLD.id, OLD.parent_org, OLD.parent_name, OLD.parent_row, OLD.scopes, OLD.accepted_by, OLD.accepted_at) THEN
      RAISE EXCEPTION 'The identity and the consent of a link never change' USING ERRCODE = 'FYE11', HINT = 'End it and ask for a new invitation (FR-ENT-04).';
    END IF;
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER group_entity_guard BEFORE UPDATE OR DELETE ON fyniq.group_entity FOR EACH ROW EXECUTE FUNCTION fyniq.group_row_guard();
CREATE TRIGGER group_entity_no_truncate BEFORE TRUNCATE ON fyniq.group_entity FOR EACH STATEMENT EXECUTE FUNCTION fyniq.group_row_guard();
CREATE TRIGGER group_membership_guard BEFORE UPDATE OR DELETE ON fyniq.group_membership FOR EACH ROW EXECUTE FUNCTION fyniq.group_row_guard();
CREATE TRIGGER group_membership_no_truncate BEFORE TRUNCATE ON fyniq.group_membership FOR EACH STATEMENT EXECUTE FUNCTION fyniq.group_row_guard();

ALTER TABLE fyniq.group_entity ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.group_entity FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.group_entity USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.group_entity
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('group_entity', 'id');
ALTER TABLE fyniq.group_membership ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.group_membership FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.group_membership USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.group_membership
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('group_membership', 'id');

REVOKE ALL ON fyniq.group_entity, fyniq.group_membership FROM PUBLIC;
GRANT SELECT ON fyniq.group_entity, fyniq.group_membership TO fyniq_api, fyniq_worker, fyniq_readonly;
-- The API writes its own invitation row; every later change of either row is made by the functions below.
GRANT INSERT (label, invited_by, invited_at) ON fyniq.group_entity TO fyniq_api;

-- ------------------------------------------------------------------ the functions
-- The acting person's own live Owner role in the caller's organisation (one membership of the person asking).
-- +goose StatementBegin
CREATE FUNCTION fyniq.group_caller_is_owner(p_user uuid, p_at timestamptz) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT EXISTS (
    SELECT 1 FROM fyniq.user_membership m JOIN fyniq.role_assignment r ON r.organisation_id = m.organisation_id AND r.membership_id = m.id
     WHERE m.organisation_id = fyniq.require_org() AND m.user_id = p_user AND m.status = 'active' AND r.role = 'owner'
       AND r.revoked_at IS NULL AND (r.expires_at IS NULL OR r.expires_at > p_at))
$$;
-- +goose StatementEnd

-- An organisation that holds no group: the installation's, a practice organisation and a sandbox organisation.
-- +goose StatementBegin
CREATE FUNCTION fyniq.group_organisation_is_ordinary(p_org uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT p_org <> fyniq.installation_org() AND NOT fyniq.organisation_is_practice(p_org) AND NOT fyniq.organisation_is_sandbox(p_org)
$$;
-- +goose StatementEnd

-- Both rows of a link are active: the parent's row names the entity and the entity's row names the parent and the scope. Reads the two tenants one after the other and
-- restores the caller's.
-- +goose StatementBegin
CREATE FUNCTION fyniq.group_link_is_live(p_parent uuid, p_entity uuid, p_scope text) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  previous text := current_setting('app.org_id', true);
  a boolean;
  b boolean := false;
BEGIN
  PERFORM set_config('app.org_id', p_parent::text, true);
  SELECT EXISTS (SELECT 1 FROM fyniq.group_entity g WHERE g.entity_org = p_entity AND g.state = 'active') INTO a;
  IF a THEN
    PERFORM set_config('app.org_id', p_entity::text, true);
    SELECT EXISTS (SELECT 1 FROM fyniq.group_membership m WHERE m.parent_org = p_parent AND m.state = 'active' AND p_scope = ANY (m.scopes)) INTO b;
  END IF;
  PERFORM set_config('app.org_id', coalesce(previous, ''), true);
  RETURN a AND b;
END
$$;
-- +goose StatementEnd

-- The parent's Owner registers the code of an invitation row they made.
-- +goose StatementBegin
CREATE FUNCTION fyniq.register_group_invitation(p_row uuid, p_hash bytea, p_expires timestamptz, p_user uuid, p_at timestamptz) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  caller uuid := fyniq.require_org();
  made   uuid;
BEGIN
  IF NOT fyniq.group_caller_is_owner(p_user, p_at) THEN
    RAISE EXCEPTION 'only an Owner of the organisation invites an entity into its group' USING ERRCODE = 'FYE01', HINT = 'Ask an Owner of the organisation, and confirm it is you with step-up (FR-ENT-04).';
  END IF;
  IF NOT fyniq.group_organisation_is_ordinary(caller) THEN
    RAISE EXCEPTION 'the installation''s organisation, a practice organisation and a sandbox organisation hold no group' USING ERRCODE = 'FYE02', HINT = 'A group is held by an organisation that keeps books (FR-ENT-04).';
  END IF;
  IF EXISTS (SELECT 1 FROM fyniq.group_membership m WHERE m.state = 'active') THEN
    RAISE EXCEPTION 'an organisation that is an entity of a group is not itself the parent of one' USING ERRCODE = 'FYE03', HINT = 'A group has one level: a parent has entities, an entity has no entities of its own (FR-ENT-04).';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM fyniq.group_entity g WHERE g.id = p_row AND g.state = 'invited') THEN
    RAISE EXCEPTION 'there is no open invitation row with that ID' USING ERRCODE = 'FYE04', HINT = 'Read the group for its open invitations (FR-ENT-04).';
  END IF;
  IF p_expires <= p_at OR p_expires > p_at + interval '24 hours' THEN
    RAISE EXCEPTION 'an invitation is valid for at most 24 hours' USING ERRCODE = 'FYE05', HINT = 'Make a new invitation (FR-ENT-04).';
  END IF;
  INSERT INTO fyniq.group_invitation (kind, code_hash, parent_org, parent_row, created_by, created_at, expires_at)
    VALUES ('group', p_hash, caller, p_row, p_user, p_at, p_expires) RETURNING id INTO made;
  RETURN made;
END
$$;
-- +goose StatementEnd

-- The entity's Owner accepts a code in their own organisation. The one cross-tenant write: the parent's invited row becomes active and the entity's consent row is made.
-- +goose StatementBegin
CREATE FUNCTION fyniq.accept_group_invitation(p_hash bytea, p_user uuid, p_at timestamptz,
  OUT out_parent_org uuid, OUT out_parent_name text, OUT out_parent_row uuid, OUT out_membership uuid, OUT out_entity_name text, OUT out_entity_currency text)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  entity   uuid := fyniq.require_org();
  previous text := current_setting('app.org_id', true);
  inv      fyniq.group_invitation%ROWTYPE;
BEGIN
  PERFORM pg_advisory_xact_lock(hashtextextended('fyniq.group.code:' || encode(p_hash, 'hex'), 0));
  SELECT * INTO inv FROM fyniq.group_invitation g WHERE g.code_hash = p_hash AND g.kind = 'group' FOR UPDATE;
  IF NOT FOUND OR inv.state <> 'open' OR inv.expires_at <= p_at THEN
    RAISE EXCEPTION 'that invitation code is not valid: it is wrong, used, withdrawn or older than 24 hours' USING ERRCODE = 'FYE06', HINT = 'Ask the parent''s Owner for a new code; a code is used once and lasts 24 hours (FR-ENT-04).';
  END IF;
  IF NOT fyniq.group_caller_is_owner(p_user, p_at) THEN
    RAISE EXCEPTION 'only an Owner of the organisation accepts an invitation into a group' USING ERRCODE = 'FYE01', HINT = 'Ask an Owner of the organisation, and confirm it is you with step-up (FR-ENT-04).';
  END IF;
  IF entity = inv.parent_org OR NOT fyniq.group_organisation_is_ordinary(entity) THEN
    RAISE EXCEPTION 'the installation''s organisation, a practice organisation, a sandbox organisation and the group''s own parent are never an entity' USING ERRCODE = 'FYE02', HINT = 'A group is held by an organisation that keeps books (FR-ENT-04).';
  END IF;
  IF EXISTS (SELECT 1 FROM fyniq.group_membership m WHERE m.state = 'active') THEN
    RAISE EXCEPTION 'the organisation belongs to a group already, and belongs to one at a time' USING ERRCODE = 'FYE07', HINT = 'End the link to the first group before joining another (FR-ENT-04).';
  END IF;
  IF EXISTS (SELECT 1 FROM fyniq.group_entity g WHERE g.state IN ('invited', 'active')) THEN
    RAISE EXCEPTION 'an organisation that is the parent of a group is not itself an entity of one' USING ERRCODE = 'FYE03', HINT = 'A group has one level: a parent has entities, an entity has no entities of its own (FR-ENT-04).';
  END IF;
  SELECT o.name, o.base_currency INTO out_entity_name, out_entity_currency FROM fyniq.organisation o WHERE o.organisation_id = entity;
  -- The parent's side.
  PERFORM set_config('app.org_id', inv.parent_org::text, true);
  IF EXISTS (SELECT 1 FROM fyniq.group_membership m WHERE m.state = 'active') THEN
    PERFORM set_config('app.org_id', coalesce(previous, ''), true);
    RAISE EXCEPTION 'the group''s parent is itself an entity of another group' USING ERRCODE = 'FYE03', HINT = 'A group has one level: a parent has entities, an entity has no entities of its own (FR-ENT-04).';
  END IF;
  SELECT o.name INTO out_parent_name FROM fyniq.organisation o WHERE o.organisation_id = inv.parent_org;
  UPDATE fyniq.group_entity g SET entity_org = entity, entity_name = out_entity_name, entity_currency = out_entity_currency, state = 'active',
         linked_by = p_user, linked_at = p_at, version = g.version + 1
   WHERE g.id = inv.parent_row AND g.state = 'invited';
  IF NOT FOUND THEN
    PERFORM set_config('app.org_id', coalesce(previous, ''), true);
    RAISE EXCEPTION 'that invitation code is not valid: it is wrong, used, withdrawn or older than 24 hours' USING ERRCODE = 'FYE06', HINT = 'Ask the parent''s Owner for a new code; a code is used once and lasts 24 hours (FR-ENT-04).';
  END IF;
  -- The entity's side.
  PERFORM set_config('app.org_id', entity::text, true);
  INSERT INTO fyniq.group_membership (parent_org, parent_name, parent_row, accepted_by, accepted_at)
    VALUES (inv.parent_org, out_parent_name, inv.parent_row, p_user, p_at) RETURNING id INTO out_membership;
  UPDATE fyniq.group_invitation g SET state = 'consumed', consumed_org = entity, consumed_at = p_at WHERE g.id = inv.id;
  PERFORM set_config('app.org_id', coalesce(previous, ''), true);
  out_parent_org := inv.parent_org;
  out_parent_row := inv.parent_row;
END
$$;
-- +goose StatementEnd

-- Either Owner ends the link with a reason: both rows end together, so the next read is refused. p_other is the other organisation of the link.
-- +goose StatementBegin
CREATE FUNCTION fyniq.end_group_link(p_user uuid, p_other uuid, p_reason text, p_at timestamptz,
  OUT out_role text, OUT out_entity_row uuid, OUT out_parent_row uuid, OUT out_membership uuid)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  caller   uuid := fyniq.require_org();
  previous text := current_setting('app.org_id', true);
BEGIN
  IF NOT fyniq.group_caller_is_owner(p_user, p_at) THEN
    RAISE EXCEPTION 'only an Owner of the organisation ends a link of its group' USING ERRCODE = 'FYE01', HINT = 'Ask an Owner of the organisation, and confirm it is you with step-up (FR-ENT-04).';
  END IF;
  IF p_reason IS NULL OR length(btrim(p_reason)) NOT BETWEEN 1 AND 500 THEN
    RAISE EXCEPTION 'give a reason of 1 to 500 characters' USING ERRCODE = 'FYE08', HINT = 'Say why in a sentence of up to 500 characters (FR-ENT-04).';
  END IF;
  -- As the parent: an active row naming the other as its entity.
  SELECT g.id INTO out_entity_row FROM fyniq.group_entity g WHERE g.entity_org = p_other AND g.state = 'active' FOR UPDATE;
  IF FOUND THEN
    out_role := 'parent';
    out_parent_row := out_entity_row;
    UPDATE fyniq.group_entity g SET state = 'ended', ended_by = p_user, ended_at = p_at, ended_reason = btrim(p_reason), version = g.version + 1 WHERE g.id = out_entity_row;
    PERFORM set_config('app.org_id', p_other::text, true);
    UPDATE fyniq.group_membership m SET state = 'ended', ended_by = p_user, ended_at = p_at, ended_reason = btrim(p_reason), version = m.version + 1
     WHERE m.parent_org = caller AND m.parent_row = out_parent_row AND m.state = 'active' RETURNING m.id INTO out_membership;
  ELSE
    -- As the entity: an active consent row naming the other as its parent.
    SELECT m.id, m.parent_row INTO out_membership, out_parent_row FROM fyniq.group_membership m WHERE m.parent_org = p_other AND m.state = 'active' FOR UPDATE;
    IF NOT FOUND THEN
      PERFORM set_config('app.org_id', coalesce(previous, ''), true);
      RAISE EXCEPTION 'there is no live link between this organisation and that one' USING ERRCODE = 'FYE09', HINT = 'Read the group to see which links stand (FR-ENT-04).';
    END IF;
    out_role := 'entity';
    out_entity_row := out_parent_row;
    UPDATE fyniq.group_membership m SET state = 'ended', ended_by = p_user, ended_at = p_at, ended_reason = btrim(p_reason), version = m.version + 1 WHERE m.id = out_membership;
    PERFORM set_config('app.org_id', p_other::text, true);
    UPDATE fyniq.group_entity g SET state = 'ended', ended_by = p_user, ended_at = p_at, ended_reason = btrim(p_reason), version = g.version + 1
     WHERE g.id = out_parent_row AND g.entity_org = caller AND g.state = 'active';
  END IF;
  PERFORM set_config('app.org_id', coalesce(previous, ''), true);
END
$$;
-- +goose StatementEnd

-- The parent's Owner withdraws an invitation nobody accepted.
-- +goose StatementBegin
CREATE FUNCTION fyniq.withdraw_group_invitation(p_row uuid, p_user uuid, p_reason text, p_at timestamptz) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF NOT fyniq.group_caller_is_owner(p_user, p_at) THEN
    RAISE EXCEPTION 'only an Owner of the organisation withdraws an invitation' USING ERRCODE = 'FYE01', HINT = 'Ask an Owner of the organisation, and confirm it is you with step-up (FR-ENT-04).';
  END IF;
  IF p_reason IS NULL OR length(btrim(p_reason)) NOT BETWEEN 1 AND 500 THEN
    RAISE EXCEPTION 'give a reason of 1 to 500 characters' USING ERRCODE = 'FYE08', HINT = 'Say why in a sentence of up to 500 characters (FR-ENT-04).';
  END IF;
  UPDATE fyniq.group_entity g SET state = 'ended', ended_by = p_user, ended_at = p_at, ended_reason = btrim(p_reason), version = g.version + 1
   WHERE g.id = p_row AND g.state = 'invited';
  IF NOT FOUND THEN
    RETURN false;
  END IF;
  UPDATE fyniq.group_invitation i SET state = 'withdrawn' WHERE i.parent_row = p_row AND i.parent_org = fyniq.require_org() AND i.state = 'open';
  RETURN true;
END
$$;
-- +goose StatementEnd

-- ------------------------------------------------------------------ the parent's read: the grant, in fixed columns
-- Per account of the entity and month, the net base amount (debit positive) through the month of p_through, with the account's code, name and type and the entity's base
-- currency. The caller is the parent (current_org), both rows of the link must be active and the entity must have consented to the scope 'figures'.
-- +goose StatementBegin
CREATE FUNCTION fyniq.group_entity_figures(p_entity uuid, p_through date)
RETURNS TABLE (account_code text, account_name text, account_type text, base_currency text, month date, net_base numeric)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  parent uuid := fyniq.require_org();
BEGIN
  IF NOT fyniq.group_link_is_live(parent, p_entity, 'figures') THEN
    RAISE EXCEPTION 'there is no live link to that organisation that lets this group read its figures' USING ERRCODE = 'FYE10', HINT = 'The entity''s Owner must have accepted the invitation and not ended the link (FR-ENT-04).';
  END IF;
  PERFORM set_config('app.org_id', p_entity::text, true);
  RETURN QUERY
    SELECT a.code, a.name, a.type, o.base_currency, date_trunc('month', p.starts_on)::date, sum(b.base_amount)
      FROM fyniq.balance b
      JOIN fyniq.account a ON a.organisation_id = b.organisation_id AND a.id = b.account_id
      JOIN fyniq.period p ON p.organisation_id = b.organisation_id AND p.id = b.period_id
      JOIN fyniq.organisation o ON o.organisation_id = b.organisation_id
     WHERE b.organisation_id = p_entity AND date_trunc('month', p.starts_on)::date <= date_trunc('month', p_through)::date
     GROUP BY a.code, a.name, a.type, o.base_currency, date_trunc('month', p.starts_on)::date
     ORDER BY a.code, date_trunc('month', p.starts_on)::date;
  PERFORM set_config('app.org_id', parent::text, true);
END
$$;
-- +goose StatementEnd

-- The state of the entity's periods through that month and the ledger revision the figures were read at.
-- +goose StatementBegin
CREATE FUNCTION fyniq.group_entity_periods(p_entity uuid, p_through date)
RETURNS TABLE (period_month date, period_status text, ledger_revision bigint)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  parent uuid := fyniq.require_org();
BEGIN
  IF NOT fyniq.group_link_is_live(parent, p_entity, 'figures') THEN
    RAISE EXCEPTION 'there is no live link to that organisation that lets this group read its figures' USING ERRCODE = 'FYE10', HINT = 'The entity''s Owner must have accepted the invitation and not ended the link (FR-ENT-04).';
  END IF;
  PERFORM set_config('app.org_id', p_entity::text, true);
  RETURN QUERY
    SELECT date_trunc('month', p.starts_on)::date, p.status, r.revision
      FROM fyniq.period p JOIN fyniq.ledger_revision r ON r.organisation_id = p.organisation_id
     WHERE p.organisation_id = p_entity AND date_trunc('month', p.starts_on)::date <= date_trunc('month', p_through)::date
     ORDER BY p.starts_on;
  PERFORM set_config('app.org_id', parent::text, true);
END
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION fyniq.group_caller_is_owner(uuid, timestamptz), fyniq.group_organisation_is_ordinary(uuid), fyniq.group_link_is_live(uuid, uuid, text),
  fyniq.register_group_invitation(uuid, bytea, timestamptz, uuid, timestamptz), fyniq.accept_group_invitation(bytea, uuid, timestamptz),
  fyniq.end_group_link(uuid, uuid, text, timestamptz), fyniq.withdraw_group_invitation(uuid, uuid, text, timestamptz),
  fyniq.group_entity_figures(uuid, date), fyniq.group_entity_periods(uuid, date) FROM PUBLIC;
-- Only the API role runs the five that people reach; the helpers are the functions' own.
GRANT EXECUTE ON FUNCTION fyniq.register_group_invitation(uuid, bytea, timestamptz, uuid, timestamptz), fyniq.accept_group_invitation(bytea, uuid, timestamptz),
  fyniq.end_group_link(uuid, uuid, text, timestamptz), fyniq.withdraw_group_invitation(uuid, uuid, text, timestamptz),
  fyniq.group_entity_figures(uuid, date), fyniq.group_entity_periods(uuid, date) TO fyniq_api;
