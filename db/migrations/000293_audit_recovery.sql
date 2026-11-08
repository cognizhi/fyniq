-- Restore behind the anchors (PRD-001 AUD-09, G4-070, G4-071; p1-audit-explorer slice 10, design D8).
--
-- A restore from a backup can land behind an anchor already published: the chain then continues from the restored head,
-- and its new events take sequence numbers the old anchors sign. The recovery step (audit.Recover, run first by the
-- nightly verification and by `make audit-recover`) finds it, and records it here: the restored head (the last event
-- whose hash matches every anchor), the anchored range now missing, and where its signed copy is. Each recovery starts
-- a new anchor epoch, whose anchors are written under audit-anchors/<org>/e<epoch>/; the old epochs' anchors past the
-- restored head are explained by it. The check of the audit trail stays failed until the Owner acknowledges it (the
-- design's open question: nobody holds A on "Audit trail" yet), once, in a row of its own: like the trail, neither ever
-- changes or goes (AUD-08).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.audit_recovery (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  epoch           integer     NOT NULL,  -- the anchor epoch it starts
  restored_seq    bigint      NOT NULL,  -- the last event whose hash matches every anchor (0: none)
  restored_hash   bytea       NOT NULL,
  missing_from    bigint      NOT NULL,  -- the anchored range no longer proven by the chain
  missing_to      bigint      NOT NULL,
  detected_at     timestamptz NOT NULL,
  anchor_key      text        NOT NULL,  -- its signed copy in the anchors' bucket
  CONSTRAINT audit_recovery_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT audit_recovery_epoch_key UNIQUE (organisation_id, epoch),
  CONSTRAINT audit_recovery_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT audit_recovery_epoch_positive CHECK (epoch > 0),
  CONSTRAINT audit_recovery_range CHECK (restored_seq >= 0 AND missing_from = restored_seq + 1 AND missing_to >= missing_from),
  CONSTRAINT audit_recovery_hash_sha256 CHECK (length(restored_hash) = 32),
  CONSTRAINT audit_recovery_key_length CHECK (length(anchor_key) BETWEEN 1 AND 300)
);

CREATE TABLE fyniq.audit_recovery_acknowledgement (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  recovery_id     uuid        NOT NULL,
  acknowledged_by uuid        NOT NULL,
  acknowledged_at timestamptz NOT NULL,
  CONSTRAINT audit_recovery_acknowledgement_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT audit_recovery_acknowledgement_once UNIQUE (organisation_id, recovery_id),
  CONSTRAINT audit_recovery_acknowledgement_recovery_fkey FOREIGN KEY (organisation_id, recovery_id)
    REFERENCES fyniq.audit_recovery (organisation_id, id)
);

-- A recovery starts the next epoch, and never changes or goes.
-- +goose StatementBegin
CREATE FUNCTION fyniq.audit_recovery_guard() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF NEW.epoch <> coalesce((SELECT max(r.epoch) FROM fyniq.audit_recovery r WHERE r.organisation_id = NEW.organisation_id), 0) + 1 THEN
      RAISE EXCEPTION 'A recovery starts the next anchor epoch, not %', NEW.epoch USING ERRCODE = 'FY306';
    END IF;
    RETURN NEW;
  END IF;
  RAISE EXCEPTION 'A recovery of the audit trail never changes or goes' USING ERRCODE = 'FY306';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER audit_recovery_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.audit_recovery
  FOR EACH ROW EXECUTE FUNCTION fyniq.audit_recovery_guard();
CREATE TRIGGER audit_recovery_no_truncate BEFORE TRUNCATE ON fyniq.audit_recovery
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.audit_recovery
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('audit_recovery', 'id');
CREATE TRIGGER audit_recovery_acknowledgement_immutable BEFORE UPDATE OR DELETE ON fyniq.audit_recovery_acknowledgement
  FOR EACH ROW EXECUTE FUNCTION fyniq.audit_immutable();
CREATE TRIGGER audit_recovery_acknowledgement_no_truncate BEFORE TRUNCATE ON fyniq.audit_recovery_acknowledgement
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.audit_recovery_acknowledgement
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('audit_recovery_acknowledgement', 'id');

ALTER TABLE fyniq.audit_recovery ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.audit_recovery FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.audit_recovery
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.audit_recovery_acknowledgement ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.audit_recovery_acknowledgement FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.audit_recovery_acknowledgement
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

GRANT INSERT (epoch, restored_seq, restored_hash, missing_from, missing_to, detected_at, anchor_key) ON fyniq.audit_recovery TO fyniq_worker;
GRANT INSERT (recovery_id, acknowledged_by, acknowledged_at) ON fyniq.audit_recovery_acknowledgement TO fyniq_api;
GRANT SELECT ON fyniq.audit_recovery, fyniq.audit_recovery_acknowledgement TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
