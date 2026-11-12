-- Person keys, and which contacts are people (PRD-001 §11.8, SEC-11, AUD-02; rev 0.244; p1-pdpa-retention slice 9,
-- design D8). An audit event about a person seals their personal values under a key of their own for the event's
-- financial year (fyniq-audit-event@3); at the end of retention the key is shredded, and the values no longer open while
-- the event and the chain stay whole. Each key is a fresh data key wrapped by the deployment's key-encryption key
-- (Cloud KMS; a key file locally). Keys are only ever added; none changes, and none goes except through the approved
-- shredding run (FY317; the run arrives with slice 10). No role holds DELETE.
--
-- A contact is a person when it is a director or its new field `individual` says so: the Accountant sets it for any
-- other natural person.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.person_key (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),  -- the key's ID, which each sealed value names
  person          text        NOT NULL,  -- member:<user id>, contact:<contact id>, invitation:<id> or join_request:<id>
  cohort          date        NOT NULL,  -- the last day of the financial year its values belong to
  wrapped         bytea       NOT NULL,  -- the data key, wrapped by the deployment's key-encryption key
  created_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT person_key_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT person_key_once UNIQUE (organisation_id, person, cohort),
  CONSTRAINT person_key_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT person_key_person_format CHECK (person ~ '^(member|contact|invitation|join_request):[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'),
  CONSTRAINT person_key_wrapped_length CHECK (octet_length(wrapped) BETWEEN 16 AND 2048)
);

-- A key never changes, and goes only inside the approved shredding run, which marks its transaction.
-- +goose StatementBegin
CREATE FUNCTION fyniq.person_key_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'UPDATE' THEN
    RAISE EXCEPTION 'A person''s key never changes' USING ERRCODE = 'FY317';
  END IF;
  IF TG_OP = 'DELETE' AND coalesce(current_setting('fyniq.shredding_run', true), '') = '' THEN
    RAISE EXCEPTION 'A person''s key goes only in an approved shredding run' USING ERRCODE = 'FY317',
      HINT = 'The DPO approves the weekly shredding run (§11.8).';
  END IF;
  RETURN CASE TG_OP WHEN 'DELETE' THEN OLD ELSE NEW END;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER person_key_guard BEFORE UPDATE OR DELETE ON fyniq.person_key
  FOR EACH ROW EXECUTE FUNCTION fyniq.person_key_guard();
CREATE TRIGGER person_key_no_truncate BEFORE TRUNCATE ON fyniq.person_key
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();

ALTER TABLE fyniq.person_key ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.person_key FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.person_key
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

-- Every role that writes audit events seals with these keys; the API also opens them for the explorer.
GRANT INSERT (person, cohort, wrapped) ON fyniq.person_key TO fyniq_api, fyniq_worker, fyniq_executor;
GRANT SELECT ON fyniq.person_key TO fyniq_api, fyniq_worker, fyniq_executor;

ALTER TABLE fyniq.contact ADD COLUMN individual boolean NOT NULL DEFAULT false;
GRANT INSERT (individual), UPDATE (individual) ON fyniq.contact TO fyniq_api;
