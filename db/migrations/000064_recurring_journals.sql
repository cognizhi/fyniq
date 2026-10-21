-- Recurring journals (PRD-001 FR-GL-02, FR-AUT-01, FR-AUT-02; founder decisions of rev 0.58, built in rev 0.60;
-- p1-general-ledger-close task 2.4).
--
-- A recurring journal is a journal's content (memo, lines, whether each one reverses on the first day of the next
-- month) and a schedule: every 1, 3, 6 or 12 months on a day of the month, the 29th to the 31st meaning the last day
-- of a shorter month (FR-AR-07's rule), from a start date to an optional end. People keep it at its version, as an
-- item is kept; each change of its content is a new, immutable version that names who wrote it, so a draft made from
-- it says which version it came from and SOD-3 can tell who wrote it.
--
-- The scheduler makes each occurrence's journal as a DRAFT, which a person posts under the usual approvals: nothing
-- recurring posts by itself (the founders' decision of 21 Oct 2026). The worker's role gets no INSERT on documents:
-- it calls create_scheduled_draft, which makes the draft journal from the recurring journal's current version, dated
-- on its next occurrence, saved by "scheduler:journals.recurring", once per occurrence, and nothing else. The draft
-- is a version-1 draft journal with no number: it takes its number when it posts. The worker may then move next_on
-- and read both tables; it never changes a recurring journal's content or schedule.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.recurring_journal (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  name            text        NOT NULL,
  status          text        NOT NULL DEFAULT 'active',
  every_months    integer     NOT NULL,
  day_of_month    integer     NOT NULL,
  starts_on       date        NOT NULL,
  ends_on         date,
  next_on         date,
  version         integer     NOT NULL DEFAULT 1,
  content_version integer     NOT NULL DEFAULT 1,
  last_on         date,
  last_journal_id uuid,
  created_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT recurring_journal_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT recurring_journal_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT recurring_journal_last_journal_fkey FOREIGN KEY (organisation_id, last_journal_id)
    REFERENCES fyniq.document (organisation_id, id),
  CONSTRAINT recurring_journal_name_length CHECK (length(btrim(name)) BETWEEN 1 AND 120),
  CONSTRAINT recurring_journal_status_known CHECK (status IN ('active', 'paused')),
  CONSTRAINT recurring_journal_every_known CHECK (every_months IN (1, 3, 6, 12)),
  CONSTRAINT recurring_journal_day_valid CHECK (day_of_month BETWEEN 1 AND 31),
  CONSTRAINT recurring_journal_ends_after_start CHECK (ends_on IS NULL OR ends_on >= starts_on),
  CONSTRAINT recurring_journal_next_in_range CHECK (next_on IS NULL OR (next_on >= starts_on AND (ends_on IS NULL OR next_on <= ends_on))),
  CONSTRAINT recurring_journal_versions_positive CHECK (version > 0 AND content_version > 0)
);

CREATE TABLE fyniq.recurring_journal_version (
  organisation_id      uuid        NOT NULL DEFAULT fyniq.current_org(),
  id                   uuid        NOT NULL DEFAULT uuidv7(),
  recurring_journal_id uuid        NOT NULL,
  version              integer     NOT NULL,
  content              jsonb       NOT NULL,
  edited_by            text        NOT NULL,
  edited_at            timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT recurring_journal_version_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT recurring_journal_version_key UNIQUE (organisation_id, recurring_journal_id, version),
  CONSTRAINT recurring_journal_version_journal_fkey FOREIGN KEY (organisation_id, recurring_journal_id)
    REFERENCES fyniq.recurring_journal (organisation_id, id),
  CONSTRAINT recurring_journal_version_positive CHECK (version > 0),
  CONSTRAINT recurring_journal_version_content_object CHECK (jsonb_typeof(content) = 'object' AND NOT content ? 'date'),
  CONSTRAINT recurring_journal_version_content_size CHECK (octet_length(content::text) <= 1048576),
  CONSTRAINT recurring_journal_version_edited_by_format CHECK (edited_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);

-- The current content version exists, checked at commit: the row and its first version are written together.
ALTER TABLE fyniq.recurring_journal ADD CONSTRAINT recurring_journal_content_version_fkey
  FOREIGN KEY (organisation_id, id, content_version)
  REFERENCES fyniq.recurring_journal_version (organisation_id, recurring_journal_id, version)
  DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE fyniq.recurring_journal ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.recurring_journal FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.recurring_journal
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.recurring_journal_version ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.recurring_journal_version FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.recurring_journal_version
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.recurring_journal
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('recurring_journal', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.recurring_journal_version
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('recurring_journal', 'recurring_journal_id');

-- A version never changes or goes, whoever tries (FY058).
-- +goose StatementBegin
CREATE FUNCTION fyniq.recurring_journal_version_immutable() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'Version % of recurring journal % never changes', OLD.version, OLD.recurring_journal_id
    USING ERRCODE = 'FY058', HINT = 'Save the recurring journal again: a change is a new version (FR-GL-02).';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER recurring_journal_version_immutable BEFORE UPDATE OR DELETE ON fyniq.recurring_journal_version
  FOR EACH ROW EXECUTE FUNCTION fyniq.recurring_journal_version_immutable();

-- The scheduler's one way to make a document (FR-GL-02, FR-AUT-02; FY057): the draft journal of an active recurring
-- journal's next occurrence, from its current version, saved by the scheduler's principal, once per occurrence. It
-- records the occurrence as the last one made; the caller moves next_on and records the audit events.
-- +goose StatementBegin
CREATE FUNCTION fyniq.create_scheduled_draft(p_recurring_journal uuid, p_on date) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  org     uuid := fyniq.require_org();
  r       fyniq.recurring_journal;
  content jsonb;
  body    jsonb;
  doc     uuid;
BEGIN
  SELECT * INTO r FROM fyniq.recurring_journal
   WHERE organisation_id = org AND id = p_recurring_journal FOR UPDATE;
  IF NOT FOUND OR r.status <> 'active' OR r.next_on IS NULL OR r.next_on <> p_on
     OR (r.last_on IS NOT NULL AND p_on <= r.last_on) THEN
    RAISE EXCEPTION 'Recurring journal % has no active occurrence on %', p_recurring_journal, p_on
      USING ERRCODE = 'FY057', HINT = 'The scheduler makes the draft of an active recurring journal''s next occurrence, once (FR-GL-02).';
  END IF;
  SELECT v.content INTO content FROM fyniq.recurring_journal_version v
   WHERE v.organisation_id = org AND v.recurring_journal_id = r.id AND v.version = r.content_version;
  body := (content - 'reverses') || jsonb_build_object('date', to_char(p_on, 'YYYY-MM-DD'),
            'recurring_journal', jsonb_build_object('id', r.id, 'version', r.content_version));
  -- One that reverses does so on the first day of the next month (FR-GL-02's accrual).
  IF content->>'reverses' = 'true' THEN
    body := body || jsonb_build_object('reverse_on', to_char(date_trunc('month', p_on) + interval '1 month', 'YYYY-MM-DD'));
  END IF;
  INSERT INTO fyniq.document (organisation_id, type) VALUES (org, 'journal') RETURNING id INTO doc;
  INSERT INTO fyniq.document_version (organisation_id, document_id, version, payload, created_by)
    VALUES (org, doc, 1, body, 'scheduler:journals.recurring');
  UPDATE fyniq.recurring_journal SET last_on = p_on, last_journal_id = doc
   WHERE organisation_id = org AND id = r.id;
  RETURN doc;
END
$$;
-- +goose StatementEnd

GRANT SELECT ON fyniq.recurring_journal, fyniq.recurring_journal_version TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
GRANT INSERT (name, status, every_months, day_of_month, starts_on, ends_on, next_on),
      UPDATE (name, status, every_months, day_of_month, starts_on, ends_on, next_on, version, content_version)
  ON fyniq.recurring_journal TO fyniq_api;
GRANT INSERT (recurring_journal_id, version, content, edited_by) ON fyniq.recurring_journal_version TO fyniq_api;
GRANT UPDATE (next_on) ON fyniq.recurring_journal TO fyniq_worker;
GRANT EXECUTE ON FUNCTION fyniq.create_scheduled_draft(uuid, date) TO fyniq_worker;
