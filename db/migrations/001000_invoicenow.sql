-- InvoiceNow submissions (PRD-001 FR-AR-12, FR-AP-05, FR-TAX-10, FR-TAX-11; p2-invoicenow task 4.1): one submission for each
-- (document, version) of a document that feeds the GST return, with its status up to IRAS's acknowledgement ID and an
-- append-only history; the documents left out with their reason; and the contingency that lets a return be filed with a
-- gap (requested by one person, approved by another).
--
-- A submission moves only along queued -> sent -> acknowledged, or to failed, and back from failed to queued (FYI01); once
-- acknowledged it never changes. Its document, version, UUID and kind never change (FYI01). History is never changed or
-- removed (FYI02). A document has one live exclusion at a time; a withdrawal ends it once (FYI03). A contingency's
-- approver is never its requester (FYI04).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.invoicenow_submission (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  document_id     uuid        NOT NULL,   -- the invoice, credit note or bill the submission is for
  document_uuid   uuid        NOT NULL,   -- BT-SG-003: the same for every version of the document
  version         integer     NOT NULL,   -- 1, then one more for each correction
  kind            text        NOT NULL,   -- peppol, extracted_sale, extracted_purchase or aggregated (FR-TAX-10)
  direction       text        NOT NULL,   -- sale or purchase
  status          text        NOT NULL DEFAULT 'queued',
  recipient       text,                   -- the buyer's participant ID, for a Peppol submission
  transmission_id text,                   -- the access point's
  iras_ack        text,                   -- IRAS's acknowledgement ID
  last_error      text,
  payload_sha256  bytea,                  -- of the exact document sent
  created_at      timestamptz NOT NULL,
  updated_at      timestamptz NOT NULL,
  created_by      text        NOT NULL,
  CONSTRAINT invoicenow_submission_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT invoicenow_submission_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT invoicenow_submission_document_fkey FOREIGN KEY (organisation_id, document_id) REFERENCES fyniq.document (organisation_id, id),
  CONSTRAINT invoicenow_submission_version_key UNIQUE (organisation_id, document_id, version),
  CONSTRAINT invoicenow_submission_version_positive CHECK (version > 0),
  CONSTRAINT invoicenow_submission_kind_known CHECK (kind IN ('peppol', 'extracted_sale', 'extracted_purchase', 'aggregated')),
  CONSTRAINT invoicenow_submission_direction_known CHECK (direction IN ('sale', 'purchase')),
  CONSTRAINT invoicenow_submission_status_known CHECK (status IN ('queued', 'sent', 'acknowledged', 'failed')),
  CONSTRAINT invoicenow_submission_direction_kind CHECK ((kind = 'extracted_purchase') = (direction = 'purchase') OR kind = 'aggregated'),
  CONSTRAINT invoicenow_submission_recipient_form CHECK (recipient IS NULL OR recipient ~ '^[0-9]{4}:[!-~]{1,100}$'),
  CONSTRAINT invoicenow_submission_peppol_recipient CHECK (kind <> 'peppol' OR recipient IS NOT NULL),
  CONSTRAINT invoicenow_submission_ack_with_status CHECK ((status = 'acknowledged') = (iras_ack IS NOT NULL AND length(iras_ack) BETWEEN 1 AND 100)),
  CONSTRAINT invoicenow_submission_error_length CHECK (last_error IS NULL OR length(last_error) <= 1000),
  CONSTRAINT invoicenow_submission_sha CHECK (payload_sha256 IS NULL OR octet_length(payload_sha256) = 32),
  CONSTRAINT invoicenow_submission_by_format CHECK (created_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);
CREATE INDEX invoicenow_submission_status ON fyniq.invoicenow_submission (organisation_id, status, created_at);
CREATE INDEX invoicenow_submission_uuid ON fyniq.invoicenow_submission (organisation_id, document_uuid);

CREATE TABLE fyniq.invoicenow_event (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  submission_id   uuid        NOT NULL,
  at              timestamptz NOT NULL,
  status          text        NOT NULL,
  detail          text,
  actor           text        NOT NULL,
  CONSTRAINT invoicenow_event_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT invoicenow_event_submission_fkey FOREIGN KEY (organisation_id, submission_id) REFERENCES fyniq.invoicenow_submission (organisation_id, id),
  CONSTRAINT invoicenow_event_status_known CHECK (status IN ('queued', 'sent', 'acknowledged', 'failed')),
  CONSTRAINT invoicenow_event_detail_length CHECK (detail IS NULL OR length(detail) <= 1000),
  CONSTRAINT invoicenow_event_actor_format CHECK (actor ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);
CREATE INDEX invoicenow_event_submission ON fyniq.invoicenow_event (organisation_id, submission_id, at);

CREATE TABLE fyniq.invoicenow_exclusion (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  document_id     uuid        NOT NULL,
  reason          text        NOT NULL,
  detail          text,
  recorded_by     text        NOT NULL,
  recorded_at     timestamptz NOT NULL,
  withdrawn_at    timestamptz,
  withdrawn_by    text,
  CONSTRAINT invoicenow_exclusion_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT invoicenow_exclusion_document_fkey FOREIGN KEY (organisation_id, document_id) REFERENCES fyniq.document (organisation_id, id),
  CONSTRAINT invoicenow_exclusion_reason_known CHECK (reason IN ('reverse_charge', 'import_permit', 'deemed_supply', 'financial_services',
    'unregistered_supplier', 'blocked_input_tax', 'out_of_scope', 'other')),
  CONSTRAINT invoicenow_exclusion_other_says CHECK (reason <> 'other' OR length(btrim(coalesce(detail, ''))) >= 3),
  CONSTRAINT invoicenow_exclusion_detail_length CHECK (detail IS NULL OR length(detail) <= 500),
  CONSTRAINT invoicenow_exclusion_by_format CHECK (recorded_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$'
    AND (withdrawn_by IS NULL OR withdrawn_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')),
  CONSTRAINT invoicenow_exclusion_withdrawn_whole CHECK ((withdrawn_at IS NULL) = (withdrawn_by IS NULL))
);
CREATE UNIQUE INDEX invoicenow_exclusion_live ON fyniq.invoicenow_exclusion (organisation_id, document_id) WHERE withdrawn_at IS NULL;

CREATE TABLE fyniq.invoicenow_contingency (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  period_from     date        NOT NULL,
  period_to       date        NOT NULL,
  reason          text        NOT NULL,   -- why the return cannot wait for the missing submissions
  evidence        text        NOT NULL,   -- the outage and any IRAS correspondence, as the person records them
  requested_by    text        NOT NULL,
  requested_at    timestamptz NOT NULL,
  approved_by     text,
  approved_at     timestamptz,
  withdrawn_at    timestamptz,
  CONSTRAINT invoicenow_contingency_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT invoicenow_contingency_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT invoicenow_contingency_period CHECK (period_to >= period_from),
  CONSTRAINT invoicenow_contingency_reason_length CHECK (length(btrim(reason)) BETWEEN 10 AND 1000),
  CONSTRAINT invoicenow_contingency_evidence_length CHECK (length(btrim(evidence)) BETWEEN 10 AND 4000),
  CONSTRAINT invoicenow_contingency_by_format CHECK (requested_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$'
    AND (approved_by IS NULL OR approved_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')),
  CONSTRAINT invoicenow_contingency_approver_differs CHECK (approved_by IS NULL OR approved_by <> requested_by),
  CONSTRAINT invoicenow_contingency_approved_whole CHECK ((approved_at IS NULL) = (approved_by IS NULL))
);
CREATE INDEX invoicenow_contingency_period ON fyniq.invoicenow_contingency (organisation_id, period_from, period_to);

-- A submission's identity and kind never change; its status moves along the legal path only; an acknowledged one is
-- final (FYI01).
-- +goose StatementBegin
CREATE FUNCTION fyniq.invoicenow_submission_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE'
     OR (NEW.document_id, NEW.document_uuid, NEW.version, NEW.kind, NEW.direction, NEW.created_at, NEW.created_by)
        IS DISTINCT FROM (OLD.document_id, OLD.document_uuid, OLD.version, OLD.kind, OLD.direction, OLD.created_at, OLD.created_by)
     OR OLD.status = 'acknowledged'
     OR (NEW.status <> OLD.status AND NOT ((OLD.status, NEW.status) IN (('queued', 'sent'), ('queued', 'failed'), ('sent', 'acknowledged'),
                                                                        ('sent', 'failed'), ('failed', 'queued')))) THEN
    RAISE EXCEPTION 'A submission moves only from queued to sent to acknowledged, or to failed and back to queued' USING ERRCODE = 'FYI01',
      HINT = 'Queue the document again, or send a corrected version (FR-TAX-10).';
  END IF;
  RETURN NEW;
END
$$;

CREATE FUNCTION fyniq.invoicenow_event_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'A submission''s history never changes or goes' USING ERRCODE = 'FYI02', HINT = 'It is the record of what IRAS and the access point said (FR-TAX-10).';
END
$$;

CREATE FUNCTION fyniq.invoicenow_exclusion_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR OLD.withdrawn_at IS NOT NULL OR NEW.withdrawn_at IS NULL
     OR (NEW.document_id, NEW.reason, NEW.detail, NEW.recorded_by, NEW.recorded_at) IS DISTINCT FROM
        (OLD.document_id, OLD.reason, OLD.detail, OLD.recorded_by, OLD.recorded_at) THEN
    RAISE EXCEPTION 'An exclusion changes only in being withdrawn, once' USING ERRCODE = 'FYI03', HINT = 'Withdraw it and record another (FR-TAX-10).';
  END IF;
  RETURN NEW;
END
$$;

-- A contingency is requested, then approved once by someone else, or withdrawn; what it says never changes (FYI04).
CREATE FUNCTION fyniq.invoicenow_contingency_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR OLD.withdrawn_at IS NOT NULL
     OR (NEW.period_from, NEW.period_to, NEW.reason, NEW.evidence, NEW.requested_by, NEW.requested_at)
        IS DISTINCT FROM (OLD.period_from, OLD.period_to, OLD.reason, OLD.evidence, OLD.requested_by, OLD.requested_at)
     OR (OLD.approved_by IS NOT NULL AND (NEW.approved_by, NEW.approved_at) IS DISTINCT FROM (OLD.approved_by, OLD.approved_at))
     OR (NEW.approved_by IS NOT NULL AND NEW.approved_by = NEW.requested_by) THEN
    RAISE EXCEPTION 'A contingency is approved once, by someone other than its requester, and then only withdrawn' USING ERRCODE = 'FYI04',
      HINT = 'Ask another person with the right to approve (FR-TAX-11).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER invoicenow_submission_guard BEFORE UPDATE OR DELETE ON fyniq.invoicenow_submission
  FOR EACH ROW EXECUTE FUNCTION fyniq.invoicenow_submission_guard();
CREATE TRIGGER invoicenow_event_guard BEFORE UPDATE OR DELETE ON fyniq.invoicenow_event
  FOR EACH ROW EXECUTE FUNCTION fyniq.invoicenow_event_guard();
CREATE TRIGGER invoicenow_exclusion_guard BEFORE UPDATE OR DELETE ON fyniq.invoicenow_exclusion
  FOR EACH ROW EXECUTE FUNCTION fyniq.invoicenow_exclusion_guard();
CREATE TRIGGER invoicenow_contingency_guard BEFORE UPDATE OR DELETE ON fyniq.invoicenow_contingency
  FOR EACH ROW EXECUTE FUNCTION fyniq.invoicenow_contingency_guard();
CREATE TRIGGER invoicenow_submission_no_truncate BEFORE TRUNCATE ON fyniq.invoicenow_submission
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();
CREATE TRIGGER invoicenow_event_no_truncate BEFORE TRUNCATE ON fyniq.invoicenow_event
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();
CREATE TRIGGER invoicenow_exclusion_no_truncate BEFORE TRUNCATE ON fyniq.invoicenow_exclusion
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();
CREATE TRIGGER invoicenow_contingency_no_truncate BEFORE TRUNCATE ON fyniq.invoicenow_contingency
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();

ALTER TABLE fyniq.invoicenow_submission ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.invoicenow_submission FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.invoicenow_submission
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.invoicenow_event ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.invoicenow_event FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.invoicenow_event
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.invoicenow_exclusion ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.invoicenow_exclusion FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.invoicenow_exclusion
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.invoicenow_contingency ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.invoicenow_contingency FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.invoicenow_contingency
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

-- Every change is audited in its transaction (AUD-01).
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.invoicenow_submission
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('invoicenow_submission', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.invoicenow_exclusion
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('invoicenow_exclusion', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.invoicenow_contingency
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('invoicenow_contingency', 'id');

GRANT SELECT ON fyniq.invoicenow_submission, fyniq.invoicenow_event, fyniq.invoicenow_exclusion, fyniq.invoicenow_contingency
  TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (document_id, document_uuid, version, kind, direction, recipient, payload_sha256, created_at, updated_at, created_by)
  ON fyniq.invoicenow_submission TO fyniq_api;
GRANT UPDATE (status, transmission_id, iras_ack, last_error, updated_at) ON fyniq.invoicenow_submission TO fyniq_api, fyniq_worker;
GRANT INSERT (submission_id, at, status, detail, actor) ON fyniq.invoicenow_event TO fyniq_api, fyniq_worker;
GRANT INSERT (document_id, reason, detail, recorded_by, recorded_at) ON fyniq.invoicenow_exclusion TO fyniq_api;
GRANT UPDATE (withdrawn_at, withdrawn_by) ON fyniq.invoicenow_exclusion TO fyniq_api;
GRANT INSERT (period_from, period_to, reason, evidence, requested_by, requested_at) ON fyniq.invoicenow_contingency TO fyniq_api;
GRANT UPDATE (approved_by, approved_at, withdrawn_at) ON fyniq.invoicenow_contingency TO fyniq_api;
