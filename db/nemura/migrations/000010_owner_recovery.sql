-- The deployment's two-person recovery of an Owner (PRD-002 NM-ARC-01, NM-ROLE-05; PRD-001 FR-ADM-09, SEC-35 as Fyniq does it for a Super admin).
--
-- An Owner who can no longer sign in (their provider account is lost, locked or compromised) is recovered by two people who hold the deployment's
-- migrator credential. One runs the command with both names; it prints a one-time code, valid 24 hours, and the audit trail records both names. The
-- Owner signs in with the account they will keep and enters the code. The recovery then replaces every other sign-in of that member, ends their
-- sessions and revokes their passkeys, each audited. A code is kept only as its SHA-256, works once, and only the member's newest code works: a new
-- one replaces an unused one. Every refusal is the same answer.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE nemura.owner_recovery (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL,
  member_id       uuid        NOT NULL,
  code_hash       bytea       NOT NULL,
  operators       text[]      NOT NULL,   -- the two people who issued it, by name (a record, as Fyniq's trail keeps them)
  created_at      timestamptz NOT NULL DEFAULT now(),
  expires_at      timestamptz NOT NULL,
  used_at         timestamptz,
  used_xact       bigint,                 -- the transaction that used it: set by the guard, never by a caller
  CONSTRAINT owner_recovery_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT owner_recovery_code_key UNIQUE (organisation_id, code_hash),
  CONSTRAINT owner_recovery_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES nemura.organisation (organisation_id),
  CONSTRAINT owner_recovery_member_fkey FOREIGN KEY (organisation_id, member_id)
    REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT owner_recovery_code_sha256 CHECK (length(code_hash) = 32),
  CONSTRAINT owner_recovery_two_operators CHECK (cardinality(operators) = 2),
  CONSTRAINT owner_recovery_expiry CHECK (expires_at > created_at AND expires_at <= created_at + interval '24 hours')
);
CREATE INDEX owner_recovery_member_idx ON nemura.owner_recovery (organisation_id, member_id, created_at);

-- A recovery only gets used, once, by the transaction that does the recovery; nothing else changes and nothing is deleted.
-- +goose StatementBegin
CREATE FUNCTION nemura.owner_recovery_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR NEW.member_id IS DISTINCT FROM OLD.member_id OR NEW.code_hash IS DISTINCT FROM OLD.code_hash
     OR NEW.operators IS DISTINCT FROM OLD.operators OR NEW.expires_at IS DISTINCT FROM OLD.expires_at OR NEW.created_at IS DISTINCT FROM OLD.created_at
     OR OLD.used_at IS NOT NULL OR NEW.used_at IS NULL OR NEW.used_xact IS DISTINCT FROM OLD.used_xact THEN
    RAISE EXCEPTION 'A recovery code can only be used, once' USING ERRCODE = 'NM113';
  END IF;
  IF OLD.expires_at <= now() THEN
    RAISE EXCEPTION 'The recovery code has expired' USING ERRCODE = 'NM113';
  END IF;
  NEW.used_xact := pg_current_xact_id()::text::bigint;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER owner_recovery_guard BEFORE UPDATE OR DELETE ON nemura.owner_recovery
  FOR EACH ROW EXECUTE FUNCTION nemura.owner_recovery_guard();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.owner_recovery
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('owner_recovery', 'id');

ALTER TABLE nemura.owner_recovery ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.owner_recovery FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.owner_recovery
  USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());

-- The api reads a code to check it and marks it used; it can never make one (no INSERT grant): only the migrator's function below does.
GRANT SELECT ON nemura.owner_recovery TO nemura_api, nemura_worker, nemura_readonly;
GRANT UPDATE (used_at) ON nemura.owner_recovery TO nemura_api;

-- The migrator issues a code for an Owner, with the audit event the caller built (the shared canonical form) in the same transaction. It refuses a
-- member who is not an active Owner on the caller's day (its clock, as every role check), and anything but exactly two distinct operator names.
-- +goose StatementBegin
CREATE FUNCTION nemura.issue_owner_recovery(p_org uuid, p_id uuid, p_member uuid, p_hash bytea, p_operators text[], p_event_id uuid, p_body text, p_on date)
RETURNS timestamptz
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$
DECLARE
  expires timestamptz := now() + interval '24 hours';
  names text[];
BEGIN
  PERFORM set_config('app.org_id', p_org::text, true);
  SELECT array_agg(btrim(n)) INTO names FROM unnest(p_operators) AS n;
  IF names IS NULL OR cardinality(names) <> 2 OR EXISTS (SELECT 1 FROM unnest(names) n WHERE length(n) NOT BETWEEN 1 AND 100)
     OR lower(names[1]) = lower(names[2]) THEN
    RAISE EXCEPTION 'A recovery needs exactly two different operators, by name' USING ERRCODE = 'NM110';
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM nemura.member m JOIN nemura.role_assignment a ON a.organisation_id = m.organisation_id AND a.member_id = m.id
     WHERE m.organisation_id = p_org AND m.id = p_member AND m.deactivated_at IS NULL AND a.role = 'owner'
       AND a.effective_from <= p_on AND (a.effective_to IS NULL OR a.effective_to > p_on)) THEN
    RAISE EXCEPTION 'The member is not an active Owner of that organisation' USING ERRCODE = 'NM111';
  END IF;
  INSERT INTO nemura.audit_event (id, body) VALUES (p_event_id, p_body);
  INSERT INTO nemura.owner_recovery (id, member_id, code_hash, operators, created_at, expires_at)
  VALUES (p_id, p_member, p_hash, names, now(), expires);
  RETURN expires;
END
$$;
-- +goose StatementEnd

-- Replaces every sign-in of a member with one, in the transaction that used the member's recovery code: it refuses unless a recovery of that member
-- was used in this very transaction, and an account that already belongs to another member of the organisation. Returns how many it replaced.
-- +goose StatementBegin
CREATE FUNCTION nemura.replace_member_identities(p_member uuid, p_issuer text, p_subject text, p_tenant text) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$
DECLARE
  org uuid := nemura.require_org();
  replaced integer;
  holder uuid;
BEGIN
  IF NOT EXISTS (SELECT 1 FROM nemura.owner_recovery r WHERE r.organisation_id = org AND r.member_id = p_member AND r.used_xact = pg_current_xact_id()::text::bigint) THEN
    RAISE EXCEPTION 'Sign-ins are replaced only by a recovery used in this transaction' USING ERRCODE = 'NM112';
  END IF;
  SELECT member_id INTO holder FROM nemura.identity_directory WHERE issuer = p_issuer AND subject = p_subject AND tenant = p_tenant AND organisation_id = org;
  IF holder IS NOT NULL AND holder <> p_member THEN
    RAISE EXCEPTION 'That account belongs to another member of the organisation' USING ERRCODE = 'NM112';
  END IF;
  DELETE FROM nemura.identity_directory
   WHERE organisation_id = org AND member_id = p_member AND NOT (issuer = p_issuer AND subject = p_subject AND tenant = p_tenant);
  GET DIAGNOSTICS replaced = ROW_COUNT;
  IF holder IS NULL THEN
    INSERT INTO nemura.identity_directory (issuer, subject, tenant, organisation_id, member_id) VALUES (p_issuer, p_subject, p_tenant, org, p_member);
  END IF;
  RETURN replaced;
END
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION nemura.issue_owner_recovery(uuid, uuid, uuid, bytea, text[], uuid, text, date), nemura.replace_member_identities(uuid, text, text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION nemura.replace_member_identities(uuid, text, text, text) TO nemura_api;
