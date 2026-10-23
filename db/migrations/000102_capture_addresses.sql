-- The email-in addresses documents are captured from, and the senders accepted at them (PRD-001 FR-DOC-12, FR-INT-22;
-- the owner's decisions of 23 Oct 2026, rev 0.105; p1-templates-email task 2.2).
--
-- Each organisation may have one address of its own and each member one of theirs, readable names on the deployment's
-- capture domain (bills@in.example.com). An address is created and later retired, never changed or deleted (FY182).
-- A name belongs to one organisation for good: once used, no other organisation takes it, so mail meant for one never
-- reaches another (FY183); the same organisation may take it again. The names live in a global registry kept by a
-- trigger, which only the routing function reads, so mail arriving for a name finds its organisation before any tenant
-- is set.
--
-- Mail from a member to their own address, or from the organisation's members and its contacts' billing or remittance
-- emails to the organisation's, is accepted; every other sender is quarantined until someone accepts or rejects it.
-- That decision is kept per sender and address kind, latest in force, append-only (FY184).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.capture_address (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  local_part      text        NOT NULL,
  user_id         uuid,                  -- a member's own; none: the organisation's
  created_by      text        NOT NULL,
  created_at      timestamptz NOT NULL,
  retired_at      timestamptz,
  retired_by      text,
  CONSTRAINT capture_address_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT capture_address_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT capture_address_member_fkey FOREIGN KEY (organisation_id, user_id)
    REFERENCES fyniq.user_membership (organisation_id, user_id),
  CONSTRAINT capture_address_local_part_format CHECK (local_part ~ '^[a-z][a-z0-9.-]{1,38}[a-z0-9]$' AND local_part !~ '\.\.'),
  CONSTRAINT capture_address_by_format CHECK (created_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$'
    AND retired_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$'),
  CONSTRAINT capture_address_retired_pair CHECK ((retired_at IS NULL) = (retired_by IS NULL))
);
-- One live address for the organisation, and one for each member.
CREATE UNIQUE INDEX capture_address_live_owner ON fyniq.capture_address
  (organisation_id, coalesce(user_id, '00000000-0000-0000-0000-000000000000'::uuid)) WHERE retired_at IS NULL;

-- The global registry of names: the organisation that owns a name, and its live address there and that address's
-- member, if any.
CREATE TABLE fyniq.capture_name (
  local_part text NOT NULL,
  owner      uuid NOT NULL,              -- the organisation, for good
  address_id uuid,                       -- none while the name is retired
  user_id    uuid,                       -- the live address's member; none for the organisation's own
  CONSTRAINT capture_name_pkey PRIMARY KEY (local_part)
);
REVOKE ALL ON fyniq.capture_name FROM PUBLIC;

CREATE TABLE fyniq.capture_sender (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  address         text        NOT NULL,  -- the sender, in lower case
  user_id         uuid,                  -- a member's own address's decision; none: the organisation's
  decision        text        NOT NULL,
  decided_by      text        NOT NULL,
  decided_at      timestamptz NOT NULL,
  CONSTRAINT capture_sender_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT capture_sender_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT capture_sender_member_fkey FOREIGN KEY (organisation_id, user_id)
    REFERENCES fyniq.user_membership (organisation_id, user_id),
  CONSTRAINT capture_sender_decision_known CHECK (decision IN ('accepted', 'rejected')),
  CONSTRAINT capture_sender_address_lower CHECK (address = lower(address) AND length(address) BETWEEN 3 AND 254),
  CONSTRAINT capture_sender_by_format CHECK (decided_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);
CREATE INDEX capture_sender_latest ON fyniq.capture_sender (organisation_id, address, decided_at);

-- An address is only ever retired, once (FY182); its name is registered, or taken back by its own organisation, and
-- never by another (FY183).
-- +goose StatementBegin
CREATE FUNCTION fyniq.capture_address_guard() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  owner uuid;
BEGIN
  IF TG_OP = 'INSERT' THEN
    INSERT INTO fyniq.capture_name (local_part, owner, address_id, user_id)
    VALUES (NEW.local_part, NEW.organisation_id, NEW.id, NEW.user_id)
    ON CONFLICT (local_part) DO UPDATE SET address_id = EXCLUDED.address_id, user_id = EXCLUDED.user_id
      WHERE capture_name.owner = EXCLUDED.owner AND capture_name.address_id IS NULL
    RETURNING capture_name.owner INTO owner;
    IF owner IS NULL THEN
      RAISE EXCEPTION 'The capture address name % is taken', NEW.local_part USING ERRCODE = 'FY183',
        HINT = 'Choose another name: a name belongs to one organisation for good (FR-INT-22).';
    END IF;
    RETURN NEW;
  END IF;
  IF TG_OP = 'DELETE' OR OLD.retired_at IS NOT NULL OR NEW.retired_at IS NULL
     OR (NEW.local_part, NEW.user_id, NEW.created_by, NEW.created_at) IS DISTINCT FROM (OLD.local_part, OLD.user_id, OLD.created_by, OLD.created_at) THEN
    RAISE EXCEPTION 'A capture address is only ever retired, once' USING ERRCODE = 'FY182',
      HINT = 'Retire it and make a new one (FR-INT-22).';
  END IF;
  UPDATE fyniq.capture_name SET address_id = NULL, user_id = NULL WHERE local_part = OLD.local_part AND address_id = OLD.id;
  RETURN NEW;
END
$$;

-- A sender decision never changes or goes (FY184): a later one is in force.
CREATE FUNCTION fyniq.capture_sender_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'A sender decision never changes or goes' USING ERRCODE = 'FY184',
    HINT = 'Decide again: the latest decision about a sender is in force (FR-INT-22).';
END
$$;

-- The live address a name routes to, and its organisation and member; nothing for a name not live. The receiver of
-- inbound mail calls it before any tenant is set.
CREATE FUNCTION fyniq.capture_route(p_local text) RETURNS TABLE (organisation_id uuid, address_id uuid, user_id uuid)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT n.owner, n.address_id, n.user_id FROM fyniq.capture_name n
   WHERE n.local_part = lower(p_local) AND n.address_id IS NOT NULL
$$;
-- +goose StatementEnd
CREATE TRIGGER capture_address_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.capture_address
  FOR EACH ROW EXECUTE FUNCTION fyniq.capture_address_guard();
CREATE TRIGGER capture_address_no_truncate BEFORE TRUNCATE ON fyniq.capture_address
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();
CREATE TRIGGER capture_sender_guard BEFORE UPDATE OR DELETE ON fyniq.capture_sender
  FOR EACH ROW EXECUTE FUNCTION fyniq.capture_sender_guard();
CREATE TRIGGER capture_sender_no_truncate BEFORE TRUNCATE ON fyniq.capture_sender
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();

ALTER TABLE fyniq.capture_address ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.capture_address FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.capture_address
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.capture_sender ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.capture_sender FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.capture_sender
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.capture_address
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('capture_address', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.capture_sender
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('capture_sender', 'id');

REVOKE ALL ON FUNCTION fyniq.capture_route(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.capture_route(text) TO fyniq_api, fyniq_worker;
GRANT SELECT ON fyniq.capture_address, fyniq.capture_sender TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (local_part, user_id, created_by, created_at) ON fyniq.capture_address TO fyniq_api;
GRANT UPDATE (retired_at, retired_by) ON fyniq.capture_address TO fyniq_api;
GRANT INSERT (address, user_id, decision, decided_by, decided_at) ON fyniq.capture_sender TO fyniq_api;
