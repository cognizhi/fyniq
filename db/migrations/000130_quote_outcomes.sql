-- The customer's answer to a quote or sales order, and its expiry (PRD-001 FR-AR-01; p1-sales task 4.1, part).
--
-- A sent quote or order is accepted or declined by the customer, recorded by a person (and, with the online link of
-- p1-sales task 5.3, by the customer), or expired by the scheduler once its validity date has passed with no answer.
-- Each quote has at most one outcome, which never changes or goes (FY193): a quote is cancelled by reversing its
-- document, never by changing its outcome. An outcome is only of a posted quote or order (FY194).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.quote_outcome (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  document_id     uuid        NOT NULL,
  outcome         text        NOT NULL,
  on_date         date        NOT NULL,
  name            text,
  note            text,
  recorded_by     text        NOT NULL,
  recorded_at     timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT quote_outcome_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT quote_outcome_once UNIQUE (organisation_id, document_id),
  CONSTRAINT quote_outcome_document_fkey FOREIGN KEY (organisation_id, document_id) REFERENCES fyniq.document (organisation_id, id),
  CONSTRAINT quote_outcome_known CHECK (outcome IN ('accepted', 'declined', 'expired')),
  CONSTRAINT quote_outcome_name_length CHECK (length(btrim(name)) BETWEEN 1 AND 200),
  CONSTRAINT quote_outcome_note_length CHECK (length(note) BETWEEN 1 AND 2000),
  CONSTRAINT quote_outcome_recorded_by_format CHECK (recorded_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);

ALTER TABLE fyniq.quote_outcome ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.quote_outcome FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.quote_outcome
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.quote_outcome
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('quote_outcome', 'id');

-- An outcome never changes or goes, whoever tries (FY193), and is only of a posted quote or order (FY194).
-- +goose StatementBegin
CREATE FUNCTION fyniq.quote_outcome_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  doc fyniq.document;
BEGIN
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'The outcome of quote % never changes', OLD.document_id
      USING ERRCODE = 'FY193', HINT = 'Cancel the quote instead: it reverses the document (FR-AR-01).';
  END IF;
  SELECT * INTO doc FROM fyniq.document WHERE organisation_id = NEW.organisation_id AND id = NEW.document_id;
  IF doc.type NOT IN ('quote', 'sales_order') OR doc.status <> 'posted' THEN
    RAISE EXCEPTION 'An outcome is only of a sent quote or sales order, not of a % that is %', doc.type, doc.status
      USING ERRCODE = 'FY194', HINT = 'Send the quote first (FR-AR-01).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER quote_outcome_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.quote_outcome
  FOR EACH ROW EXECUTE FUNCTION fyniq.quote_outcome_guard();

GRANT SELECT ON fyniq.quote_outcome TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
GRANT INSERT (document_id, outcome, on_date, name, note, recorded_by) ON fyniq.quote_outcome TO fyniq_api, fyniq_worker;
