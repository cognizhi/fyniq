-- The breach register and its PDPA timers, and the tabletop exercises (PRD-001 CMP-PD-05, CTL-DP-06, rev 0.244;
-- p1-pdpa-retention slice 7, design D6). A breach is assessed within 30 days of the company becoming aware of it; once
-- it is decided notifiable, the PDPC is notified within 3 calendar days. A breach is notifiable when it is likely to
-- cause significant harm or affects 500 or more people; one whose data includes a person's name or identification number
-- with their pay, card or bank account numbers, or an account's identifier with its password, is deemed to (the
-- Notification of Data Breaches Regulations 2021, reg 3 and its Schedule), so the database refuses to call it not
-- notifiable. Breaches, their steps and the exercises are only ever added (FY315), each audited (AUD-01), under the
-- "Data protection (PDPA)" row of Appendix D.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.pd_breach (
  organisation_id   uuid        NOT NULL DEFAULT fyniq.current_org(),
  id                uuid        NOT NULL DEFAULT uuidv7(),
  title             text        NOT NULL,
  description       text        NOT NULL,
  aware_at          timestamptz NOT NULL,  -- when the company became aware of it
  assessment_due_on date        NOT NULL,  -- 30 days after, in Singapore
  individuals       integer     NOT NULL,  -- how many people, as best known
  classes           text[]      NOT NULL,  -- the kinds of data involved
  prescribed        boolean     NOT NULL,  -- deemed to cause significant harm
  recorded_by       text        NOT NULL,
  recorded_at       timestamptz NOT NULL,
  CONSTRAINT pd_breach_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT pd_breach_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT pd_breach_texts CHECK (length(btrim(title)) BETWEEN 1 AND 200 AND length(btrim(description)) BETWEEN 1 AND 8000),
  CONSTRAINT pd_breach_individuals CHECK (individuals >= 0),
  CONSTRAINT pd_breach_classes_known CHECK (classes <@ ARRAY['names', 'identification_numbers', 'pay', 'bank_accounts', 'card_numbers',
    'credentials', 'contact_details', 'other']::text[] AND cardinality(classes) > 0),
  CONSTRAINT pd_breach_prescribed CHECK (prescribed = ((classes && ARRAY['names', 'identification_numbers']::text[]
    AND classes && ARRAY['pay', 'bank_accounts', 'card_numbers']::text[]) OR 'credentials' = ANY(classes))),
  CONSTRAINT pd_breach_assessment_due CHECK (assessment_due_on = (aware_at AT TIME ZONE 'Asia/Singapore')::date + 30),
  CONSTRAINT pd_breach_recorded_by_format CHECK (recorded_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);

CREATE TABLE fyniq.pd_breach_step (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  breach_id       uuid        NOT NULL,
  step            text        NOT NULL,  -- assessed, decided, pdpc_notified, individuals_notified, contained or closed
  at              timestamptz NOT NULL,
  by_actor        text        NOT NULL,
  note            text,
  notifiable      boolean,               -- decided's
  pdpc_due_on     date,                  -- decided notifiable's: 3 calendar days after the decision, in Singapore
  reference       text,                  -- pdpc_notified's: the PDPC's reference
  CONSTRAINT pd_breach_step_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT pd_breach_step_breach_fkey FOREIGN KEY (organisation_id, breach_id) REFERENCES fyniq.pd_breach (organisation_id, id),
  CONSTRAINT pd_breach_step_known CHECK (step IN ('assessed', 'decided', 'pdpc_notified', 'individuals_notified', 'contained', 'closed')),
  CONSTRAINT pd_breach_step_decision CHECK ((step = 'decided') = (notifiable IS NOT NULL)),
  CONSTRAINT pd_breach_step_pdpc_due CHECK ((pdpc_due_on IS NOT NULL) = (step = 'decided' AND notifiable)
    AND (pdpc_due_on IS NULL OR pdpc_due_on = (at AT TIME ZONE 'Asia/Singapore')::date + 3)),
  CONSTRAINT pd_breach_step_reference CHECK ((step = 'pdpc_notified') = (reference IS NOT NULL) AND (reference IS NULL OR length(btrim(reference)) BETWEEN 1 AND 200)),
  CONSTRAINT pd_breach_step_note_length CHECK (length(note) BETWEEN 1 AND 4000),
  CONSTRAINT pd_breach_step_by_format CHECK (by_actor ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);
CREATE INDEX pd_breach_step_breach ON fyniq.pd_breach_step (organisation_id, breach_id, at);

CREATE TABLE fyniq.pd_breach_exercise (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  held_on         date        NOT NULL,
  scenario        text        NOT NULL,
  findings        text        NOT NULL,
  recorded_by     text        NOT NULL,
  recorded_at     timestamptz NOT NULL,
  CONSTRAINT pd_breach_exercise_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT pd_breach_exercise_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT pd_breach_exercise_texts CHECK (length(btrim(scenario)) BETWEEN 1 AND 4000 AND length(btrim(findings)) BETWEEN 1 AND 8000),
  CONSTRAINT pd_breach_exercise_by_format CHECK (recorded_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);

-- Steps come in order: each once; nothing after closing; deciding after assessing; a breach deemed to cause significant
-- harm, or affecting 500 or more people, is never decided not notifiable; the PDPC notified only of a notifiable breach;
-- closing after the decision and, for a notifiable breach, after the PDPC is notified. Nothing changes or goes.
-- +goose StatementBegin
CREATE FUNCTION fyniq.pd_breach_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  b          fyniq.pd_breach;
  steps      text[];
  notifiable boolean;
BEGIN
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'Breaches, their steps and the exercises never change or go' USING ERRCODE = 'FY315',
      HINT = 'Add the breach''s next step (CMP-PD-05).';
  END IF;
  IF TG_TABLE_NAME = 'pd_breach_step' THEN
    SELECT * INTO b FROM fyniq.pd_breach x WHERE x.organisation_id = NEW.organisation_id AND x.id = NEW.breach_id;
    SELECT coalesce(array_agg(s.step ORDER BY s.at, s.id), '{}'), bool_or(s.notifiable) INTO steps, notifiable FROM fyniq.pd_breach_step s
      WHERE s.organisation_id = NEW.organisation_id AND s.breach_id = NEW.breach_id;
    IF 'closed' = ANY(steps) THEN
      RAISE EXCEPTION 'The breach is closed' USING ERRCODE = 'FY315';
    END IF;
    IF NEW.step = ANY(steps) THEN
      RAISE EXCEPTION 'The breach was % already', NEW.step USING ERRCODE = 'FY315';
    END IF;
    IF NEW.step = 'decided' AND NOT 'assessed' = ANY(steps) THEN
      RAISE EXCEPTION 'A breach is decided after it is assessed' USING ERRCODE = 'FY315';
    END IF;
    IF NEW.step = 'decided' AND NOT NEW.notifiable AND (b.prescribed OR b.individuals >= 500) THEN
      RAISE EXCEPTION 'A breach of prescribed data, or of 500 or more people, is notifiable' USING ERRCODE = 'FY315',
        HINT = 'The Notification of Data Breaches Regulations 2021 deem it to cause significant harm, or of significant scale.';
    END IF;
    IF NEW.step = 'pdpc_notified' AND NOT coalesce(notifiable, false) THEN
      RAISE EXCEPTION 'The PDPC is notified of a breach decided notifiable' USING ERRCODE = 'FY315';
    END IF;
    IF NEW.step = 'closed' AND (NOT 'decided' = ANY(steps) OR (coalesce(notifiable, false) AND NOT 'pdpc_notified' = ANY(steps))) THEN
      RAISE EXCEPTION 'A breach is closed after its decision, and a notifiable one after the PDPC is notified' USING ERRCODE = 'FY315';
    END IF;
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER pd_breach_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.pd_breach
  FOR EACH ROW EXECUTE FUNCTION fyniq.pd_breach_guard();
CREATE TRIGGER pd_breach_step_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.pd_breach_step
  FOR EACH ROW EXECUTE FUNCTION fyniq.pd_breach_guard();
CREATE TRIGGER pd_breach_exercise_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.pd_breach_exercise
  FOR EACH ROW EXECUTE FUNCTION fyniq.pd_breach_guard();
CREATE TRIGGER pd_breach_no_truncate BEFORE TRUNCATE ON fyniq.pd_breach
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE TRIGGER pd_breach_step_no_truncate BEFORE TRUNCATE ON fyniq.pd_breach_step
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE TRIGGER pd_breach_exercise_no_truncate BEFORE TRUNCATE ON fyniq.pd_breach_exercise
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.pd_breach
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('pd_breach', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.pd_breach_step
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('pd_breach', 'breach_id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.pd_breach_exercise
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('pd_breach_exercise', 'id');

ALTER TABLE fyniq.pd_breach ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.pd_breach FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.pd_breach
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.pd_breach_step ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.pd_breach_step FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.pd_breach_step
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.pd_breach_exercise ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.pd_breach_exercise FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.pd_breach_exercise
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

GRANT INSERT (title, description, aware_at, assessment_due_on, individuals, classes, prescribed, recorded_by, recorded_at) ON fyniq.pd_breach TO fyniq_api;
GRANT INSERT (breach_id, step, at, by_actor, note, notifiable, pdpc_due_on, reference) ON fyniq.pd_breach_step TO fyniq_api;
GRANT INSERT (held_on, scenario, findings, recorded_by, recorded_at) ON fyniq.pd_breach_exercise TO fyniq_api;
GRANT SELECT ON fyniq.pd_breach, fyniq.pd_breach_step, fyniq.pd_breach_exercise TO fyniq_api, fyniq_worker, fyniq_readonly;
