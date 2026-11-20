-- After a restore of Nemura's database the link's work after the restore point is held as unknown and reconciled before anything is sent or paid (PRD-002 NM-INT-13; PRD-001 FR-PPL-19,
-- DR-06; p1-nemura-fyniq-link task 5.2, design D8).
--
--  * link_restore records the restore: the link's checkpoint as the restored database knows it (the highest sequence it queued and the highest it sent, and the highest Fyniq sequence it received) and who ran
--    the recovery. The link is suspended in the same transaction, so nothing is sent while it is open.
--  * link_restore_run lists the runs approved with proof whose answer the restored database does not hold: for each, Fyniq may have posted it after the restore point. Each is decided against Fyniq's own
--    receipt (posted, with its reference, or not posted) by an Owner or Administrator, and when all are decided the restore is reconciled and the link resumes.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE nemura.link_restore (
  organisation_id      uuid        NOT NULL DEFAULT nemura.current_org(),
  id                   uuid        NOT NULL DEFAULT uuidv7(),
  link_id              uuid        NOT NULL,
  recorded_at          timestamptz NOT NULL,
  recorded_by          text        NOT NULL,   -- the two operators' names
  last_sequence_queued bigint      NOT NULL,
  last_sequence_sent   bigint      NOT NULL,
  last_fyniq_sequence  bigint      NOT NULL,
  state                text        NOT NULL DEFAULT 'open',
  reconciled_at        timestamptz,
  reconciled_by        uuid,
  CONSTRAINT link_restore_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT link_restore_link_fkey FOREIGN KEY (organisation_id, link_id) REFERENCES nemura.fyniq_link (organisation_id, id),
  CONSTRAINT link_restore_reconciler_fkey FOREIGN KEY (organisation_id, reconciled_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT link_restore_state CHECK (state IN ('open', 'reconciled')),
  CONSTRAINT link_restore_reconciled_whole CHECK ((state = 'reconciled') = (reconciled_at IS NOT NULL AND reconciled_by IS NOT NULL))
);
CREATE UNIQUE INDEX link_restore_one_open ON nemura.link_restore (organisation_id) WHERE state = 'open';

CREATE TABLE nemura.link_restore_run (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  restore_id      uuid        NOT NULL,
  run_id          uuid        NOT NULL,
  version         integer     NOT NULL,
  outcome         text,
  reference       text,
  decided_by      uuid,
  decided_at      timestamptz,
  CONSTRAINT link_restore_run_pkey PRIMARY KEY (organisation_id, restore_id, run_id, version),
  CONSTRAINT link_restore_run_restore_fkey FOREIGN KEY (organisation_id, restore_id) REFERENCES nemura.link_restore (organisation_id, id),
  CONSTRAINT link_restore_run_run_fkey FOREIGN KEY (organisation_id, run_id) REFERENCES nemura.pay_run (organisation_id, id),
  CONSTRAINT link_restore_run_decider_fkey FOREIGN KEY (organisation_id, decided_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT link_restore_run_outcome CHECK (outcome IS NULL OR outcome IN ('posted', 'not_posted')),
  CONSTRAINT link_restore_run_whole CHECK ((outcome IS NULL) = (decided_by IS NULL AND decided_at IS NULL)),
  CONSTRAINT link_restore_run_reference CHECK ((outcome = 'posted') = (reference IS NOT NULL))
);

-- +goose StatementBegin
CREATE FUNCTION nemura.link_restore_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A restore is kept as it was recorded' USING ERRCODE = 'NM193';
  END IF;
  IF TG_TABLE_NAME = 'link_restore' THEN
    IF (NEW.id, NEW.link_id, NEW.recorded_at, NEW.recorded_by, NEW.last_sequence_queued, NEW.last_sequence_sent, NEW.last_fyniq_sequence) IS DISTINCT FROM
       (OLD.id, OLD.link_id, OLD.recorded_at, OLD.recorded_by, OLD.last_sequence_queued, OLD.last_sequence_sent, OLD.last_fyniq_sequence) OR OLD.state <> 'open' THEN
      RAISE EXCEPTION 'A restore is kept as it was recorded: it is only reconciled, once' USING ERRCODE = 'NM193';
    END IF;
  ELSIF (NEW.restore_id, NEW.run_id, NEW.version) IS DISTINCT FROM (OLD.restore_id, OLD.run_id, OLD.version) OR OLD.outcome IS NOT NULL THEN
    RAISE EXCEPTION 'A run held by a restore is decided once' USING ERRCODE = 'NM193';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER link_restore_guard BEFORE UPDATE OR DELETE ON nemura.link_restore FOR EACH ROW EXECUTE FUNCTION nemura.link_restore_guard();
CREATE TRIGGER link_restore_run_guard BEFORE UPDATE OR DELETE ON nemura.link_restore_run FOR EACH ROW EXECUTE FUNCTION nemura.link_restore_guard();

ALTER TABLE nemura.link_restore ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.link_restore FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.link_restore USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.link_restore_run ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.link_restore_run FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.link_restore_run USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());

REVOKE ALL ON nemura.link_restore, nemura.link_restore_run FROM PUBLIC;
GRANT SELECT ON nemura.link_restore, nemura.link_restore_run TO nemura_api, nemura_worker, nemura_readonly;
-- The deployment's recovery (the migrator) records a restore; the people team decides it through the API.
GRANT UPDATE (state, reconciled_at, reconciled_by) ON nemura.link_restore TO nemura_api;
GRANT UPDATE (outcome, reference, decided_by, decided_at) ON nemura.link_restore_run TO nemura_api;
