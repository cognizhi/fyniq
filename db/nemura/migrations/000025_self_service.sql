-- Self-service: a person's own details, details changes that need HR, and questions to HR (PRD-002 NM-ESS-03, NM-ESS-04; p1-nemura-self-service-letters slice 3).
--
-- Contact details, the address and emergency contacts are the person's own to change at once (audited). A change of legal name, residency or NRIC or FIN waits for
-- evidence and another person's approval; the request holds no NRIC or FIN, only what changed in words and the evidence, and HR records the number through the sealed
-- flow. A question to HR is answered once, by someone who is not the asker.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE nemura.person_contact (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  person_id       uuid        NOT NULL,
  personal_email  text,
  address         text,
  postal_code     text,
  updated_at      timestamptz NOT NULL,
  CONSTRAINT person_contact_pkey PRIMARY KEY (organisation_id, person_id),
  CONSTRAINT person_contact_person_fkey FOREIGN KEY (organisation_id, person_id) REFERENCES nemura.person (organisation_id, id),
  CONSTRAINT person_contact_lengths CHECK (coalesce(length(personal_email), 0) <= 200 AND coalesce(length(address), 0) <= 300 AND coalesce(length(postal_code), 0) <= 12),
  CONSTRAINT person_contact_email_shape CHECK (personal_email IS NULL OR personal_email ~ '^[^@\s]+@[^@\s]+\.[^@\s]+$')
);

CREATE TABLE nemura.emergency_contact (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  person_id       uuid        NOT NULL,
  position        smallint    NOT NULL,
  contact_name    text        NOT NULL,
  relationship    text        NOT NULL,
  contact_phone   text        NOT NULL,
  updated_at      timestamptz NOT NULL,
  CONSTRAINT emergency_contact_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT emergency_contact_person_fkey FOREIGN KEY (organisation_id, person_id) REFERENCES nemura.person (organisation_id, id),
  CONSTRAINT emergency_contact_position UNIQUE (organisation_id, person_id, position),
  CONSTRAINT emergency_contact_position_range CHECK (position BETWEEN 1 AND 3),
  CONSTRAINT emergency_contact_lengths CHECK (length(btrim(contact_name)) BETWEEN 1 AND 120 AND length(btrim(relationship)) BETWEEN 1 AND 60 AND length(btrim(contact_phone)) BETWEEN 3 AND 30)
);

