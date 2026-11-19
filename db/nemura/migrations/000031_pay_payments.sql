-- Payment states (PRD-002 NM-PAY-10; p1-nemura-payroll slice 5a).
--
-- A person's payment in a pay run moves through states: scheduled, released, sent, accepted, paid, rejected, returned or unknown. Each change is an event that is written once, never changed or
-- deleted, and the database only lets the legal moves through: "paid" is never the first word of an unknown outcome, and an unknown outcome blocks a new payment (a new "scheduled") until it is
-- resolved as paid, rejected or returned. The events are keyed on the run and the person, not on a version, so that block survives a corrected version. A state comes from Fyniq's status events
-- (source `fyniq`, with the instruction ID) or from a person recording the outcome with evidence (source `manual`, with a document of the payee as evidence): an exported file or a posted
-- journal is never "paid". The amount is restricted pay data; no audit event holds it.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE nemura.pay_payment_event (
  organisation_id uuid          NOT NULL DEFAULT nemura.current_org(),
  id              uuid          NOT NULL DEFAULT uuidv7(),
  run_id          uuid          NOT NULL,
  person_id       uuid          NOT NULL,
  seq             integer       NOT NULL,
  state           text          NOT NULL,
  instruction_id  text,
  amount          numeric(20,4) NOT NULL,
  paid_on         date          NOT NULL,
  source          text          NOT NULL,
  evidence_id     uuid,
  note            text,
  recorded_by     uuid,
  recorded_at     timestamptz   NOT NULL,
  CONSTRAINT pay_payment_event_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT pay_payment_event_run_fkey FOREIGN KEY (organisation_id, run_id) REFERENCES nemura.pay_run (organisation_id, id),
  CONSTRAINT pay_payment_event_person_fkey FOREIGN KEY (organisation_id, person_id) REFERENCES nemura.person (organisation_id, id),
  CONSTRAINT pay_payment_event_evidence_fkey FOREIGN KEY (organisation_id, evidence_id) REFERENCES nemura.person_document (organisation_id, id),
  CONSTRAINT pay_payment_event_by_fkey FOREIGN KEY (organisation_id, recorded_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT pay_payment_event_seq UNIQUE (organisation_id, run_id, person_id, seq),
  CONSTRAINT pay_payment_event_state CHECK (state IN ('scheduled', 'released', 'sent', 'accepted', 'paid', 'rejected', 'returned', 'unknown')),
  CONSTRAINT pay_payment_event_amount CHECK (amount >= 0),
  CONSTRAINT pay_payment_event_seq_positive CHECK (seq >= 1),
  CONSTRAINT pay_payment_event_note CHECK (note IS NULL OR length(note) <= 500),
  -- A state is from the bank's side through Fyniq (with the instruction) or a person's with evidence; never a bare word.
  CONSTRAINT pay_payment_event_source CHECK ((source = 'fyniq' AND recorded_by IS NULL AND instruction_id IS NOT NULL AND length(btrim(instruction_id)) > 0)
                                          OR (source = 'manual' AND recorded_by IS NOT NULL AND evidence_id IS NOT NULL))
);
CREATE INDEX pay_payment_event_person_idx ON nemura.pay_payment_event (organisation_id, person_id, run_id, seq);

-- +goose StatementBegin
CREATE FUNCTION nemura.pay_payment_event_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
DECLARE
  prev text;
  last_seq integer;
  ok boolean;
BEGIN
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'A payment state is evidence: a change is a new event' USING ERRCODE = 'NM127';
  END IF;
  PERFORM pg_advisory_xact_lock(hashtextextended('pay-payment:' || NEW.organisation_id::text || NEW.run_id::text || NEW.person_id::text, 0));
  SELECT state, seq INTO prev, last_seq FROM nemura.pay_payment_event
   WHERE organisation_id = NEW.organisation_id AND run_id = NEW.run_id AND person_id = NEW.person_id ORDER BY seq DESC LIMIT 1;
  IF NEW.seq IS DISTINCT FROM COALESCE(last_seq, 0) + 1 THEN
    RAISE EXCEPTION 'A payment state follows the last one: expected event number %', COALESCE(last_seq, 0) + 1 USING ERRCODE = 'NM175';
  END IF;
  IF prev IS NOT NULL THEN
    ok := CASE prev
      WHEN 'scheduled' THEN NEW.state IN ('released', 'sent', 'accepted', 'paid', 'rejected', 'unknown')
      WHEN 'released'  THEN NEW.state IN ('sent', 'accepted', 'paid', 'rejected', 'unknown')
      WHEN 'sent'      THEN NEW.state IN ('accepted', 'paid', 'rejected', 'returned', 'unknown')
      WHEN 'accepted'  THEN NEW.state IN ('paid', 'rejected', 'returned', 'unknown')
      WHEN 'paid'      THEN NEW.state IN ('returned')
      WHEN 'rejected'  THEN NEW.state IN ('scheduled')
      WHEN 'returned'  THEN NEW.state IN ('scheduled')
      WHEN 'unknown'   THEN NEW.state IN ('paid', 'rejected', 'returned')
      ELSE false END;
    IF NOT ok THEN
      RAISE EXCEPTION 'A payment cannot go from % to %: an unknown outcome is resolved as paid, rejected or returned before a new payment', prev, NEW.state USING ERRCODE = 'NM175';
    END IF;
  END IF;
  IF NEW.evidence_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM nemura.person_document WHERE organisation_id = NEW.organisation_id AND id = NEW.evidence_id AND person_id = NEW.person_id) THEN
    RAISE EXCEPTION 'The evidence of a payment is a document of the person paid' USING ERRCODE = 'NM176';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER pay_payment_event_guard BEFORE INSERT OR UPDATE OR DELETE ON nemura.pay_payment_event FOR EACH ROW EXECUTE FUNCTION nemura.pay_payment_event_guard();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON nemura.pay_payment_event DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('pay_payment_event', 'id');

ALTER TABLE nemura.pay_payment_event ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.pay_payment_event FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.pay_payment_event USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());

GRANT SELECT ON nemura.pay_payment_event TO nemura_api, nemura_worker, nemura_readonly;
GRANT INSERT (id, run_id, person_id, seq, state, instruction_id, amount, paid_on, source, evidence_id, note, recorded_by, recorded_at) ON nemura.pay_payment_event TO nemura_api;
