-- Reopening a closed period (PRD-001 FR-GL-08, FM-1, AUD-04; rev 0.51; p1-general-ledger-close task 1.3).
--
-- A closed period reopens only through an approved request: a second person approves, with a reason (FM-1). The
-- approved request reopens the latest closed period under the ledger lock (ENG-47), and period_reopen records it with
-- the close it undoes, the approval, the reason, and the snapshots before (the close's) and after (the trial balance at
-- the period end when it reopened), whose SHA-256 the database recomputes. A reopening never changes or goes (FY154),
-- and names the period's latest close and an approved request to reopen that period, signed by the one who reopens it
-- (FY155).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Periods join the approval engine's object types: a request to reopen one, and an organisation's policy for it. The
-- widened checks are added NOT VALID and validated by 000058 (squawk: constraint-missing-not-valid).
ALTER TABLE fyniq.approval_request DROP CONSTRAINT approval_request_object_known;
ALTER TABLE fyniq.approval_request ADD CONSTRAINT approval_request_object_known CHECK (
  (object_type, operation) IN (('organisation_setting', 'organisation_setting.change'), ('membership', 'role.assign'),
                               ('membership', 'role.revoke'), ('member_invitation', 'member_invitation.create'),
                               ('approval_policy', 'approval_policy.change'), ('statutory_pack', 'statutory_pack.activate'),
                               ('team_mode', 'team_mode.change'), ('period', 'period.reopen'))) NOT VALID;
ALTER TABLE fyniq.approval_policy DROP CONSTRAINT approval_policy_object_known;
ALTER TABLE fyniq.approval_policy ADD CONSTRAINT approval_policy_object_known CHECK (
  object_type IN ('organisation_setting', 'membership', 'member_invitation', 'statutory_pack', 'team_mode', 'period')) NOT VALID;

CREATE TABLE fyniq.period_reopen (
  organisation_id   uuid        NOT NULL DEFAULT fyniq.current_org(),
  id                uuid        NOT NULL DEFAULT uuidv7(),
  period_id         uuid        NOT NULL,
  close_id          uuid        NOT NULL,  -- the close it undoes
  approval_id       uuid        NOT NULL,  -- the approved request (FM-1)
  reason            text        NOT NULL,
  reopened_by       text        NOT NULL,  -- "type:id": the final approver, as the audit trail names them
  reopened_at       timestamptz NOT NULL,
  reopened_revision bigint      NOT NULL,
  before_hash       bytea       NOT NULL,  -- the close's snapshot
  after_snapshot    text        NOT NULL,  -- the trial balance at the period end when it reopened, canonical JSON
  after_hash        bytea       NOT NULL,
  CONSTRAINT period_reopen_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT period_reopen_close_key UNIQUE (organisation_id, close_id),
  CONSTRAINT period_reopen_period_fkey FOREIGN KEY (organisation_id, period_id) REFERENCES fyniq.period (organisation_id, id),
  CONSTRAINT period_reopen_close_fkey FOREIGN KEY (organisation_id, close_id) REFERENCES fyniq.period_close (organisation_id, id),
  CONSTRAINT period_reopen_approval_fkey FOREIGN KEY (organisation_id, approval_id) REFERENCES fyniq.approval_request (organisation_id, id),
  CONSTRAINT period_reopen_actor_format CHECK (reopened_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$'),
  CONSTRAINT period_reopen_reason_length CHECK (length(btrim(reason)) BETWEEN 1 AND 500),
  CONSTRAINT period_reopen_revision CHECK (reopened_revision >= 0),
  CONSTRAINT period_reopen_before_hash CHECK (octet_length(before_hash) = 32),
  CONSTRAINT period_reopen_after_hash CHECK (after_hash = sha256(convert_to(after_snapshot, 'UTF8')))
);
CREATE INDEX period_reopen_period ON fyniq.period_reopen (organisation_id, period_id, reopened_at);

-- A reopening never changes; it names the period's latest close, its snapshot, and an approved request to reopen that
-- period, which the one who reopens it signed.
-- +goose StatementBegin
CREATE FUNCTION fyniq.period_reopen_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  c fyniq.period_close;
BEGIN
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'A reopening never changes' USING ERRCODE = 'FY154', HINT = 'Close the period again, and reopen it again if needed.';
  END IF;
  SELECT * INTO c FROM fyniq.period_close x
   WHERE x.organisation_id = NEW.organisation_id AND x.period_id = NEW.period_id
   ORDER BY x.attested_at DESC, x.id DESC LIMIT 1;
  IF c.id IS DISTINCT FROM NEW.close_id OR c.snapshot_hash <> NEW.before_hash OR NOT EXISTS (
       SELECT 1 FROM fyniq.approval_request r
        WHERE r.organisation_id = NEW.organisation_id AND r.id = NEW.approval_id AND r.object_type = 'period'
          AND r.object_key = NEW.period_id::text AND r.operation = 'period.reopen' AND r.status = 'approved'
          AND EXISTS (SELECT 1 FROM fyniq.approval_signature s
                       WHERE s.organisation_id = r.organisation_id AND s.request_id = r.id AND s.decision = 'approve'
                         AND s.voided_at IS NULL AND 'user:' || s.signer::text = NEW.reopened_by)) THEN
    RAISE EXCEPTION 'A period reopens on its latest close, by an approved request to reopen it' USING ERRCODE = 'FY155',
      HINT = 'Ask to reopen the period; a second founder approves (FM-1).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER period_reopen_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.period_reopen
  FOR EACH ROW EXECUTE FUNCTION fyniq.period_reopen_guard();
CREATE TRIGGER period_reopen_no_truncate BEFORE TRUNCATE ON fyniq.period_reopen
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();

ALTER TABLE fyniq.period_reopen ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.period_reopen FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.period_reopen
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.period_reopen
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('period_reopen', 'id');

GRANT SELECT ON fyniq.period_reopen TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
GRANT INSERT (period_id, close_id, approval_id, reason, reopened_by, reopened_at, reopened_revision, before_hash, after_snapshot, after_hash)
  ON fyniq.period_reopen TO fyniq_api;
