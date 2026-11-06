-- Owner succession (PRD-001 FR-ADM-09; rev 0.181, the owner's decisions of 6 Nov 2026; p1-users-roles task 2.4).
--
-- When another Owner is permanently unavailable (departure, incapacity, death), an Owner starts a succession with a
-- reason and the kind of evidence it rests on: a board resolution, or the external accountant's recorded
-- confirmation, attached as files to its own owner_succession document. The outgoing Owner is told at once, and for
-- 7 days may object, which ends it; the Owner who started it may withdraw it. From the 7th day a Super admin, any one,
-- the one who started it included, completes it: the outgoing Owner's membership ends, and the member it names, if
-- any, becomes an Owner. The database is the backstop (FY268):
--   - a succession starts in notice, by an active Owner, of another active Owner, for 7 days exactly, naming an active
--     member other than the outgoing one as successor, if any, and on an owner_succession document; one at a time per
--     outgoing Owner;
--   - it ends once, from notice: objected by the outgoing Owner, withdrawn by the one who started it, or completed by
--     a Super admin from its due time with a file attached; nothing else of it ever changes, and it never goes.
-- The widened document check and the foreign keys are added NOT VALID and validated by 000235 (squawk:
-- constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.document DROP CONSTRAINT document_type_known;
ALTER TABLE fyniq.document ADD CONSTRAINT document_type_known
  CHECK (type IN ('journal', 'opening_balance', 'fx_revaluation', 'bill', 'bill_payment', 'vendor_credit', 'credit_application',
                  'supplier_refund', 'purchase_order', 'invoice', 'customer_receipt', 'write_off', 'credit_note',
                  'customer_credit_application', 'customer_refund', 'customer_deposit', 'quote', 'sales_order', 'statement',
                  'bank_transaction', 'bank_agreement', 'expense_claim', 'claim_reimbursement', 'cash_advance', 'fixed_asset',
                  'asset_disposal', 'payee_bank_change', 'claimant_bank_change', 'payment_batch',
                  'pre_incorporation_cost', 'director_pay_choice', 'director_fee', 'exempt_status',
                  'director_confirmation', 'director_settlement', 'director_repayment', 'account_merge',
                  'owner_succession')) NOT VALID;

CREATE TABLE fyniq.owner_succession (
  organisation_id         uuid        NOT NULL DEFAULT fyniq.current_org(),
  id                      uuid        NOT NULL DEFAULT uuidv7(),
  document_id             uuid        NOT NULL, -- holds its evidence files
  outgoing_membership_id  uuid        NOT NULL,
  outgoing_user_id        uuid        NOT NULL,
  successor_membership_id uuid,                  -- the member who becomes an Owner on completion, if any
  reason                  text        NOT NULL,
  evidence_kind           text        NOT NULL,
  started_by              uuid        NOT NULL,
  started_at              timestamptz NOT NULL,
  due_at                  timestamptz NOT NULL,
  status                  text        NOT NULL DEFAULT 'notice',
  ended_by                uuid,
  ended_at                timestamptz,
  version                 integer     NOT NULL DEFAULT 1,
  CONSTRAINT owner_succession_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT owner_succession_document_fkey FOREIGN KEY (organisation_id, document_id) REFERENCES fyniq.document (organisation_id, id) NOT VALID,
  CONSTRAINT owner_succession_outgoing_fkey FOREIGN KEY (organisation_id, outgoing_membership_id)
    REFERENCES fyniq.user_membership (organisation_id, id) NOT VALID,
  CONSTRAINT owner_succession_successor_fkey FOREIGN KEY (organisation_id, successor_membership_id)
    REFERENCES fyniq.user_membership (organisation_id, id) NOT VALID,
  CONSTRAINT owner_succession_outgoing_user_fkey FOREIGN KEY (outgoing_user_id) REFERENCES fyniq.app_user (user_id) NOT VALID,
  CONSTRAINT owner_succession_started_by_fkey FOREIGN KEY (started_by) REFERENCES fyniq.app_user (user_id) NOT VALID,
  CONSTRAINT owner_succession_ended_by_fkey FOREIGN KEY (ended_by) REFERENCES fyniq.app_user (user_id) NOT VALID,
  CONSTRAINT owner_succession_reason_length CHECK (length(btrim(reason)) BETWEEN 1 AND 500),
  CONSTRAINT owner_succession_evidence_known CHECK (evidence_kind IN ('board_resolution', 'accountant_confirmation')),
  CONSTRAINT owner_succession_status_known CHECK (status IN ('notice', 'objected', 'withdrawn', 'completed')),
  CONSTRAINT owner_succession_wait CHECK (due_at = started_at + interval '7 days'),
  CONSTRAINT owner_succession_ended CHECK ((status = 'notice') = (ended_at IS NULL) AND (ended_at IS NULL) = (ended_by IS NULL)
                                           AND (ended_at IS NULL OR ended_at >= started_at)),
  CONSTRAINT owner_succession_version_positive CHECK (version > 0)
);
CREATE UNIQUE INDEX owner_succession_one_open ON fyniq.owner_succession (organisation_id, outgoing_membership_id)
  WHERE status = 'notice';

-- +goose StatementBegin
-- Whether a user is one of the installation's Super admins now (FR-ADM-07): the table is the installation's.
CREATE FUNCTION fyniq.is_super_admin(p_user uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT EXISTS (SELECT 1 FROM fyniq.super_admin s WHERE s.user_id = p_user AND s.removed_at IS NULL)
$$;
-- +goose StatementEnd

-- +goose StatementBegin
-- Whether a membership is active and holds the Owner role now.
CREATE FUNCTION fyniq.is_owner(p_org uuid, p_membership uuid, p_now timestamptz) RETURNS boolean
LANGUAGE sql STABLE
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT EXISTS (SELECT 1 FROM fyniq.role_assignment r
                   JOIN fyniq.user_membership m ON m.organisation_id = r.organisation_id AND m.id = r.membership_id
                  WHERE r.organisation_id = p_org AND r.membership_id = p_membership AND m.status = 'active' AND r.role = 'owner'
                    AND r.revoked_at IS NULL AND (r.expires_at IS NULL OR r.expires_at > p_now))
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION fyniq.owner_succession_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  starter uuid;
BEGIN
  IF TG_OP IN ('DELETE', 'TRUNCATE') THEN
    RAISE EXCEPTION 'A succession is never deleted' USING ERRCODE = 'FY268';
  END IF;
  IF TG_OP = 'INSERT' THEN
    SELECT m.id INTO starter FROM fyniq.user_membership m WHERE m.organisation_id = NEW.organisation_id AND m.user_id = NEW.started_by;
    IF NEW.status <> 'notice' OR NEW.version <> 1 OR NEW.started_by = NEW.outgoing_user_id
       OR NOT fyniq.is_owner(NEW.organisation_id, starter, NEW.started_at)
       OR NOT fyniq.is_owner(NEW.organisation_id, NEW.outgoing_membership_id, NEW.started_at)
       OR NOT EXISTS (SELECT 1 FROM fyniq.user_membership m WHERE m.organisation_id = NEW.organisation_id
                       AND m.id = NEW.outgoing_membership_id AND m.user_id = NEW.outgoing_user_id)
       OR (NEW.successor_membership_id IS NOT NULL AND (NEW.successor_membership_id = NEW.outgoing_membership_id
           OR NOT EXISTS (SELECT 1 FROM fyniq.user_membership m WHERE m.organisation_id = NEW.organisation_id
                           AND m.id = NEW.successor_membership_id AND m.status = 'active')))
       OR NOT EXISTS (SELECT 1 FROM fyniq.document d WHERE d.organisation_id = NEW.organisation_id AND d.id = NEW.document_id
                       AND d.type = 'owner_succession') THEN
      RAISE EXCEPTION 'A succession starts in notice, by an active Owner, of another active Owner, naming an active member as successor'
        USING ERRCODE = 'FY268', HINT = 'An Owner starts it for another Owner who is permanently unavailable (FR-ADM-09).';
    END IF;
    RETURN NEW;
  END IF;
  IF OLD.status <> 'notice' OR NEW.version <> OLD.version + 1
     OR (NEW.document_id, NEW.outgoing_membership_id, NEW.outgoing_user_id, NEW.successor_membership_id, NEW.reason, NEW.evidence_kind,
         NEW.started_by, NEW.started_at, NEW.due_at)
        IS DISTINCT FROM (OLD.document_id, OLD.outgoing_membership_id, OLD.outgoing_user_id, OLD.successor_membership_id, OLD.reason,
         OLD.evidence_kind, OLD.started_by, OLD.started_at, OLD.due_at) THEN
    RAISE EXCEPTION 'Succession % ends once, from notice, and nothing else of it changes', OLD.id USING ERRCODE = 'FY268';
  END IF;
  IF (NEW.status = 'objected' AND NEW.ended_by IS DISTINCT FROM OLD.outgoing_user_id)
     OR (NEW.status = 'withdrawn' AND NEW.ended_by IS DISTINCT FROM OLD.started_by) THEN
    RAISE EXCEPTION 'Only the outgoing Owner objects to succession %, and only the one who started it withdraws it', OLD.id
      USING ERRCODE = 'FY268';
  END IF;
  IF NEW.status = 'completed' AND (NEW.ended_at < OLD.due_at OR NOT fyniq.is_super_admin(NEW.ended_by)
      OR NOT EXISTS (SELECT 1 FROM fyniq.evidence e WHERE e.organisation_id = NEW.organisation_id AND e.document_id = NEW.document_id
                      AND e.removed_at IS NULL)) THEN
    RAISE EXCEPTION 'Succession % is completed by a Super admin, from its due time, with a file attached', OLD.id
      USING ERRCODE = 'FY268', HINT = 'Attach the board resolution or the accountant''s confirmation, and wait the 7 days of notice (FR-ADM-09).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER owner_succession_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.owner_succession
  FOR EACH ROW EXECUTE FUNCTION fyniq.owner_succession_guard();
CREATE TRIGGER owner_succession_no_truncate BEFORE TRUNCATE ON fyniq.owner_succession
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.owner_succession_guard();

ALTER TABLE fyniq.owner_succession ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.owner_succession FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.owner_succession
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.owner_succession
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('owner_succession', 'id');

REVOKE ALL ON fyniq.owner_succession FROM PUBLIC;
GRANT SELECT ON fyniq.owner_succession TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (document_id, outgoing_membership_id, outgoing_user_id, successor_membership_id, reason, evidence_kind, started_by,
  started_at, due_at) ON fyniq.owner_succession TO fyniq_api;
GRANT UPDATE (status, ended_by, ended_at, version) ON fyniq.owner_succession TO fyniq_api;

REVOKE ALL ON FUNCTION fyniq.is_super_admin(uuid), fyniq.is_owner(uuid, uuid, timestamptz), fyniq.owner_succession_guard() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.is_super_admin(uuid), fyniq.is_owner(uuid, uuid, timestamptz) TO fyniq_api;
