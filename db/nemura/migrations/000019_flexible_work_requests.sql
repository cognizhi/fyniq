-- Flexible work arrangement requests (PRD-002 NM-LV-10; p1-nemura-leave-time slice 3).
--
-- Under the Tripartite Guidelines on Flexible Work Arrangement Requests the employer answers a formal request in writing within the months the country pack gives (two),
-- with a reason for a refusal. A request keeps its formal receipt date, which a clarification or a change of type never restarts: so the receipt date and the reply
-- deadline are written once. The written reply is written once too, by someone other than the person, and is the record the guidelines ask the employer to keep.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE nemura.flex_request (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  person_id       uuid        NOT NULL,
  arrangement     text        NOT NULL,
  start_date      date        NOT NULL,
  end_date        date,
  reason          text        NOT NULL,
  received_on     date        NOT NULL,   -- the formal receipt date: the clock of the reply starts here and never restarts
  reply_due       date        NOT NULL,   -- received_on plus the pack's months, worked out when it is received
  pack_version    text        NOT NULL,
  state           text        NOT NULL DEFAULT 'received',
  requested_by    uuid        NOT NULL,
  requested_at    timestamptz NOT NULL,
  outcome         text,                   -- accepted, rejected or alternative: how the employer answered
  written_reply   text,
  replied_by      uuid,
  replied_at      timestamptz,
  withdrawn_at    timestamptz,
  CONSTRAINT flex_request_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT flex_request_person_fkey FOREIGN KEY (organisation_id, person_id) REFERENCES nemura.person (organisation_id, id),
  CONSTRAINT flex_request_by_fkey FOREIGN KEY (organisation_id, requested_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT flex_request_replied_by_fkey FOREIGN KEY (organisation_id, replied_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT flex_request_arrangement_known CHECK (arrangement IN ('flexi_time', 'flexi_place', 'flexi_load')),
  CONSTRAINT flex_request_dates CHECK (end_date IS NULL OR end_date >= start_date),
  CONSTRAINT flex_request_reason_length CHECK (length(btrim(reason)) BETWEEN 1 AND 2000),
  CONSTRAINT flex_request_due CHECK (reply_due > received_on),
  CONSTRAINT flex_request_state_known CHECK (state IN ('received', 'replied', 'withdrawn')),
  CONSTRAINT flex_request_outcome_known CHECK (outcome IS NULL OR outcome IN ('accepted', 'rejected', 'alternative')),
  CONSTRAINT flex_request_reply_whole CHECK ((state = 'replied') = (outcome IS NOT NULL) AND (outcome IS NULL) = (written_reply IS NULL) AND (outcome IS NULL) = (replied_by IS NULL) AND (outcome IS NULL) = (replied_at IS NULL)),
  CONSTRAINT flex_request_reply_length CHECK (written_reply IS NULL OR length(btrim(written_reply)) BETWEEN 1 AND 4000),
  CONSTRAINT flex_request_withdrawn_whole CHECK ((state = 'withdrawn') = (withdrawn_at IS NOT NULL))
);
CREATE INDEX flex_request_person_idx ON nemura.flex_request (organisation_id, person_id, requested_at);
CREATE INDEX flex_request_state_idx ON nemura.flex_request (organisation_id, state, reply_due);

-- A request's own words, receipt date and deadline never change; it is answered once, in writing, by someone other than the person, and may be withdrawn by the
-- person only while it waits.
-- +goose StatementBegin
CREATE FUNCTION nemura.flex_request_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A flexible work request is evidence: it is withdrawn, never deleted' USING ERRCODE = 'NM151';
  END IF;
  IF TG_OP = 'INSERT' THEN
    IF NEW.state <> 'received' THEN
      RAISE EXCEPTION 'A flexible work request starts received' USING ERRCODE = 'NM151';
    END IF;
    RETURN NEW;
  END IF;
  IF NEW.person_id IS DISTINCT FROM OLD.person_id OR NEW.arrangement IS DISTINCT FROM OLD.arrangement OR NEW.start_date IS DISTINCT FROM OLD.start_date
     OR NEW.end_date IS DISTINCT FROM OLD.end_date OR NEW.reason IS DISTINCT FROM OLD.reason OR NEW.received_on IS DISTINCT FROM OLD.received_on
     OR NEW.reply_due IS DISTINCT FROM OLD.reply_due OR NEW.pack_version IS DISTINCT FROM OLD.pack_version OR NEW.requested_by IS DISTINCT FROM OLD.requested_by
     OR NEW.requested_at IS DISTINCT FROM OLD.requested_at THEN
    RAISE EXCEPTION 'A flexible work request keeps its words, its receipt date and its deadline' USING ERRCODE = 'NM151';
  END IF;
  IF OLD.state <> 'received' THEN
    RAISE EXCEPTION 'A % flexible work request is closed', OLD.state USING ERRCODE = 'NM151';
  END IF;
  IF NEW.state = 'replied' AND EXISTS (SELECT 1 FROM nemura.person p WHERE p.organisation_id = NEW.organisation_id AND p.id = NEW.person_id AND p.member_id = NEW.replied_by) THEN
    RAISE EXCEPTION 'Nobody answers their own flexible work request' USING ERRCODE = 'NM152';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER flex_request_guard BEFORE INSERT OR UPDATE OR DELETE ON nemura.flex_request FOR EACH ROW EXECUTE FUNCTION nemura.flex_request_guard();

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.flex_request DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('flex_request', 'id');

ALTER TABLE nemura.flex_request ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.flex_request FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.flex_request USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());

GRANT SELECT ON nemura.flex_request TO nemura_api, nemura_worker, nemura_readonly;
GRANT INSERT (id, person_id, arrangement, start_date, end_date, reason, received_on, reply_due, pack_version, requested_by, requested_at) ON nemura.flex_request TO nemura_api;
GRANT UPDATE (state, outcome, written_reply, replied_by, replied_at, withdrawn_at) ON nemura.flex_request TO nemura_api;
