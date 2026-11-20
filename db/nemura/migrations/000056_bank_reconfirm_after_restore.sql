-- After a restore of Nemura's database, Fyniq may hold a bank-detail version Nemura lost (PRD-002 NM-INT-13; PRD-001 FR-PPL-19). A bank-detail version was the
-- ordinal of a person's confirmed changes, so a restored Nemura numbers the next confirmed change with an ordinal Fyniq already holds for another record,
-- and Fyniq ignores it. Fyniq's answer to an ignored bank-details event now carries the version and the record it holds; Nemura compares it with its own record at
-- that version and, where they differ, does two things:
--  * bank_version_floor raises that person's version floor, so the next confirmed change is numbered above what Fyniq holds (it only ever rises, and is never
--    deleted; a change now keeps the version it was confirmed with, in bank_account_change.bank_version, and one confirmed before this migration keeps its ordinal); and
--  * bank_reconfirm records the person for the people team to re-confirm: while it is open, no payment line of the person is made, by file or by Fyniq. It is resolved
--    when a bank change numbered above the held version is confirmed (the ordinary change: proposal, approval, the employee's passkey), never by a person's say-so.
-- Both hold identities and numbers only: no account, name or amount.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE nemura.bank_account_change ADD COLUMN bank_version integer;
ALTER TABLE nemura.bank_account_change ADD CONSTRAINT bank_account_change_bank_version CHECK (bank_version IS NULL OR bank_version >= 1) NOT VALID;

CREATE TABLE nemura.bank_version_floor (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  person_id       uuid        NOT NULL,
  floor           integer     NOT NULL,
  updated_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT bank_version_floor_pkey PRIMARY KEY (organisation_id, person_id),
  CONSTRAINT bank_version_floor_person_fkey FOREIGN KEY (organisation_id, person_id) REFERENCES nemura.person (organisation_id, id),
  CONSTRAINT bank_version_floor_positive CHECK (floor >= 1)
);

CREATE TABLE nemura.bank_reconfirm (
  organisation_id   uuid        NOT NULL DEFAULT nemura.current_org(),
  id                uuid        NOT NULL DEFAULT uuidv7(),
  person_id         uuid        NOT NULL,
  held_version      integer     NOT NULL,   -- the version Fyniq told Nemura it holds
  held_record_id    uuid        NOT NULL,   -- the record Fyniq told Nemura it holds (an ID, never an account)
  nemura_version    integer     NOT NULL,   -- Nemura's own version when this was found
  found_at          timestamptz NOT NULL,
  state             text        NOT NULL DEFAULT 'open',
  resolved_at       timestamptz,
  resolved_change_id uuid,
  CONSTRAINT bank_reconfirm_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT bank_reconfirm_person_fkey FOREIGN KEY (organisation_id, person_id) REFERENCES nemura.person (organisation_id, id),
  CONSTRAINT bank_reconfirm_change_fkey FOREIGN KEY (organisation_id, resolved_change_id) REFERENCES nemura.bank_account_change (organisation_id, id),
  CONSTRAINT bank_reconfirm_state_known CHECK (state IN ('open', 'resolved')),
  CONSTRAINT bank_reconfirm_versions CHECK (held_version >= 1 AND nemura_version >= 0),
  CONSTRAINT bank_reconfirm_resolved_whole CHECK ((state = 'resolved') = (resolved_at IS NOT NULL AND resolved_change_id IS NOT NULL))
);
CREATE UNIQUE INDEX bank_reconfirm_one_open ON nemura.bank_reconfirm (organisation_id, person_id) WHERE state = 'open';

-- The floor only rises and is never deleted; a flag ends once, by a confirmed change, and is never deleted or reopened.
-- +goose StatementBegin
CREATE FUNCTION nemura.bank_version_floor_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR NEW.person_id IS DISTINCT FROM OLD.person_id OR NEW.floor <= OLD.floor THEN
    RAISE EXCEPTION 'A person''s bank-detail version floor only rises and is never deleted' USING ERRCODE = 'NM194';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER bank_version_floor_guard BEFORE UPDATE OR DELETE ON nemura.bank_version_floor FOR EACH ROW EXECUTE FUNCTION nemura.bank_version_floor_guard();

-- +goose StatementBegin
CREATE FUNCTION nemura.bank_reconfirm_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR OLD.state <> 'open' OR NEW.state <> 'resolved' OR NEW.person_id IS DISTINCT FROM OLD.person_id
     OR NEW.held_version IS DISTINCT FROM OLD.held_version OR NEW.held_record_id IS DISTINCT FROM OLD.held_record_id
     OR NEW.nemura_version IS DISTINCT FROM OLD.nemura_version OR NEW.found_at IS DISTINCT FROM OLD.found_at THEN
    RAISE EXCEPTION 'A bank re-confirmation is found once and resolved once, and never deleted' USING ERRCODE = 'NM195';
  END IF;
  -- it ends only by a confirmed change of the same person's account, numbered above the version Fyniq held
  IF NOT EXISTS (SELECT 1 FROM nemura.bank_account_change c WHERE c.organisation_id = NEW.organisation_id AND c.id = NEW.resolved_change_id
                   AND c.person_id = NEW.person_id AND c.state = 'confirmed' AND c.bank_version > NEW.held_version) THEN
    RAISE EXCEPTION 'A bank re-confirmation ends only with a confirmed change of that person''s account, numbered above the version Fyniq held' USING ERRCODE = 'NM195';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER bank_reconfirm_guard BEFORE UPDATE OR DELETE ON nemura.bank_reconfirm FOR EACH ROW EXECUTE FUNCTION nemura.bank_reconfirm_guard();

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.bank_reconfirm
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('bank_reconfirm', 'id');

ALTER TABLE nemura.bank_version_floor ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.bank_version_floor FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.bank_version_floor USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.bank_reconfirm ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.bank_reconfirm FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.bank_reconfirm USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());

REVOKE ALL ON nemura.bank_version_floor, nemura.bank_reconfirm FROM PUBLIC;
GRANT SELECT ON nemura.bank_version_floor, nemura.bank_reconfirm TO nemura_api, nemura_worker, nemura_readonly;
-- The worker reads Fyniq's answer: it raises the floor and finds the flag. The API, which confirms a change, resolves the flag; neither can do the other's part.
GRANT INSERT (person_id, floor), UPDATE (floor, updated_at) ON nemura.bank_version_floor TO nemura_worker;
-- A change keeps the version it was confirmed with: set by the confirmation, which the guard above lets move a change forward only.
GRANT UPDATE (bank_version) ON nemura.bank_account_change TO nemura_api;
GRANT INSERT (person_id, held_version, held_record_id, nemura_version, found_at) ON nemura.bank_reconfirm TO nemura_worker;
GRANT UPDATE (state, resolved_at, resolved_change_id) ON nemura.bank_reconfirm TO nemura_api;
