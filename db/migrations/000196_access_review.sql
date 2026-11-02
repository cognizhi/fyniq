-- The user access review (PRD-001 FR-CTL-06, SEC-06; rev 0.162; p1-checks task 4.1, design D10).
--
-- Settings › Access review shows who can do what, live: each member's roles, last sign-in and second factor, the role
-- conflicts and what the team mode allows, external members and their expiry, and the integration clients. An Owner
-- attests it once a calendar quarter has ended. The database is the backstop:
--   - access_review_attestation: one per organisation and quarter, written once by the API with the report as the Owner
--     saw it (canonical JSON) and its SHA-256, which the database recomputes; audited on the attestation in the same
--     transaction (AUD-01); never changed or deleted (FY247);
--   - member_sign_ins(): each active member of the current organisation, their last sign-in and how their latest
--     session passed its second factor, read through this function only: the sessions themselves stay the
--     installation's.
--
-- A member is removed from the organisation (rev 0.165, the owner's decision of 2 Nov 2026; FR-ADM-01, SEC-06): an
-- approved request after the founder bootstrap, like a role change ('member.remove'). The database is the backstop:
--   - a membership is removed only once each of its roles has ended, and a removed member is given no role (FY248);
--     the organisation's last Owner keeps the role (FY093), so it is never removed;
--   - a removed person who accepts a new invitation is a member again: the same membership, active once more.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.access_review_attestation (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  quarter         date        NOT NULL,
  attested_by     text        NOT NULL,
  attested_at     timestamptz NOT NULL,
  note            text,
  report          text        NOT NULL,
  report_hash     bytea       NOT NULL,
  CONSTRAINT access_review_attestation_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT access_review_attestation_quarter_key UNIQUE (organisation_id, quarter),
  CONSTRAINT access_review_attestation_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT access_review_attestation_quarter_start CHECK (quarter = date_trunc('quarter', quarter)::date),
  CONSTRAINT access_review_attestation_after_its_quarter CHECK (attested_at >= quarter + interval '3 months'),
  CONSTRAINT access_review_attestation_attested_by CHECK (attested_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$'),
  CONSTRAINT access_review_attestation_note CHECK (length(note) BETWEEN 1 AND 1000),
  CONSTRAINT access_review_attestation_report CHECK (octet_length(report) <= 1048576),
  CONSTRAINT access_review_attestation_report_hash CHECK (report_hash = sha256(convert_to(report, 'UTF8')))
);

-- +goose StatementBegin
-- An attestation is written once and never changed or deleted.
CREATE FUNCTION fyniq.access_review_attestation_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'An access review''s attestation is never changed or deleted'
    USING ERRCODE = 'FY247', HINT = 'Each quarter is attested once (FR-CTL-06).';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER access_review_attestation_guard BEFORE UPDATE OR DELETE ON fyniq.access_review_attestation
  FOR EACH ROW EXECUTE FUNCTION fyniq.access_review_attestation_guard();
CREATE TRIGGER access_review_attestation_no_truncate BEFORE TRUNCATE ON fyniq.access_review_attestation
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.access_review_attestation_guard();

ALTER TABLE fyniq.access_review_attestation ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.access_review_attestation FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.access_review_attestation
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

-- Attested: audited on the attestation in the same transaction (AUD-01).
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.access_review_attestation
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('access_review_attestation', 'id');

REVOKE ALL ON fyniq.access_review_attestation FROM PUBLIC;
GRANT SELECT ON fyniq.access_review_attestation TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (quarter, attested_by, attested_at, note, report, report_hash) ON fyniq.access_review_attestation TO fyniq_api;

-- +goose StatementBegin
-- The current organisation's active members who have signed in, their last sign-in by any of their identities, and
-- how their latest session passed its second factor ('sign_in', 'passkey', or '' for none): what the access review
-- shows (FR-CTL-06). A member who never signed in has no row.
CREATE FUNCTION fyniq.member_sign_ins() RETURNS TABLE (user_id uuid, last_sign_in timestamptz, step_up_method text)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT m.user_id, s.created_at, coalesce(s.step_up_method, '')
    FROM fyniq.user_membership m
    JOIN LATERAL (
      SELECT us.created_at, us.step_up_method FROM fyniq.user_session us
        JOIN fyniq.user_identity i ON i.issuer = us.issuer AND i.subject = us.subject
       WHERE i.user_id = m.user_id
       ORDER BY us.created_at DESC
       LIMIT 1) s ON true
   WHERE m.organisation_id = fyniq.current_org() AND m.status = 'active'
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION fyniq.member_sign_ins() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.member_sign_ins() TO fyniq_api, fyniq_worker;

-- +goose StatementBegin
-- A removed membership holds no role that has not ended, and is given none (FY248).
CREATE FUNCTION fyniq.removed_member_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_TABLE_NAME = 'user_membership' THEN
    IF NEW.status = 'removed' AND OLD.status = 'active' AND EXISTS (
        SELECT 1 FROM fyniq.role_assignment r
         WHERE r.organisation_id = NEW.organisation_id AND r.membership_id = NEW.id AND r.revoked_at IS NULL) THEN
      RAISE EXCEPTION 'A member is removed only once each of their roles has ended' USING ERRCODE = 'FY248',
        HINT = 'End their roles, expired ones included, in the same transaction (FR-ADM-01).';
    END IF;
  ELSIF EXISTS (SELECT 1 FROM fyniq.user_membership m
                 WHERE m.organisation_id = NEW.organisation_id AND m.id = NEW.membership_id AND m.status = 'removed') THEN
    RAISE EXCEPTION 'A removed member is given no role' USING ERRCODE = 'FY248',
      HINT = 'Invite them again: accepting the invitation makes them a member once more (FR-ADM-01).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

CREATE TRIGGER removed_member_guard BEFORE UPDATE OF status ON fyniq.user_membership
  FOR EACH ROW EXECUTE FUNCTION fyniq.removed_member_guard();
CREATE TRIGGER removed_member_guard BEFORE INSERT ON fyniq.role_assignment
  FOR EACH ROW EXECUTE FUNCTION fyniq.removed_member_guard();

-- Removing a member is an approved request after the founder bootstrap (FM-1), as a role change is.
ALTER TABLE fyniq.approval_request DROP CONSTRAINT approval_request_object_known;
ALTER TABLE fyniq.approval_request ADD CONSTRAINT approval_request_object_known CHECK (
  (object_type, operation) IN (('organisation_setting', 'organisation_setting.change'), ('membership', 'role.assign'),
                               ('membership', 'role.revoke'), ('membership', 'member.remove'),
                               ('member_invitation', 'member_invitation.create'),
                               ('approval_policy', 'approval_policy.change'), ('statutory_pack', 'statutory_pack.activate'),
                               ('team_mode', 'team_mode.change'), ('period', 'period.reopen'),
                               ('journal', 'journal.post'), ('journal', 'journal.reverse'), ('journal', 'journal.edit'),
                               ('opening_balance', 'opening_balance.edit'),
                               ('bill', 'bill.post'), ('bill', 'bill.void'), ('bill', 'bill.edit'),
                               ('bill_payment', 'bill_payment.void'),
                               ('credit_application', 'credit_application.void'),
                               ('supplier_refund', 'supplier_refund.void'),
                               ('purchase_order', 'purchase_order.approve'),
                               ('document_template', 'document_template.publish'),
                               ('invoice', 'invoice.post'), ('invoice', 'invoice.void'), ('invoice', 'invoice.edit'),
                               ('customer_receipt', 'customer_receipt.void'),
                               ('write_off', 'write_off.post'), ('write_off', 'write_off.void'),
                               ('customer_credit_application', 'customer_credit_application.void'),
                               ('customer_refund', 'customer_refund.void'),
                               ('customer_deposit', 'customer_deposit.void'),
                               ('recurring_invoice', 'recurring_invoice.approve'),
                               ('connector', 'connector.change'), ('bank_rule', 'bank_rule.auto_apply'),
                               ('expense_claim', 'expense_claim.post'), ('claim_reimbursement', 'claim_reimbursement.void'),
                               ('cash_advance', 'cash_advance.void'), ('asset_disposal', 'asset_disposal.void'),
                               ('fixed_asset', 'fixed_asset.void'),
                               ('payee_bank_change', 'payee_bank_change.approve'),
                               ('claimant_bank_change', 'claimant_bank_change.approve'),
                               ('payment_batch', 'payment_batch.approve'), ('payment_batch', 'payment_batch.reapprove'))) NOT VALID;
-- 000197 validates it, in a transaction of its own (squawk: constraint-missing-not-valid).

-- +goose StatementBegin
-- Accepting an invitation (000029), and now also by a removed member: their membership is active once more, with the
-- invitation's roles.
CREATE OR REPLACE FUNCTION fyniq.accept_member_invitation(p_hash bytea, p_display_name text, p_issuer text, p_subject text, p_now timestamptz,
  OUT organisation_id uuid, OUT invitation_id uuid, OUT user_id uuid, OUT new_user boolean, OUT membership_id uuid)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  org uuid;
  inv fyniq.member_invitation;
  r   text;
BEGIN
  FOR org IN SELECT t.organisation_id FROM fyniq.tenant_directory t ORDER BY t.created_at, t.organisation_id LOOP
    PERFORM set_config('app.org_id', org::text, true);
    SELECT * INTO inv FROM fyniq.member_invitation i
     WHERE i.organisation_id = org AND i.code_hash = p_hash AND i.accepted_at IS NULL AND i.withdrawn_at IS NULL AND i.expires_at > p_now
       FOR UPDATE;
    EXIT WHEN FOUND;
  END LOOP;
  IF inv.id IS NULL THEN
    PERFORM set_config('app.org_id', '', true);
    RAISE EXCEPTION 'The invitation is not valid, used or expired' USING ERRCODE = 'FY092';
  END IF;
  accept_member_invitation.organisation_id := inv.organisation_id;
  accept_member_invitation.invitation_id := inv.id;
  accept_member_invitation.user_id := fyniq.user_for_identity(p_issuer, p_subject);
  accept_member_invitation.new_user := accept_member_invitation.user_id IS NULL;
  IF accept_member_invitation.new_user THEN
    INSERT INTO fyniq.app_user (display_name) VALUES (p_display_name) RETURNING app_user.user_id INTO accept_member_invitation.user_id;
    INSERT INTO fyniq.user_identity (issuer, subject, user_id) VALUES (p_issuer, p_subject, accept_member_invitation.user_id);
  ELSIF NOT EXISTS (SELECT 1 FROM fyniq.app_user u WHERE u.user_id = accept_member_invitation.user_id AND u.status = 'active')
     OR EXISTS (SELECT 1 FROM fyniq.user_membership m
                 WHERE m.organisation_id = inv.organisation_id AND m.user_id = accept_member_invitation.user_id AND m.status = 'active') THEN
    RAISE EXCEPTION 'The invitation is not valid, used or expired' USING ERRCODE = 'FY092';
  END IF;
  -- The members' lock: a role change counting the members waits for this join, and the other way round.
  PERFORM pg_advisory_xact_lock(hashtextextended('fyniq.members:' || inv.organisation_id::text, 0));
  UPDATE fyniq.user_membership m SET status = 'active', version = m.version + 1
   WHERE m.organisation_id = inv.organisation_id AND m.user_id = accept_member_invitation.user_id AND m.status = 'removed'
  RETURNING m.id INTO accept_member_invitation.membership_id;
  IF NOT FOUND THEN
    INSERT INTO fyniq.user_membership (user_id) VALUES (accept_member_invitation.user_id)
    RETURNING id INTO accept_member_invitation.membership_id;
  END IF;
  FOREACH r IN ARRAY inv.roles LOOP
    INSERT INTO fyniq.role_assignment (membership_id, role, expires_at)
    VALUES (accept_member_invitation.membership_id, r, inv.roles_expire_at);
  END LOOP;
  UPDATE fyniq.member_invitation i SET accepted_at = p_now, accepted_by = accept_member_invitation.user_id
   WHERE i.organisation_id = inv.organisation_id AND i.id = inv.id;
END
$$;
-- +goose StatementEnd
