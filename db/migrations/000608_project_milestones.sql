-- Milestones of a fixed-fee project and the periods of a retainer (PRD-001 FR-PRJ-10; p1-projects task 5.3, design D9).
--
-- A fixed-fee project bills by milestone: a name, an amount in the project's currency, a due date, and the day the customer accepted it;
-- an accepted milestone can be invoiced, once. A retainer bills its monthly amount the same way, a period a row, without acceptance, and
-- draws time down against what was billed. An internal project has none (FY607). A milestone's amount and name never change once it
-- is saved; only its acceptance is recorded, once. The invoice that bills one marks it through the trigger of migration 000606, which
-- now also reads milestone_id.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.project_milestone (
  organisation_id uuid          NOT NULL DEFAULT fyniq.current_org(),
  id              uuid          NOT NULL DEFAULT uuidv7(),
  project_id      uuid          NOT NULL,
  kind            text          NOT NULL,
  name            text          NOT NULL,
  amount          numeric(20,4) NOT NULL,
  due_on          date,
  accepted_on     date,
  version         integer       NOT NULL DEFAULT 1,
  created_at      timestamptz   NOT NULL DEFAULT now(),
  changed_at      timestamptz   NOT NULL DEFAULT now(),
  CONSTRAINT project_milestone_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT project_milestone_project_fkey FOREIGN KEY (organisation_id, project_id) REFERENCES fyniq.project (organisation_id, id),
  CONSTRAINT project_milestone_kind CHECK (kind IN ('milestone', 'retainer')),
  CONSTRAINT project_milestone_name_length CHECK (length(btrim(name)) BETWEEN 1 AND 100),
  CONSTRAINT project_milestone_amount CHECK (amount <> 'NaN' AND amount > 0),
  CONSTRAINT project_milestone_version_positive CHECK (version >= 1),
  CONSTRAINT project_milestone_retainer_unaccepted CHECK (kind = 'milestone' OR accepted_on IS NULL)
);
CREATE INDEX project_milestone_project_idx ON fyniq.project_milestone (organisation_id, project_id, created_at);

-- +goose StatementBegin
-- A milestone belongs to a fixed-fee project (kind milestone) or a retainer (kind retainer) that is not closed, keeps its name and amount,
-- is accepted once, and is never deleted (FY607).
CREATE FUNCTION fyniq.project_milestone_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  p record;
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A milestone is never deleted: an invoice may bill it' USING ERRCODE = 'FY607';
  END IF;
  IF TG_OP = 'INSERT' THEN
    SELECT status, billing_method INTO p FROM fyniq.project WHERE organisation_id = NEW.organisation_id AND id = NEW.project_id;
    IF NOT FOUND THEN
      RETURN NEW; -- the foreign key refuses a project that does not exist
    END IF;
    IF p.status = 'closed' THEN
      RAISE EXCEPTION 'A closed project takes no new milestone' USING ERRCODE = 'FY607', HINT = 'Reopen the project first';
    END IF;
    IF (NEW.kind = 'milestone' AND p.billing_method IS DISTINCT FROM 'fixed_fee') OR (NEW.kind = 'retainer' AND p.billing_method IS DISTINCT FROM 'retainer') THEN
      RAISE EXCEPTION 'A % belongs to a % project', NEW.kind, CASE NEW.kind WHEN 'milestone' THEN 'fixed-fee' ELSE 'retainer' END USING ERRCODE = 'FY607';
    END IF;
    IF NEW.version <> 1 OR NEW.accepted_on IS NOT NULL THEN
      RAISE EXCEPTION 'A new milestone starts at version 1, not accepted' USING ERRCODE = 'FY607';
    END IF;
  ELSE
    IF (NEW.project_id, NEW.kind, NEW.name, NEW.amount, NEW.created_at) IS DISTINCT FROM (OLD.project_id, OLD.kind, OLD.name, OLD.amount, OLD.created_at)
       OR NEW.version <> OLD.version + 1 THEN
      RAISE EXCEPTION 'A milestone keeps its name and amount and changes one version at a time' USING ERRCODE = 'FY607';
    END IF;
    IF OLD.accepted_on IS NOT NULL AND NEW.accepted_on IS DISTINCT FROM OLD.accepted_on THEN
      RAISE EXCEPTION 'A milestone is accepted once' USING ERRCODE = 'FY607';
    END IF;
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER project_milestone_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.project_milestone
  FOR EACH ROW EXECUTE FUNCTION fyniq.project_milestone_guard();

ALTER TABLE fyniq.project_milestone ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.project_milestone FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.project_milestone
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.project_milestone
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('project', 'project_id');

GRANT SELECT ON fyniq.project_milestone TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
GRANT INSERT (project_id, kind, name, amount, due_on) ON fyniq.project_milestone TO fyniq_api;
GRANT UPDATE (accepted_on, version, changed_at) ON fyniq.project_milestone TO fyniq_api;

-- The invoice trigger now marks a milestone it bills (FY606): an accepted milestone, or a retainer's period, of the invoice's project.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.project_billing_follows_invoice() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  org uuid := fyniq.require_org();
  pb  jsonb;
  pid uuid;
  e   jsonb;
  ok  integer;
  mid uuid;
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
  IF pb ? 'milestone_id' THEN
    mid := (pb ->> 'milestone_id')::uuid;
    SELECT count(*) INTO ok FROM fyniq.project_milestone m
     WHERE m.organisation_id = org AND m.id = mid AND m.project_id = pid AND (m.kind = 'retainer' OR m.accepted_on IS NOT NULL);
    IF ok = 0 THEN
      RAISE EXCEPTION 'Milestone % is not an accepted milestone or a retainer period of the project', mid USING ERRCODE = 'FY606',
        HINT = 'Record the customer''s acceptance first';
    END IF;
    BEGIN
      INSERT INTO fyniq.project_billed_item (organisation_id, project_id, kind, ref_id, invoice_id) VALUES (org, pid, 'milestone', mid, NEW.id);
    EXCEPTION WHEN unique_violation THEN
      RAISE EXCEPTION 'Milestone % is already billed on another invoice', mid USING ERRCODE = 'FY606',
        HINT = 'Void the invoice that billed it, or take it off this one';
    END;
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