CREATE TABLE nemura.details_change (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  person_id       uuid        NOT NULL,
  kind            text        NOT NULL,
  new_legal_name  text,
  new_residency   text,
  new_pr_start    date,
  note            text        NOT NULL,
  evidence_id     uuid        NOT NULL,
  state           text        NOT NULL DEFAULT 'requested',
  requested_by    uuid        NOT NULL,
  requested_at    timestamptz NOT NULL,
  due_on          date        NOT NULL,
  decided_by      uuid,
  decided_at      timestamptz,
  decision_reason text,
  CONSTRAINT details_change_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT details_change_person_fkey FOREIGN KEY (organisation_id, person_id) REFERENCES nemura.person (organisation_id, id),
  CONSTRAINT details_change_evidence_fkey FOREIGN KEY (organisation_id, evidence_id) REFERENCES nemura.person_document (organisation_id, id),
  CONSTRAINT details_change_requested_by_fkey FOREIGN KEY (organisation_id, requested_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT details_change_decided_by_fkey FOREIGN KEY (organisation_id, decided_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT details_change_kind_known CHECK (kind IN ('legal_name', 'residency', 'nric_fin')),
  CONSTRAINT details_change_state_known CHECK (state IN ('requested', 'approved', 'refused', 'withdrawn')),
  CONSTRAINT details_change_kind_whole CHECK (
    (kind = 'legal_name' AND new_legal_name IS NOT NULL AND length(btrim(new_legal_name)) BETWEEN 1 AND 200 AND new_residency IS NULL AND new_pr_start IS NULL)
    OR (kind = 'residency' AND new_legal_name IS NULL AND new_residency IN ('citizen', 'permanent_resident', 'foreigner') AND ((new_residency = 'permanent_resident') = (new_pr_start IS NOT NULL)))
    OR (kind = 'nric_fin' AND new_legal_name IS NULL AND new_residency IS NULL AND new_pr_start IS NULL)),
  CONSTRAINT details_change_note_length CHECK (length(btrim(note)) BETWEEN 1 AND 500),
  CONSTRAINT details_change_decided_whole CHECK ((state = 'requested') = (decided_at IS NULL)
    AND ((state IN ('approved', 'refused')) = (decided_by IS NOT NULL))
    AND (state <> 'refused' OR length(btrim(coalesce(decision_reason, ''))) > 0)
    AND coalesce(length(decision_reason), 0) <= 500)
);
CREATE INDEX details_change_person_idx ON nemura.details_change (organisation_id, person_id, requested_at);
CREATE INDEX details_change_open_idx ON nemura.details_change (organisation_id, due_on) WHERE state = 'requested';

CREATE TABLE nemura.hr_question (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  person_id       uuid        NOT NULL,
  question        text        NOT NULL,
  state           text        NOT NULL DEFAULT 'open',
  asked_at        timestamptz NOT NULL,
  due_on          date        NOT NULL,
  reply           text,
  replied_by      uuid,
  replied_at      timestamptz,
  CONSTRAINT hr_question_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT hr_question_person_fkey FOREIGN KEY (organisation_id, person_id) REFERENCES nemura.person (organisation_id, id),
  CONSTRAINT hr_question_replied_by_fkey FOREIGN KEY (organisation_id, replied_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT hr_question_state_known CHECK (state IN ('open', 'answered', 'withdrawn')),
  CONSTRAINT hr_question_length CHECK (length(btrim(question)) BETWEEN 1 AND 1000 AND coalesce(length(reply), 0) <= 2000),
  CONSTRAINT hr_question_answered_whole CHECK ((state = 'answered') = (replied_at IS NOT NULL AND replied_by IS NOT NULL AND length(btrim(coalesce(reply, ''))) > 0))
);
CREATE INDEX hr_question_person_idx ON nemura.hr_question (organisation_id, person_id, asked_at);
CREATE INDEX hr_question_open_idx ON nemura.hr_question (organisation_id, due_on) WHERE state = 'open';

-- A request moves forward once, from requested to approved, refused or withdrawn; what it asked never changes; nobody decides their own.
-- +goose StatementBegin
CREATE FUNCTION nemura.details_change_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR NEW.person_id IS DISTINCT FROM OLD.person_id OR NEW.kind IS DISTINCT FROM OLD.kind OR NEW.new_legal_name IS DISTINCT FROM OLD.new_legal_name
     OR NEW.new_residency IS DISTINCT FROM OLD.new_residency OR NEW.new_pr_start IS DISTINCT FROM OLD.new_pr_start OR NEW.note IS DISTINCT FROM OLD.note
     OR NEW.evidence_id IS DISTINCT FROM OLD.evidence_id OR NEW.requested_by IS DISTINCT FROM OLD.requested_by OR NEW.requested_at IS DISTINCT FROM OLD.requested_at
     OR NEW.due_on IS DISTINCT FROM OLD.due_on OR OLD.state <> 'requested' OR NEW.state = 'requested' THEN
    RAISE EXCEPTION 'A request to change details keeps its words and its evidence, and is decided once' USING ERRCODE = 'NM165';
  END IF;
  IF NEW.state IN ('approved', 'refused') AND EXISTS (SELECT 1 FROM nemura.person p WHERE p.organisation_id = NEW.organisation_id AND p.id = NEW.person_id AND p.member_id = NEW.decided_by) THEN
    RAISE EXCEPTION 'Nobody decides a change to their own details: another member of the people team does' USING ERRCODE = 'NM166';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER details_change_guard BEFORE UPDATE OR DELETE ON nemura.details_change FOR EACH ROW EXECUTE FUNCTION nemura.details_change_guard();

-- +goose StatementBegin
CREATE FUNCTION nemura.hr_question_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR NEW.person_id IS DISTINCT FROM OLD.person_id OR NEW.question IS DISTINCT FROM OLD.question OR NEW.asked_at IS DISTINCT FROM OLD.asked_at
     OR NEW.due_on IS DISTINCT FROM OLD.due_on OR OLD.state <> 'open' OR NEW.state = 'open' THEN
    RAISE EXCEPTION 'A question keeps its words and is answered or withdrawn once' USING ERRCODE = 'NM167';
  END IF;
  IF NEW.state = 'answered' AND EXISTS (SELECT 1 FROM nemura.person p WHERE p.organisation_id = NEW.organisation_id AND p.id = NEW.person_id AND p.member_id = NEW.replied_by) THEN
    RAISE EXCEPTION 'Nobody answers their own question: another member of the people team does' USING ERRCODE = 'NM168';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER hr_question_guard BEFORE UPDATE OR DELETE ON nemura.hr_question FOR EACH ROW EXECUTE FUNCTION nemura.hr_question_guard();

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.person_contact DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('person_contact', 'person_id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.emergency_contact DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('emergency_contact', 'person_id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.details_change DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('details_change', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.hr_question DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('hr_question', 'id');

ALTER TABLE nemura.person_contact ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.person_contact FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.person_contact USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.emergency_contact ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.emergency_contact FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.emergency_contact USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.details_change ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.details_change FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.details_change USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.hr_question ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.hr_question FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.hr_question USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());

GRANT SELECT ON nemura.person_contact, nemura.emergency_contact, nemura.details_change, nemura.hr_question TO nemura_api, nemura_worker, nemura_readonly;
GRANT INSERT (person_id, personal_email, address, postal_code, updated_at), UPDATE (personal_email, address, postal_code, updated_at) ON nemura.person_contact TO nemura_api;
GRANT INSERT (id, person_id, position, contact_name, relationship, contact_phone, updated_at), UPDATE (contact_name, relationship, contact_phone, updated_at), DELETE ON nemura.emergency_contact TO nemura_api;
GRANT INSERT (id, person_id, kind, new_legal_name, new_residency, new_pr_start, note, evidence_id, requested_by, requested_at, due_on),
      UPDATE (state, decided_by, decided_at, decision_reason) ON nemura.details_change TO nemura_api;
GRANT INSERT (id, person_id, question, asked_at, due_on), UPDATE (state, reply, replied_by, replied_at) ON nemura.hr_question TO nemura_api;
