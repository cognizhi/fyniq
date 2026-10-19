-- Document numbers (PRD-001 FR-ORG-04, rev 0.30; p1-organisation-chart task 2.2).
--
-- A document type has one number sequence: its prefix, the next number, how many digits it pads to, and
-- whether it is gap-free. A gap-free sequence numbers a document in the transaction that posts it, under
-- the sequence row's lock, which ENG-47 takes after the documents' and before the ledger revision's, so a
-- posting that fails takes no number and two postings never take the same one. A sequence that is not
-- gap-free numbers a document when it is saved, so an abandoned draft leaves a gap.
--
-- A number is a row of document_number, never changed or deleted: unique within its organisation and
-- document type, and pointing at its document. Sequences are settings, audited like the organisation's
-- other settings; a number is audited as an event on its document.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.document_sequence (
  organisation_id uuid    NOT NULL DEFAULT fyniq.current_org(),
  id              uuid    NOT NULL DEFAULT uuidv7(),
  document_type   text    NOT NULL,
  prefix          text    NOT NULL,
  next_number     bigint  NOT NULL DEFAULT 1,
  digits          smallint NOT NULL DEFAULT 4,
  gap_free        boolean NOT NULL DEFAULT true,
  version         integer NOT NULL DEFAULT 1,
  CONSTRAINT document_sequence_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT document_sequence_type_key UNIQUE (organisation_id, document_type),
  CONSTRAINT document_sequence_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT document_sequence_type_known CHECK (document_type IN ('journal')),
  CONSTRAINT document_sequence_prefix_format CHECK (prefix ~ '^[A-Z0-9][A-Z0-9/-]{0,11}$'),
  CONSTRAINT document_sequence_next_positive CHECK (next_number BETWEEN 1 AND 999999999999),
  CONSTRAINT document_sequence_digits_range CHECK (digits BETWEEN 1 AND 12),
  CONSTRAINT document_sequence_version_positive CHECK (version > 0)
);

CREATE TABLE fyniq.document_number (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL,  -- the document's ID
  document_type   text        NOT NULL,
  number          text        NOT NULL,
  created_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT document_number_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT document_number_unique UNIQUE (organisation_id, document_type, number),
  CONSTRAINT document_number_document_fkey FOREIGN KEY (organisation_id, id)
    REFERENCES fyniq.document (organisation_id, id),
  CONSTRAINT document_number_length CHECK (length(number) BETWEEN 1 AND 30)
);

-- A sequence's next number never goes back, so a number is never issued twice; its type never changes.
-- +goose StatementBegin
CREATE FUNCTION fyniq.document_sequence_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF NEW.document_type IS DISTINCT FROM OLD.document_type THEN
    RAISE EXCEPTION 'The document type of a number sequence cannot change' USING ERRCODE = 'FY090';
  END IF;
  IF NEW.next_number < OLD.next_number THEN
    RAISE EXCEPTION 'The next % number cannot go back from % to %', OLD.document_type, OLD.next_number, NEW.next_number
      USING ERRCODE = 'FY090',
            HINT = 'A number is never issued twice (FR-ORG-04): move the next number forward, or leave it.';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER document_sequence_guard BEFORE UPDATE ON fyniq.document_sequence
  FOR EACH ROW EXECUTE FUNCTION fyniq.document_sequence_guard();

ALTER TABLE fyniq.document_sequence ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.document_sequence FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.document_sequence
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.document_number ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.document_number FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.document_number
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

-- A change to a sequence's settings is audited; taking a number is audited as an event on its document.
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE OF prefix, digits, gap_free, version ON fyniq.document_sequence
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('document_sequence', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.document_number
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('document', 'id');

-- Every role that creates documents numbers them: the API, and the executor for approved proposals. Only the
-- API changes a sequence's settings. The worker creates no documents yet, so it only reads; its scheduled
-- postings gain the grant with the change that adds them. Numbers are insert-only.
GRANT SELECT ON fyniq.document_sequence, fyniq.document_number TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
GRANT INSERT (document_type, prefix, next_number, digits, gap_free),
      UPDATE (prefix, next_number, digits, gap_free, version) ON fyniq.document_sequence TO fyniq_api;
-- The executor sets a sequence up only with its defaults, and then only takes numbers.
GRANT INSERT (document_type, prefix, next_number, digits, gap_free), UPDATE (next_number) ON fyniq.document_sequence TO fyniq_executor;
GRANT INSERT (id, document_type, number) ON fyniq.document_number TO fyniq_api, fyniq_executor;
