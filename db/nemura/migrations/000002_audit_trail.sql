-- Nemura's audit trail (PRD-002 NM-ARC-05, NM-SEC-01; PRD-001 §11.5, AUD-01, AUD-08, INV-12).
--
-- Every saved change is written with an audit event in the same transaction. The database chains the events per
-- organisation, exactly as Fyniq's does (db/migrations/000006), so one verifier, and the format of contracts/audit/, serve
-- both applications:
--
--   hash = SHA-256(prev_hash ‖ body)
--
-- where body is the event's canonical JSON as stored (UTF-8), and the first event's prev_hash is 32 zero bytes. The chain
-- head row is locked for each append, which serialises appends per organisation. The trail cannot be changed, shortened or
-- switched off (AUD-08), and a change to an audited table with no event of its own in the same transaction is refused.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- ------------------------------------------------------------------ tenant: chain head
CREATE TABLE nemura.audit_chain (
  organisation_id uuid   NOT NULL DEFAULT nemura.current_org(),
  seq             bigint NOT NULL DEFAULT 0,
  head_hash       bytea  NOT NULL DEFAULT '\x0000000000000000000000000000000000000000000000000000000000000000',
  CONSTRAINT audit_chain_pkey PRIMARY KEY (organisation_id),
  CONSTRAINT audit_chain_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES nemura.organisation (organisation_id),
  CONSTRAINT audit_chain_seq_not_negative CHECK (seq >= 0),
  CONSTRAINT audit_chain_hash_sha256 CHECK (length(head_hash) = 32)
);

-- ------------------------------------------------------------------ tenant: audit events (AUD-01)
CREATE TABLE nemura.audit_event (
  organisation_id uuid   NOT NULL DEFAULT nemura.current_org(),
  id              uuid   NOT NULL,           -- the body's event_id
  seq             bigint NOT NULL,           -- set by audit_event_append
  body            text   NOT NULL,           -- canonical JSON, hashed exactly as stored
  prev_hash       bytea  NOT NULL,           -- set by audit_event_append
  hash            bytea  NOT NULL,           -- set by audit_event_append
  created_xact    bigint NOT NULL,           -- set by audit_event_append; for the AUD-08 check
  action          text   GENERATED ALWAYS AS (body::jsonb ->> 'action') STORED,
  object_type     text   GENERATED ALWAYS AS (body::jsonb -> 'object' ->> 'type') STORED,
  object_id       uuid   GENERATED ALWAYS AS ((body::jsonb -> 'object' ->> 'id')::uuid) STORED,
  CONSTRAINT audit_event_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT audit_event_seq_key UNIQUE (organisation_id, seq),
  CONSTRAINT audit_event_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES nemura.organisation (organisation_id),
  CONSTRAINT audit_event_seq_positive CHECK (seq > 0),
  CONSTRAINT audit_event_hashes_sha256 CHECK (length(prev_hash) = 32 AND length(hash) = 32),
  CONSTRAINT audit_event_body_object CHECK (jsonb_typeof(body::jsonb) = 'object'),
  CONSTRAINT audit_event_body_fields
    CHECK (body::jsonb ?& ARRAY['schema', 'event_id', 'organisation_id', 'occurred_at', 'actor', 'action', 'object']),
  CONSTRAINT audit_event_body_ids
    CHECK ((body::jsonb ->> 'event_id')::uuid = id AND (body::jsonb ->> 'organisation_id')::uuid = organisation_id),
  CONSTRAINT audit_event_body_size CHECK (octet_length(body) <= 2097152)
);
CREATE INDEX audit_event_object_idx ON nemura.audit_event (organisation_id, created_xact, object_type, object_id);

-- Appends an event to its organisation's chain under the chain head's lock.
-- +goose StatementBegin
CREATE FUNCTION nemura.audit_event_append() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$
DECLARE
  org uuid := nemura.require_org();
  h   record;
