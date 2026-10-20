-- An organisation activates a statutory pack version through the approval engine (PRD-001 ENG-32, SOD-10,
-- FR-ADM-04, rev 0.41; p1-statutory-packs task 3.1, p1-approval-engine task 4.1).
--
-- 000034 made the activation row and let no role insert it until the engine existed. The engine executes an
-- approved change in the API's unit of work, so the API may now insert one, and only as the record of an approved
-- request: the request is approved, asks to activate exactly this version, was made by the proposer, and the
-- approver signed its envelope. An activation is never changed or deleted. Activating a version is an object the
-- engine knows, with a policy an organisation may change. The checks on the request and policy tables are widened
-- here NOT VALID and validated by 000042 in a transaction of their own (squawk: constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.statutory_pack_activation
  ADD CONSTRAINT statutory_pack_activation_approval_fkey FOREIGN KEY (organisation_id, approval_id)
    REFERENCES fyniq.approval_request (organisation_id, id) NOT VALID;

-- +goose StatementBegin
CREATE FUNCTION fyniq.statutory_pack_activation_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  r fyniq.approval_request;
BEGIN
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'An activation of a statutory pack version is never changed or deleted' USING ERRCODE = 'FY107',
      HINT = 'Activate the next version instead (ENG-32, ENG-33).';
  END IF;
  SELECT * INTO r FROM fyniq.approval_request q WHERE q.organisation_id = NEW.organisation_id AND q.id = NEW.approval_id;
  IF r.status IS DISTINCT FROM 'approved' OR r.object_type <> 'statutory_pack' OR r.operation <> 'statutory_pack.activate'
     OR r.object_key <> NEW.version_id OR r.requested_by <> NEW.proposed_by
     OR NOT EXISTS (SELECT 1 FROM fyniq.approval_signature s
                     WHERE s.organisation_id = r.organisation_id AND s.request_id = r.id AND s.signer = NEW.approved_by
                       AND s.decision = 'approve' AND s.voided_at IS NULL AND s.envelope_hash = r.envelope_hash) THEN
    RAISE EXCEPTION 'A statutory pack version is activated only by the approved request to activate it, signed by its approver'
      USING ERRCODE = 'FY107', HINT = 'Ask to activate the version and have another member approve it (ENG-32, SOD-10).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER statutory_pack_activation_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.statutory_pack_activation
  FOR EACH ROW EXECUTE FUNCTION fyniq.statutory_pack_activation_guard();

-- The API inserts the activation when it executes the approved request; nobody changes or deletes one.
GRANT INSERT (version_id, proposed_by, approved_by, approval_id, activated_at) ON fyniq.statutory_pack_activation TO fyniq_api;

-- Activating a pack version is an object the engine knows, with a policy an organisation may change.
ALTER TABLE fyniq.approval_request DROP CONSTRAINT approval_request_object_known;
ALTER TABLE fyniq.approval_request ADD CONSTRAINT approval_request_object_known CHECK (
  (object_type, operation) IN (('organisation_setting', 'organisation_setting.change'), ('membership', 'role.assign'),
                               ('membership', 'role.revoke'), ('member_invitation', 'member_invitation.create'),
                               ('approval_policy', 'approval_policy.change'), ('statutory_pack', 'statutory_pack.activate'))) NOT VALID;
ALTER TABLE fyniq.approval_policy DROP CONSTRAINT approval_policy_object_known;
ALTER TABLE fyniq.approval_policy ADD CONSTRAINT approval_policy_object_known CHECK (
  object_type IN ('organisation_setting', 'membership', 'member_invitation', 'statutory_pack')) NOT VALID;
