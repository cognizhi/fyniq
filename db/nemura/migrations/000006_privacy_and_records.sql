-- Privacy and records (PRD-002 NM-SEC-03, NM-SEC-04, NM-SEC-05, NM-SEC-07; PRD-001 CMP-PD-04, CMP-PD-06).
--
-- Access to one's own file: a tracked request with a due date, HR's decision for each category of record (release, redact other
-- people's data, or withhold with a recorded exception), and the outcome in writing. Retention: a deletion that is due is a report
-- for review, never silent; a legal hold keeps a record; each deletion leaves proof of what went and when, without its content.
-- Transfers outside Singapore need a current assessment. The categories themselves (purpose, retention, restricting right) are
-- code and compliance/nemura-data-inventory.yaml, which a test keeps complete against this schema.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- ------------------------------------------------------------------ access requests (NM-SEC-07)
CREATE TABLE nemura.access_request (
  organisation_id  uuid        NOT NULL DEFAULT nemura.current_org(),
  id               uuid        NOT NULL DEFAULT uuidv7(),
  requester_id     uuid        NOT NULL,           -- the person who asks for their own data
  received_on      date        NOT NULL,
  due_on           date        NOT NULL,           -- 30 days after it was received (CMP-PD-04)
  extended_notice_on date,                         -- the day the person was told in writing it will take longer
  handled_by       uuid,
  state            text        NOT NULL DEFAULT 'open',
  outcome          text,                           -- the written outcome given to the person
  answered_on      date,
  created_at       timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT access_request_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT access_request_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES nemura.organisation (organisation_id),
  CONSTRAINT access_request_requester_fkey FOREIGN KEY (organisation_id, requester_id)
    REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT access_request_handler_fkey FOREIGN KEY (organisation_id, handled_by)
    REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT access_request_state_known CHECK (state IN ('open', 'in_review', 'answered')),
  CONSTRAINT access_request_due CHECK (due_on = received_on + 30),
  CONSTRAINT access_request_notice_in_time CHECK (extended_notice_on IS NULL OR extended_notice_on <= due_on),
  -- an answer is in writing, on a day, by someone other than the person asking (nobody handles their own request)
  CONSTRAINT access_request_answered_whole CHECK ((state = 'answered') = (outcome IS NOT NULL AND answered_on IS NOT NULL AND handled_by IS NOT NULL)),
  CONSTRAINT access_request_outcome_length CHECK (length(btrim(coalesce(outcome, 'x'))) BETWEEN 1 AND 20000),
  CONSTRAINT access_request_not_own CHECK (handled_by IS NULL OR handled_by <> requester_id)
);
CREATE INDEX access_request_due_idx ON nemura.access_request (organisation_id, state, due_on);

-- One decision per category of record for the request: release it, release it with other people's data removed, or withhold it
-- under a recorded exception (for example opinions kept solely for an evaluative purpose: PDPC selected-topics guidelines,
-- [verify the current edition with the DPO]).
CREATE TABLE nemura.access_request_item (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  request_id      uuid        NOT NULL,
  category        text        NOT NULL,
  decision        text        NOT NULL,
  exception       text,
  decided_by      uuid        NOT NULL,
  decided_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT access_request_item_pkey PRIMARY KEY (organisation_id, request_id, category),
  CONSTRAINT access_request_item_request_fkey FOREIGN KEY (organisation_id, request_id)
    REFERENCES nemura.access_request (organisation_id, id),
  CONSTRAINT access_request_item_decider_fkey FOREIGN KEY (organisation_id, decided_by)
    REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT access_request_item_decision_known CHECK (decision IN ('release', 'redact', 'withhold')),
  CONSTRAINT access_request_item_exception_for_withhold CHECK ((decision = 'withhold') = (length(btrim(coalesce(exception, ''))) > 0)),
  CONSTRAINT access_request_item_category_format CHECK (category ~ '^[a-z][a-z0-9_-]*$' AND length(category) <= 60)
);

-- A request is answered once, and its decisions are not changed after the answer.
-- +goose StatementBegin
CREATE FUNCTION nemura.access_request_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR NEW.requester_id IS DISTINCT FROM OLD.requester_id OR NEW.received_on IS DISTINCT FROM OLD.received_on
     OR NEW.due_on IS DISTINCT FROM OLD.due_on OR OLD.state = 'answered' THEN
    RAISE EXCEPTION 'An access request keeps what was asked and, once answered, its answer' USING ERRCODE = 'NM090';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER access_request_guard BEFORE UPDATE OR DELETE ON nemura.access_request
  FOR EACH ROW EXECUTE FUNCTION nemura.access_request_guard();

-- +goose StatementBegin
CREATE FUNCTION nemura.access_request_item_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR EXISTS (SELECT 1 FROM nemura.access_request r
       WHERE r.organisation_id = coalesce(NEW.organisation_id, OLD.organisation_id) AND r.id = coalesce(NEW.request_id, OLD.request_id) AND r.state = 'answered') THEN
    RAISE EXCEPTION 'The decisions of an answered access request are kept' USING ERRCODE = 'NM090';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER access_request_item_guard BEFORE INSERT OR UPDATE OR DELETE ON nemura.access_request_item
  FOR EACH ROW EXECUTE FUNCTION nemura.access_request_item_guard();

-- ------------------------------------------------------------------ legal holds and deletion (NM-SEC-05)
CREATE TABLE nemura.legal_hold (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  record_type     text        NOT NULL,
  subject_id      uuid,                              -- one person's records of that type; null holds every record of the type
  reason          text        NOT NULL,
  placed_by       uuid        NOT NULL,
  placed_on       date        NOT NULL,
  released_on     date,
  CONSTRAINT legal_hold_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT legal_hold_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES nemura.organisation (organisation_id),
  CONSTRAINT legal_hold_placed_by_fkey FOREIGN KEY (organisation_id, placed_by)
    REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT legal_hold_record_type_format CHECK (record_type ~ '^[a-z][a-z0-9_-]*$' AND length(record_type) <= 60),
  CONSTRAINT legal_hold_reason_length CHECK (length(btrim(reason)) BETWEEN 1 AND 2000),
  CONSTRAINT legal_hold_dates CHECK (released_on IS NULL OR released_on >= placed_on)
);

-- A due deletion is a report for review (never silent): what is due, what a hold keeps, and a review date after which an
-- unreviewed report is overdue and shows ✖ in Checks; "reviewed" never means kept for ever.
CREATE TABLE nemura.deletion_report (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  record_type     text        NOT NULL,
  due_count       integer     NOT NULL,
  held_count      integer     NOT NULL,
  created_on      date        NOT NULL,
  review_by       date        NOT NULL,              -- 30 days after by default
  state           text        NOT NULL DEFAULT 'pending',
  reviewed_by     uuid,
  reviewed_on     date,
  CONSTRAINT deletion_report_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT deletion_report_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES nemura.organisation (organisation_id),
  CONSTRAINT deletion_report_reviewer_fkey FOREIGN KEY (organisation_id, reviewed_by)
    REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT deletion_report_state_known CHECK (state IN ('pending', 'reviewed', 'deleted')),
  CONSTRAINT deletion_report_counts CHECK (due_count >= 0 AND held_count >= 0),
  CONSTRAINT deletion_report_review_after CHECK (review_by >= created_on),
  CONSTRAINT deletion_report_reviewed_whole CHECK ((state = 'pending') = (reviewed_by IS NULL AND reviewed_on IS NULL))
);

-- Each deletion leaves proof of what went and when: the record's type and id and a fingerprint of its content, never the content.
CREATE TABLE nemura.deletion_proof (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  report_id       uuid        NOT NULL,
  record_type     text        NOT NULL,
  record_id       uuid        NOT NULL,
  fingerprint     bytea       NOT NULL,
  deleted_on      date        NOT NULL,
  CONSTRAINT deletion_proof_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT deletion_proof_report_fkey FOREIGN KEY (organisation_id, report_id)
    REFERENCES nemura.deletion_report (organisation_id, id),
  CONSTRAINT deletion_proof_fingerprint_sha256 CHECK (length(fingerprint) = 32)
);

-- ------------------------------------------------------------------ transfers outside Singapore (NM-SEC-03, CMP-PD-06)
CREATE TABLE nemura.transfer_assessment (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  flow            text        NOT NULL,              -- the flow of the data inventory, such as email or ai
  country         text        NOT NULL,
  assessed_on     date        NOT NULL,
  valid_until     date        NOT NULL,
  assessed_by     uuid        NOT NULL,
  CONSTRAINT transfer_assessment_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT transfer_assessment_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES nemura.organisation (organisation_id),
  CONSTRAINT transfer_assessment_by_fkey FOREIGN KEY (organisation_id, assessed_by)
    REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT transfer_assessment_country CHECK (country ~ '^[A-Z]{2}$' AND country <> 'SG'),
  CONSTRAINT transfer_assessment_dates CHECK (valid_until > assessed_on),
  CONSTRAINT transfer_assessment_flow_format CHECK (flow ~ '^[a-z][a-z0-9_-]*$' AND length(flow) <= 60)
);

-- ------------------------------------------------------------------ audit, row-level security, grants
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.access_request
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('access_request', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.legal_hold
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('legal_hold', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.deletion_report
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('deletion_report', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON nemura.transfer_assessment
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('transfer_assessment', 'id');

ALTER TABLE nemura.access_request ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.access_request FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.access_request
  USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.access_request_item ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.access_request_item FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.access_request_item
  USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.legal_hold ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.legal_hold FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.legal_hold
  USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.deletion_report ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.deletion_report FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.deletion_report
  USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.deletion_proof ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.deletion_proof FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.deletion_proof
  USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.transfer_assessment ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.transfer_assessment FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.transfer_assessment
  USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());

-- The trail of proof is append-only: nothing in it is changed or deleted.
-- +goose StatementBegin
CREATE FUNCTION nemura.append_only() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION '% is append-only (% refused)', TG_TABLE_NAME, TG_OP USING ERRCODE = 'NM091';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER deletion_proof_append_only BEFORE UPDATE OR DELETE ON nemura.deletion_proof
  FOR EACH ROW EXECUTE FUNCTION nemura.append_only();
CREATE TRIGGER transfer_assessment_append_only BEFORE UPDATE OR DELETE ON nemura.transfer_assessment
  FOR EACH ROW EXECUTE FUNCTION nemura.append_only();

GRANT SELECT ON nemura.access_request, nemura.access_request_item, nemura.legal_hold, nemura.deletion_report, nemura.deletion_proof,
  nemura.transfer_assessment TO nemura_api, nemura_worker, nemura_readonly;
GRANT INSERT (id, requester_id, received_on, due_on), UPDATE (handled_by, state, outcome, answered_on, extended_notice_on)
  ON nemura.access_request TO nemura_api;
GRANT INSERT (request_id, category, decision, exception, decided_by), UPDATE (decision, exception, decided_by)
  ON nemura.access_request_item TO nemura_api;
GRANT INSERT (id, record_type, subject_id, reason, placed_by, placed_on), UPDATE (released_on) ON nemura.legal_hold TO nemura_api;
GRANT INSERT (id, record_type, due_count, held_count, created_on, review_by), UPDATE (state, reviewed_by, reviewed_on)
  ON nemura.deletion_report TO nemura_api, nemura_worker;
GRANT INSERT (id, report_id, record_type, record_id, fingerprint, deleted_on) ON nemura.deletion_proof TO nemura_worker;
GRANT INSERT (id, flow, country, assessed_on, valid_until, assessed_by) ON nemura.transfer_assessment TO nemura_api;
