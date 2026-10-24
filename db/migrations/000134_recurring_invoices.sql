-- Recurring invoices (PRD-001 FR-AR-07, FR-AUT-01, FR-AUT-02; p1-sales task 6.1, part; the owner's decision of 24 Oct
-- 2026: each makes a draft by default).
--
-- A recurring invoice is an invoice's content (the customer, the reference, the currency, the lines and the notes) and
-- a schedule, kept as a recurring bill is (000090): every 1, 3, 6 or 12 months on a day of the month, the 29th to the
-- 31st meaning the last day of a shorter month, or every 1 or 2 weeks from its start's weekday (FR-AR-07: weekly to
-- yearly), from a start date to an optional end. Each change of its content is a new, immutable version that names who
-- wrote it, so a draft made from it says which version it came from and SOD-3 can tell who wrote it (FY200). Its
-- content never holds a date, a due date, a rate, deposits or a credited invoice: those come with each invoice.
--
-- The scheduler makes each occurrence's invoice as a DRAFT, which a person issues under the usual approvals. The
-- worker's role gets no INSERT on documents: it calls create_scheduled_invoice_draft, which makes the draft from the
-- current version, dated on the next occurrence, saved by "scheduler:invoices.recurring", once per occurrence, and
-- nothing else (FY199). Issue and send (the owner's decision, after a second person's approval) comes with its own
-- migration.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.recurring_invoice (
  organisation_id  uuid        NOT NULL DEFAULT fyniq.current_org(),
  id               uuid        NOT NULL DEFAULT uuidv7(),
  name             text        NOT NULL,
  status           text        NOT NULL DEFAULT 'active',
  every_months     integer     NOT NULL DEFAULT 0,
  every_weeks      integer     NOT NULL DEFAULT 0,
  day_of_month     integer     NOT NULL DEFAULT 0,
  starts_on        date        NOT NULL,
  ends_on          date,
  next_on          date,
  version          integer     NOT NULL DEFAULT 1,
  content_version  integer     NOT NULL DEFAULT 1,
  last_on          date,
  last_invoice_id  uuid,
  created_at       timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT recurring_invoice_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT recurring_invoice_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT recurring_invoice_last_invoice_fkey FOREIGN KEY (organisation_id, last_invoice_id)
    REFERENCES fyniq.document (organisation_id, id),
  CONSTRAINT recurring_invoice_name_length CHECK (length(btrim(name)) BETWEEN 1 AND 120),
  CONSTRAINT recurring_invoice_status_known CHECK (status IN ('active', 'paused')),
  CONSTRAINT recurring_invoice_every_known CHECK (
    (every_months IN (1, 3, 6, 12) AND every_weeks = 0 AND day_of_month BETWEEN 1 AND 31)
    OR (every_weeks IN (1, 2) AND every_months = 0 AND day_of_month = 0)),
  CONSTRAINT recurring_invoice_ends_after_start CHECK (ends_on IS NULL OR ends_on >= starts_on),
  CONSTRAINT recurring_invoice_next_in_range CHECK (next_on IS NULL OR (next_on >= starts_on AND (ends_on IS NULL OR next_on <= ends_on))),
  CONSTRAINT recurring_invoice_versions_positive CHECK (version > 0 AND content_version > 0)
);