BEGIN
  SELECT seq, head_hash INTO h FROM nemura.audit_chain WHERE organisation_id = org FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'The organisation of this transaction has no audit chain' USING ERRCODE = 'NM006';
  END IF;
  NEW.organisation_id := org;
  NEW.seq := h.seq + 1;
  NEW.prev_hash := h.head_hash;
  NEW.hash := sha256(h.head_hash || convert_to(NEW.body, 'UTF8'));
  NEW.created_xact := pg_current_xact_id()::text::bigint;
  UPDATE nemura.audit_chain SET seq = NEW.seq, head_hash = NEW.hash WHERE organisation_id = org;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER audit_event_append BEFORE INSERT ON nemura.audit_event
  FOR EACH ROW EXECUTE FUNCTION nemura.audit_event_append();

-- The trail cannot be changed, shortened or switched off (AUD-08).
-- +goose StatementBegin
CREATE FUNCTION nemura.audit_immutable() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'The audit trail cannot be changed or deleted (% on %)', TG_OP, TG_TABLE_NAME
    USING ERRCODE = 'NM060', HINT = 'Audit events are append-only (AUD-08).';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER audit_event_immutable BEFORE UPDATE OR DELETE ON nemura.audit_event
  FOR EACH ROW EXECUTE FUNCTION nemura.audit_immutable();
CREATE TRIGGER audit_event_no_truncate BEFORE TRUNCATE ON nemura.audit_event
  FOR EACH STATEMENT EXECUTE FUNCTION nemura.audit_immutable();
CREATE TRIGGER audit_chain_no_truncate BEFORE TRUNCATE ON nemura.audit_chain
  FOR EACH STATEMENT EXECUTE FUNCTION nemura.audit_immutable();

-- The chain head only moves forward, one event at a time, and is never deleted.
-- +goose StatementBegin
CREATE FUNCTION nemura.audit_chain_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR NEW.seq <> OLD.seq + 1 THEN
    RAISE EXCEPTION 'The audit chain head only moves forward by one event' USING ERRCODE = 'NM060';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER audit_chain_guard BEFORE UPDATE OR DELETE ON nemura.audit_chain
  FOR EACH ROW EXECUTE FUNCTION nemura.audit_chain_guard();

-- A new organisation gets its chain head and its directory entry.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION nemura.organisation_registered() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  INSERT INTO nemura.audit_chain (organisation_id) VALUES (NEW.organisation_id);
  INSERT INTO nemura.tenant_directory (organisation_id) VALUES (NEW.organisation_id);
  RETURN NULL;
END
$$;
-- +goose StatementEnd

-- ------------------------------------------------------------------ AUD-08: no change without its event
-- Checked at commit for each audited row: an audit event about the same object must have been written in the same
-- transaction. Later migrations attach it to each audited table as they create it.
-- +goose StatementBegin
CREATE FUNCTION nemura.require_audit() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$
DECLARE
  audited_type text := TG_ARGV[0];
  audited_id   uuid := (to_jsonb(NEW) ->> TG_ARGV[1])::uuid;
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM nemura.audit_event e
     WHERE e.organisation_id = NEW.organisation_id
       AND e.created_xact = pg_current_xact_id()::text::bigint
       AND e.object_type = audited_type
       AND e.object_id = audited_id) THEN
    RAISE EXCEPTION 'The change to % % has no audit event in its transaction', audited_type, audited_id
      USING ERRCODE = 'NM061',
            HINT = 'Write changes through the unit of work, which records their audit events (AUD-08).';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.organisation
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('organisation', 'organisation_id');

-- ------------------------------------------------------------------ row-level security
ALTER TABLE nemura.audit_chain ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.audit_chain FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.audit_chain
  USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());

ALTER TABLE nemura.audit_event ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.audit_event FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.audit_event
  USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());

-- ------------------------------------------------------------------ grants (db/README.md)
-- Services append events; the worker and the read-only role read them; nobody updates them.
GRANT INSERT (id, body) ON nemura.audit_event TO nemura_api, nemura_worker, nemura_executor;
GRANT SELECT ON nemura.audit_event, nemura.audit_chain TO nemura_worker, nemura_readonly;

-- Organisations created before this migration (local databases only; nothing is in production): the owner reads them past
-- FORCE ROW LEVEL SECURITY for these statements.
ALTER TABLE nemura.organisation NO FORCE ROW LEVEL SECURITY;
INSERT INTO nemura.audit_chain (organisation_id) SELECT organisation_id FROM nemura.organisation;
ALTER TABLE nemura.organisation FORCE ROW LEVEL SECURITY;
