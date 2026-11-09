-- Consent where a purpose needs it (PRD-001 CMP-PD-03, rev 0.244; p1-pdpa-retention slice 3, design D2). The DPO defines
-- the organisation's purposes, each saying whether it needs consent; none is seeded, since Fyniq uses members' and
-- contacts' data for the work and the business relationship and for its legal obligations [counsel to confirm]. Each
-- consent given or withdrawn, for a member or a contact, is a row of its own. Both are only ever added (FY311), each
-- audited in the same transaction (AUD-01), under the "Data protection (PDPA)" row of Appendix D.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.consent_purpose (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  name            text        NOT NULL,
  description     text        NOT NULL,
  audience        text        NOT NULL,  -- members or contacts
  needs_consent   boolean     NOT NULL,
  created_by      text        NOT NULL,
  created_at      timestamptz NOT NULL,
  CONSTRAINT consent_purpose_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT consent_purpose_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT consent_purpose_name_once UNIQUE (organisation_id, name),
  CONSTRAINT consent_purpose_name_length CHECK (length(btrim(name)) BETWEEN 1 AND 200),
  CONSTRAINT consent_purpose_description_length CHECK (length(btrim(description)) BETWEEN 1 AND 2000),
  CONSTRAINT consent_purpose_audience_known CHECK (audience IN ('members', 'contacts')),
  CONSTRAINT consent_purpose_created_by_format CHECK (created_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);

CREATE TABLE fyniq.consent_record (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  purpose_id      uuid        NOT NULL,
  person          text        NOT NULL,  -- member:<user id> or contact:<contact id>
  event           text        NOT NULL,  -- given or withdrawn
  at              timestamptz NOT NULL,
  recorded_by     text        NOT NULL,
  note            text,
  CONSTRAINT consent_record_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT consent_record_purpose_fkey FOREIGN KEY (organisation_id, purpose_id) REFERENCES fyniq.consent_purpose (organisation_id, id),
  CONSTRAINT consent_record_person_format CHECK (person ~ '^(member|contact):[0-9a-f-]{36}$'),
  CONSTRAINT consent_record_event_known CHECK (event IN ('given', 'withdrawn')),
  CONSTRAINT consent_record_note_length CHECK (length(note) BETWEEN 1 AND 2000),
  CONSTRAINT consent_record_by_format CHECK (recorded_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);
CREATE INDEX consent_record_person ON fyniq.consent_record (organisation_id, purpose_id, person, at);

-- A purpose is only ever added and never changes; a consent is given only when none stands for that person and purpose,
-- and withdrawn only when one does. Nothing is changed or removed.
-- +goose StatementBegin
CREATE FUNCTION fyniq.consent_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  last text;
  aud  text;
BEGIN
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'Consent purposes and records never change or go' USING ERRCODE = 'FY311',
      HINT = 'Record a withdrawal, or add a new purpose (CMP-PD-03).';
  END IF;
  IF TG_TABLE_NAME = 'consent_record' THEN
    SELECT p.audience INTO aud FROM fyniq.consent_purpose p WHERE p.organisation_id = NEW.organisation_id AND p.id = NEW.purpose_id;
    IF (aud = 'members') <> (NEW.person LIKE 'member:%') THEN
      RAISE EXCEPTION 'The purpose is for %, and % is not one', aud, NEW.person USING ERRCODE = 'FY311';
    END IF;
    SELECT r.event INTO last FROM fyniq.consent_record r
      WHERE r.organisation_id = NEW.organisation_id AND r.purpose_id = NEW.purpose_id AND r.person = NEW.person
      ORDER BY r.at DESC, r.id DESC LIMIT 1;
    IF NEW.event = coalesce(last, 'withdrawn') THEN
      RAISE EXCEPTION 'A consent is % only after it is %', NEW.event,
        CASE NEW.event WHEN 'given' THEN 'withdrawn, or before any' ELSE 'given' END USING ERRCODE = 'FY311';
    END IF;
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER consent_purpose_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.consent_purpose
  FOR EACH ROW EXECUTE FUNCTION fyniq.consent_guard();
CREATE TRIGGER consent_record_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.consent_record
  FOR EACH ROW EXECUTE FUNCTION fyniq.consent_guard();
CREATE TRIGGER consent_purpose_no_truncate BEFORE TRUNCATE ON fyniq.consent_purpose
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE TRIGGER consent_record_no_truncate BEFORE TRUNCATE ON fyniq.consent_record
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.consent_purpose
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('consent_purpose', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.consent_record
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('consent_record', 'id');

ALTER TABLE fyniq.consent_purpose ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.consent_purpose FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.consent_purpose
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.consent_record ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.consent_record FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.consent_record
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

GRANT INSERT (name, description, audience, needs_consent, created_by, created_at) ON fyniq.consent_purpose TO fyniq_api;
GRANT INSERT (purpose_id, person, event, at, recorded_by, note) ON fyniq.consent_record TO fyniq_api;
GRANT SELECT ON fyniq.consent_purpose, fyniq.consent_record TO fyniq_api, fyniq_worker, fyniq_readonly;
