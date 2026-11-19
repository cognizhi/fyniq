-- What was billed to a customer from a project (PRD-001 FR-PRJ-10; p1-projects task 5.2, design D9).
--
-- A draft invoice made from a project's unbilled work carries a project_billing object in its payload: the approved time entries and
-- the posted billable expense lines it bills. The database marks them billed when the invoice is posted and releases them when it is
-- voided, whichever way the invoice is issued, approved or voided (INV, FY606): an entry or a line is billed once, and its invoice is
-- the one that billed it. Nothing writes the marks but this trigger.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.project_billed_item (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  project_id      uuid        NOT NULL,
  kind            text        NOT NULL,
  ref_id          uuid        NOT NULL,
  line_no         integer     NOT NULL DEFAULT 0,
  invoice_id      uuid        NOT NULL,
  created_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT project_billed_item_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT project_billed_item_once UNIQUE (organisation_id, kind, ref_id, line_no),
  CONSTRAINT project_billed_item_project_fkey FOREIGN KEY (organisation_id, project_id) REFERENCES fyniq.project (organisation_id, id),
  CONSTRAINT project_billed_item_invoice_fkey FOREIGN KEY (organisation_id, invoice_id) REFERENCES fyniq.document (organisation_id, id),
  CONSTRAINT project_billed_item_kind CHECK (kind IN ('time', 'expense', 'milestone')),
  CONSTRAINT project_billed_item_line CHECK (line_no >= 0)
);
CREATE INDEX project_billed_item_invoice_idx ON fyniq.project_billed_item (organisation_id, invoice_id);
CREATE INDEX project_billed_item_project_idx ON fyniq.project_billed_item (organisation_id, project_id, kind);

ALTER TABLE fyniq.project_billed_item ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.project_billed_item FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.project_billed_item
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

-- Read only for every role: the marks are written by the trigger below, as the invoice's own posting.
GRANT SELECT ON fyniq.project_billed_item TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;

-- An invoice posted marks what it bills; one voided releases it (FY606). The payload is the invoice's current version.
-- +goose StatementBegin
CREATE FUNCTION fyniq.project_billing_follows_invoice() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  org uuid := fyniq.require_org();
  pb  jsonb;
  pid uuid;
  e   jsonb;
  ok  integer;
BEGIN
  IF NEW.status = 'reversed' THEN
    DELETE FROM fyniq.project_billed_item WHERE organisation_id = org AND invoice_id = NEW.id;
    RETURN NULL;
  END IF;
  SELECT payload -> 'project_billing' INTO pb FROM fyniq.document_version
   WHERE organisation_id = org AND document_id = NEW.id AND version = NEW.current_version;
  IF pb IS NULL OR jsonb_typeof(pb) <> 'object' THEN
    RETURN NULL;
  END IF;
  pid := (pb ->> 'project_id')::uuid;
  FOR e IN SELECT value FROM jsonb_array_elements(COALESCE(pb -> 'time_entries', '[]'::jsonb)) LOOP
    SELECT count(*) INTO ok FROM fyniq.time_entry t
     WHERE t.organisation_id = org AND t.id = (e #>> '{}')::uuid AND t.project_id = pid AND t.status = 'approved' AND t.billable;
    IF ok = 0 THEN
      RAISE EXCEPTION 'Time entry % is not approved billable time of the project', e #>> '{}' USING ERRCODE = 'FY606',
        HINT = 'Bill approved, billable time of the project only';
    END IF;
    BEGIN
      INSERT INTO fyniq.project_billed_item (organisation_id, project_id, kind, ref_id, invoice_id)
        VALUES (org, pid, 'time', (e #>> '{}')::uuid, NEW.id);
    EXCEPTION WHEN unique_violation THEN
      RAISE EXCEPTION 'Time entry % is already billed on another invoice', e #>> '{}' USING ERRCODE = 'FY606',
        HINT = 'Void the invoice that billed it, or take it off this one';
    END;
  END LOOP;
  FOR e IN SELECT value FROM jsonb_array_elements(COALESCE(pb -> 'expenses', '[]'::jsonb)) LOOP
    SELECT count(*) INTO ok FROM fyniq.document d
     WHERE d.organisation_id = org AND d.id = (e ->> 'document_id')::uuid AND d.status = 'posted' AND d.type IN ('bill', 'expense_claim');
    IF ok = 0 THEN
      RAISE EXCEPTION 'Expense % is not a posted bill or claim', e ->> 'document_id' USING ERRCODE = 'FY606',
        HINT = 'Bill the costs of posted bills and claims only';
    END IF;
    BEGIN
      INSERT INTO fyniq.project_billed_item (organisation_id, project_id, kind, ref_id, line_no, invoice_id)
        VALUES (org, pid, 'expense', (e ->> 'document_id')::uuid, (e ->> 'line')::integer, NEW.id);
    EXCEPTION WHEN unique_violation THEN
      RAISE EXCEPTION 'Expense line % of % is already billed on another invoice', e ->> 'line', e ->> 'document_id' USING ERRCODE = 'FY606',
        HINT = 'Void the invoice that billed it, or take it off this one';
    END;
  END LOOP;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER project_billing_follows_invoice AFTER UPDATE OF status ON fyniq.document
  FOR EACH ROW WHEN (NEW.type = 'invoice' AND OLD.status IS DISTINCT FROM NEW.status AND NEW.status IN ('posted', 'reversed'))
  EXECUTE FUNCTION fyniq.project_billing_follows_invoice();
