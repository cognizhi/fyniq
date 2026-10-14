-- Source documents and their immutable versions (PRD-001 §11.4; ENG-02, ENG-03, FR-GL-12).
--
-- A document is the record a posting comes from: in P0 the manual journal; invoices, bills, bank
-- transactions, pay runs and the rest join the type list with their P1 sources. Each saved state is
-- an immutable document_version; the document points at its current version. The engine posts a
-- version, and the entry's posting identity names it, so the same version never posts twice.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.document (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  type            text        NOT NULL,
  status          text        NOT NULL DEFAULT 'draft',
  current_version integer     NOT NULL DEFAULT 1,
  created_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT document_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT document_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT document_type_known CHECK (type IN ('journal')),
  CONSTRAINT document_status_known CHECK (status IN ('draft', 'posted', 'reversed')),
  CONSTRAINT document_current_version_positive CHECK (current_version > 0)
);

CREATE TABLE fyniq.document_version (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  document_id     uuid        NOT NULL,
  version         integer     NOT NULL,
  payload         jsonb       NOT NULL,
  created_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT document_version_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT document_version_key UNIQUE (organisation_id, document_id, version),
  CONSTRAINT document_version_document_fkey FOREIGN KEY (organisation_id, document_id)
    REFERENCES fyniq.document (organisation_id, id),
  CONSTRAINT document_version_positive CHECK (version > 0),
  CONSTRAINT document_version_payload_object CHECK (jsonb_typeof(payload) = 'object'),
  CONSTRAINT document_version_payload_size CHECK (octet_length(payload::text) <= 1048576)
);

-- The current version must exist; checked at commit, because the document is written first.
ALTER TABLE fyniq.document ADD CONSTRAINT document_current_version_fkey
  FOREIGN KEY (organisation_id, id, current_version)
  REFERENCES fyniq.document_version (organisation_id, document_id, version)
  DEFERRABLE INITIALLY DEFERRED;

-- A document moves draft → posted → reversed, never back; its type never changes; its current
-- version only moves forward. Versions are immutable.
-- +goose StatementBegin
CREATE FUNCTION fyniq.document_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF NEW.type IS DISTINCT FROM OLD.type OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
    RAISE EXCEPTION 'The type of document % cannot change', OLD.id USING ERRCODE = 'FY050';
  END IF;
  IF NEW.status IS DISTINCT FROM OLD.status AND NOT (
       (OLD.status = 'draft' AND NEW.status = 'posted') OR (OLD.status = 'posted' AND NEW.status = 'reversed')) THEN
    RAISE EXCEPTION 'Document % cannot go from % to %', OLD.id, OLD.status, NEW.status
      USING ERRCODE = 'FY051',
            HINT = 'A draft is posted, a posted document is reversed, and a reversed one stays reversed (FR-GL-12).';
  END IF;
  IF NEW.current_version < OLD.current_version THEN
    RAISE EXCEPTION 'Document % cannot go back to version %', OLD.id, NEW.current_version USING ERRCODE = 'FY052';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER document_guard BEFORE UPDATE ON fyniq.document
  FOR EACH ROW EXECUTE FUNCTION fyniq.document_guard();

-- +goose StatementBegin
CREATE FUNCTION fyniq.document_version_immutable() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'Document versions cannot be changed or deleted (% on %)', TG_OP, TG_TABLE_NAME
    USING ERRCODE = 'FY002', HINT = 'Save a new version instead.';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER document_version_immutable BEFORE UPDATE OR DELETE ON fyniq.document_version
  FOR EACH ROW EXECUTE FUNCTION fyniq.document_version_immutable();
CREATE TRIGGER document_version_no_truncate BEFORE TRUNCATE ON fyniq.document_version
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();
CREATE TRIGGER document_no_truncate BEFORE TRUNCATE ON fyniq.document
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();

-- ------------------------------------------------------------------ row-level security
ALTER TABLE fyniq.document ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.document FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.document
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

ALTER TABLE fyniq.document_version ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.document_version FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.document_version
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

-- ------------------------------------------------------------------ grants (db/README.md)
GRANT SELECT ON fyniq.document, fyniq.document_version
  TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
GRANT INSERT (type), UPDATE (status, current_version) ON fyniq.document TO fyniq_api, fyniq_executor;
GRANT INSERT (document_id, version, payload) ON fyniq.document_version TO fyniq_api, fyniq_executor;
