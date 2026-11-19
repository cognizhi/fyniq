-- Final pay and the tax-clearance hold (PRD-002 NM-PAY-06, NM-PAY-08, NM-INT-08; PRD-001 FR-PAY-10; p1-nemura-payroll slice 5b).
--
-- A one-off item can now be leave pay on leaving, pay in lieu of notice or a retrenchment benefit. A tax-clearance hold (IRAS Form IR21: the employer withholds all monies due to an employee who is
-- ceasing work in Singapore until tax clearance is given) is a series of versions of one hold, written once and never changed: a hold is put on, and released only with a document of the person as evidence.
-- While the latest version is "held" every dollar due to the person in a pay run is held and none is disbursed. The same versions are what Fyniq is told (`people.tax_hold.changed`: hold_id, version,
-- status, effective_from, evidence). No audit event holds an amount.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE nemura.pay_one_off DROP CONSTRAINT pay_one_off_nature_known;
ALTER TABLE nemura.pay_one_off ADD CONSTRAINT pay_one_off_nature_known CHECK (nature IN ('bonus', 'commission', 'allowance', 'other', 'leave_encashment', 'notice_pay', 'retrenchment_benefit')) NOT VALID;
-- (a table of a few hundred rows at most in a company, and the new check covers the old ones)
-- squawk-ignore constraint-missing-not-valid
ALTER TABLE nemura.pay_one_off VALIDATE CONSTRAINT pay_one_off_nature_known;
-- A retrenchment benefit is compensation for the loss of the job and not CPF wages (CPF Board): the database holds it too.
ALTER TABLE nemura.pay_one_off ADD CONSTRAINT pay_one_off_retrenchment_not_wages CHECK (nature <> 'retrenchment_benefit' OR NOT cpf_wages) NOT VALID;
-- squawk-ignore constraint-missing-not-valid
ALTER TABLE nemura.pay_one_off VALIDATE CONSTRAINT pay_one_off_retrenchment_not_wages;

CREATE TABLE nemura.tax_hold (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  hold_id         uuid        NOT NULL,   -- the hold these versions belong to: the id of its first version
  person_id       uuid        NOT NULL,
  version         integer     NOT NULL,
  status          text        NOT NULL,
  effective_from  date        NOT NULL,
  evidence_id     uuid,                   -- a document of the person: required to release
  reason          text,
  recorded_by     uuid        NOT NULL,
  recorded_at     timestamptz NOT NULL,
  CONSTRAINT tax_hold_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT tax_hold_person_fkey FOREIGN KEY (organisation_id, person_id) REFERENCES nemura.person (organisation_id, id),
  CONSTRAINT tax_hold_evidence_fkey FOREIGN KEY (organisation_id, evidence_id) REFERENCES nemura.person_document (organisation_id, id),
  CONSTRAINT tax_hold_by_fkey FOREIGN KEY (organisation_id, recorded_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT tax_hold_version_once UNIQUE (organisation_id, hold_id, version),
  CONSTRAINT tax_hold_status CHECK (status IN ('held', 'released')),
  CONSTRAINT tax_hold_version_positive CHECK (version >= 1),
  CONSTRAINT tax_hold_release_has_evidence CHECK (status <> 'released' OR evidence_id IS NOT NULL),
  CONSTRAINT tax_hold_reason_length CHECK (reason IS NULL OR length(reason) <= 300)
);
CREATE INDEX tax_hold_person_idx ON nemura.tax_hold (organisation_id, person_id, version);

-- +goose StatementBegin
CREATE FUNCTION nemura.tax_hold_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
DECLARE
  prev_status text;
  prev_version integer;
BEGIN
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'A tax-clearance hold is history: a change is a new version' USING ERRCODE = 'NM127';
  END IF;
  PERFORM pg_advisory_xact_lock(hashtextextended('tax-hold:' || NEW.organisation_id::text || NEW.person_id::text, 0));
  SELECT status, version INTO prev_status, prev_version FROM nemura.tax_hold
   WHERE organisation_id = NEW.organisation_id AND person_id = NEW.person_id ORDER BY recorded_at DESC, version DESC, id DESC LIMIT 1;
  IF prev_status IS NULL OR prev_status = 'released' THEN
    -- a new hold: its first version, put on
    IF NEW.status <> 'held' OR NEW.version <> 1 OR NEW.hold_id <> NEW.id THEN
      RAISE EXCEPTION 'A new hold starts as held, at version 1, under its own id' USING ERRCODE = 'NM177';
    END IF;
  ELSE
    -- the open hold: the next version releases it
    IF NEW.status <> 'released' OR NEW.version <> prev_version + 1 OR NOT EXISTS (SELECT 1 FROM nemura.tax_hold h WHERE h.organisation_id = NEW.organisation_id AND h.person_id = NEW.person_id AND h.hold_id = NEW.hold_id AND h.version = prev_version) THEN
      RAISE EXCEPTION 'An open hold is released by its next version, with its own hold id' USING ERRCODE = 'NM177';
    END IF;
  END IF;
  IF NEW.evidence_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM nemura.person_document WHERE organisation_id = NEW.organisation_id AND id = NEW.evidence_id AND person_id = NEW.person_id) THEN
    RAISE EXCEPTION 'The evidence of a hold is a document of the person' USING ERRCODE = 'NM176';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER tax_hold_guard BEFORE INSERT OR UPDATE OR DELETE ON nemura.tax_hold FOR EACH ROW EXECUTE FUNCTION nemura.tax_hold_guard();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON nemura.tax_hold DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('tax_hold', 'id');

ALTER TABLE nemura.tax_hold ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.tax_hold FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.tax_hold USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());

GRANT SELECT ON nemura.tax_hold TO nemura_api, nemura_worker, nemura_readonly;
GRANT INSERT (id, hold_id, person_id, version, status, effective_from, evidence_id, reason, recorded_by, recorded_at) ON nemura.tax_hold TO nemura_api;