CREATE TABLE fyniq.recurring_invoice_version (
  organisation_id      uuid        NOT NULL DEFAULT fyniq.current_org(),
  id                   uuid        NOT NULL DEFAULT uuidv7(),
  recurring_invoice_id uuid        NOT NULL,
  version              integer     NOT NULL,
  content              jsonb       NOT NULL,
  edited_by            text        NOT NULL,
  edited_at            timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT recurring_invoice_version_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT recurring_invoice_version_key UNIQUE (organisation_id, recurring_invoice_id, version),
  CONSTRAINT recurring_invoice_version_invoice_fkey FOREIGN KEY (organisation_id, recurring_invoice_id)
    REFERENCES fyniq.recurring_invoice (organisation_id, id),
  CONSTRAINT recurring_invoice_version_positive CHECK (version > 0),
  CONSTRAINT recurring_invoice_version_content_object CHECK (jsonb_typeof(content) = 'object'
    AND NOT content ?| ARRAY['date', 'due_date', 'fx_rate', 'fx_rate_source', 'invoice_id', 'deposits', 'recurring_invoice']),
  CONSTRAINT recurring_invoice_version_content_size CHECK (octet_length(content::text) <= 1048576),
  CONSTRAINT recurring_invoice_version_edited_by_format CHECK (edited_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);

-- The current content version exists, checked at commit: the row and its first version are written together.
ALTER TABLE fyniq.recurring_invoice ADD CONSTRAINT recurring_invoice_content_version_fkey
  FOREIGN KEY (organisation_id, id, content_version)
  REFERENCES fyniq.recurring_invoice_version (organisation_id, recurring_invoice_id, version)
  DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE fyniq.recurring_invoice ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.recurring_invoice FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.recurring_invoice
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.recurring_invoice_version ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.recurring_invoice_version FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.recurring_invoice_version
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.recurring_invoice
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('recurring_invoice', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.recurring_invoice_version
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('recurring_invoice', 'recurring_invoice_id');

-- A version never changes or goes, whoever tries (FY200).
-- +goose StatementBegin
CREATE FUNCTION fyniq.recurring_invoice_version_immutable() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'Version % of recurring invoice % never changes', OLD.version, OLD.recurring_invoice_id
    USING ERRCODE = 'FY200', HINT = 'Save the recurring invoice again: a change is a new version (FR-AR-07).';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER recurring_invoice_version_immutable BEFORE UPDATE OR DELETE ON fyniq.recurring_invoice_version
  FOR EACH ROW EXECUTE FUNCTION fyniq.recurring_invoice_version_immutable();
CREATE TRIGGER recurring_invoice_version_no_truncate BEFORE TRUNCATE ON fyniq.recurring_invoice_version
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();

-- The scheduler's one way to make an invoice (FR-AR-07, FR-AUT-02; FY199): the draft invoice of an active recurring
-- invoice's next occurrence, from its current version, saved by the scheduler's principal, once per occurrence. It
-- records the occurrence as the last one made; the caller moves next_on and records the audit events.
-- +goose StatementBegin
CREATE FUNCTION fyniq.create_scheduled_invoice_draft(p_recurring_invoice uuid, p_on date) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  org     uuid := fyniq.require_org();
  r       fyniq.recurring_invoice;
  content jsonb;
  doc     uuid;
BEGIN
  SELECT * INTO r FROM fyniq.recurring_invoice
   WHERE organisation_id = org AND id = p_recurring_invoice FOR UPDATE;
  IF NOT FOUND OR r.status <> 'active' OR r.next_on IS NULL OR r.next_on <> p_on
     OR (r.last_on IS NOT NULL AND p_on <= r.last_on) THEN
    RAISE EXCEPTION 'Recurring invoice % has no active occurrence on %', p_recurring_invoice, p_on
      USING ERRCODE = 'FY199', HINT = 'The scheduler makes the draft of an active recurring invoice''s next occurrence, once (FR-AR-07).';
  END IF;
  SELECT v.content INTO content FROM fyniq.recurring_invoice_version v
   WHERE v.organisation_id = org AND v.recurring_invoice_id = r.id AND v.version = r.content_version;
  INSERT INTO fyniq.document (organisation_id, type) VALUES (org, 'invoice') RETURNING id INTO doc;
  INSERT INTO fyniq.document_version (organisation_id, document_id, version, payload, created_by)
    VALUES (org, doc, 1, content || jsonb_build_object('date', to_char(p_on, 'YYYY-MM-DD'),
              'recurring_invoice', jsonb_build_object('id', r.id, 'version', r.content_version)),
            'scheduler:invoices.recurring');
  UPDATE fyniq.recurring_invoice SET last_on = p_on, last_invoice_id = doc
   WHERE organisation_id = org AND id = r.id;
  RETURN doc;
END
$$;
-- +goose StatementEnd

GRANT SELECT ON fyniq.recurring_invoice, fyniq.recurring_invoice_version TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
GRANT INSERT (name, status, every_months, every_weeks, day_of_month, starts_on, ends_on, next_on),
      UPDATE (name, status, every_months, every_weeks, day_of_month, starts_on, ends_on, next_on, version, content_version)
  ON fyniq.recurring_invoice TO fyniq_api;
GRANT INSERT (recurring_invoice_id, version, content, edited_by) ON fyniq.recurring_invoice_version TO fyniq_api;
GRANT UPDATE (next_on) ON fyniq.recurring_invoice TO fyniq_worker;
GRANT EXECUTE ON FUNCTION fyniq.create_scheduled_invoice_draft(uuid, date) TO fyniq_worker;
