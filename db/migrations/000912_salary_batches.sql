-- Salary payment batches (PRD-001 FR-PAY-11, FR-PPL-12, 13, 14, 17; PRD-002 Appendix E payroll.payments.ready; p1-payroll-link task 6.1, 6.2, 4.2, design D9).
--
-- A salary batch is the payment lines Nemura sends for a pay run that Fyniq has posted: one line per person, with the person's bank-detail record, version and digest.
-- It is a sibling of the payment batch, not a kind of it, for four reasons (design D9, amended): the payment batch's document payload, its versions, its audit values, its approval diff and
-- its envelope all print each payee and amount, which FR-PPL-17 forbids for salaries; a payment batch is released once, so a part of it could not go while a held person's line waits;
-- its items are bills and claims, which a salary line is not; and a salary line has the states of FR-PPL-12, which a payee does not. What it keeps from the payment batch: the approval
-- engine, the Payment releaser's release under a step-up, the payment files' writers and the separation of duties.
--
--  * salary_batch: the header, one per run version; its lines are sealed in salary_batch_line (the name, bank, account name and number are sealed by the application: SEC-11), carrying
--    only IDs, the amount, the bank-detail record, version and digest in the clear. A revised line is a new version of it (never an update), which makes the approval stale (FR-PPL-14).
--  * salary_line_event: a line's states, append-only: scheduled, released, sent, accepted, paid, rejected, returned, unknown (FR-PPL-12). "Paid" has a source: the bank feed matching the
--    debit, or a person's record with evidence. An exported file or a release is never "paid" (FY912).
--  * salary_release and salary_release_line: what each release wrote (the file sealed, its SHA-256) and which lines went in it. A line goes in one release; a held person's line waits and
--    goes in a later one (FR-PPL-13). A release names an approved batch's approval, and its releaser is neither the run's preparer nor its approver in Nemura (issuer and subject: SOD-4).
-- The database is the backstop: lines, events and releases are never changed or deleted.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.salary_batch (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  seq             integer     NOT NULL,                  -- the batch's number is SB- and this
  link_id         uuid        NOT NULL,
  source_system   text        NOT NULL,
  run_id          uuid        NOT NULL,
  run_version     integer     NOT NULL,
  receipt_id      uuid        NOT NULL,                  -- the posted run's receipt: who prepared and approved it, and what it posted
  wage_month      date        NOT NULL,
  pay_date        date,                                  -- chosen when the batch is prepared in Fyniq
  money_account_id uuid,
  method          text,
  prepared_by     text,                                  -- who prepared it in Fyniq (SOD-2): an actor of the audit trail
  status          text        NOT NULL DEFAULT 'open',
  version         integer     NOT NULL DEFAULT 1,        -- up by one when a line is revised or the batch is prepared again: the approval is of one version
  lines_version   integer     NOT NULL DEFAULT 1,        -- the latest payments.ready object version received
  cancel_reason   text,
  created_at      timestamptz NOT NULL,
  changed_at      timestamptz NOT NULL,
  CONSTRAINT salary_batch_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT salary_batch_seq_key UNIQUE (organisation_id, seq),
  CONSTRAINT salary_batch_run_key UNIQUE (organisation_id, source_system, run_id, run_version),
  CONSTRAINT salary_batch_link_fkey FOREIGN KEY (organisation_id, link_id) REFERENCES fyniq.people_link (organisation_id, id),
  CONSTRAINT salary_batch_receipt_fkey FOREIGN KEY (organisation_id, receipt_id) REFERENCES fyniq.payroll_run_receipt (organisation_id, id),
  CONSTRAINT salary_batch_account_fkey FOREIGN KEY (organisation_id, money_account_id) REFERENCES fyniq.account (organisation_id, id),
  CONSTRAINT salary_batch_status_known CHECK (status IN ('open', 'approved', 'cancelled')),
  CONSTRAINT salary_batch_method_known CHECK (method IS NULL OR method IN ('fast', 'giro')),
  CONSTRAINT salary_batch_positive CHECK (seq >= 1 AND run_version >= 1 AND version >= 1 AND lines_version >= 1),
  CONSTRAINT salary_batch_prepared_whole CHECK ((pay_date IS NULL) = (money_account_id IS NULL) AND (pay_date IS NULL) = (method IS NULL) AND (pay_date IS NULL) = (prepared_by IS NULL)),
  CONSTRAINT salary_batch_prepared_by_form CHECK (prepared_by IS NULL OR prepared_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$'),
  CONSTRAINT salary_batch_approved_prepared CHECK (status <> 'approved' OR pay_date IS NOT NULL),
  CONSTRAINT salary_batch_cancel_reason CHECK ((status = 'cancelled') = (cancel_reason IS NOT NULL) AND (cancel_reason IS NULL OR length(btrim(cancel_reason)) BETWEEN 1 AND 500))
);

CREATE TABLE fyniq.salary_batch_line (
  organisation_id     uuid           NOT NULL DEFAULT fyniq.current_org(),
  id                  uuid           NOT NULL DEFAULT uuidv7(),
  batch_id            uuid           NOT NULL,
  line_ref            uuid           NOT NULL,           -- Nemura's ID of the payment line: stable across its versions
  line_version        integer        NOT NULL,
  person_id           uuid           NOT NULL,           -- the people projection's person
  amount              numeric(20, 4) NOT NULL,
  bank_detail_id      uuid           NOT NULL,
  bank_detail_version integer        NOT NULL,
  bank_detail_digest  text           NOT NULL,
  sealed              bytea          NOT NULL,           -- sealed by the application (SEC-11), bound to the line: the payee's name, bank, account name and number
  event_id            uuid           NOT NULL,           -- the inbox event that brought it
  created_at          timestamptz    NOT NULL,
  CONSTRAINT salary_batch_line_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT salary_batch_line_version_key UNIQUE (organisation_id, batch_id, line_ref, line_version),
  CONSTRAINT salary_batch_line_batch_fkey FOREIGN KEY (organisation_id, batch_id) REFERENCES fyniq.salary_batch (organisation_id, id),
  CONSTRAINT salary_batch_line_amount CHECK (amount <> 'NaN' AND amount = trunc(amount, 2) AND amount > 0),
  CONSTRAINT salary_batch_line_versions CHECK (line_version >= 1 AND bank_detail_version >= 1),
  CONSTRAINT salary_batch_line_digest CHECK (bank_detail_digest ~ '^[0-9a-f]{64}$'),
  CONSTRAINT salary_batch_line_sealed CHECK (octet_length(sealed) BETWEEN 40 AND 4096)
);
CREATE INDEX salary_batch_line_person ON fyniq.salary_batch_line (organisation_id, person_id);

CREATE TABLE fyniq.salary_release (
  organisation_id uuid           NOT NULL DEFAULT fyniq.current_org(),
  id              uuid           NOT NULL DEFAULT uuidv7(),
  batch_id        uuid           NOT NULL,
  batch_version   integer        NOT NULL,
  approval_id     uuid           NOT NULL,
  released_by     uuid           NOT NULL,
  released_at     timestamptz    NOT NULL,
  writer          text           NOT NULL,
  file_name       text           NOT NULL,
  media_type      text           NOT NULL,
  file_sealed     bytea          NOT NULL,
  file_sha256     bytea          NOT NULL,
  lines           integer        NOT NULL,
  total           numeric(20, 4) NOT NULL,
  CONSTRAINT salary_release_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT salary_release_batch_fkey FOREIGN KEY (organisation_id, batch_id) REFERENCES fyniq.salary_batch (organisation_id, id),
  CONSTRAINT salary_release_approval_fkey FOREIGN KEY (organisation_id, approval_id) REFERENCES fyniq.approval_request (organisation_id, id),
  CONSTRAINT salary_release_by_fkey FOREIGN KEY (released_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT salary_release_writer_format CHECK (writer ~ '^[a-z0-9-]+@[0-9]+$'),
  CONSTRAINT salary_release_name_length CHECK (length(file_name) BETWEEN 1 AND 200),
  CONSTRAINT salary_release_media_length CHECK (length(media_type) BETWEEN 3 AND 100),
  CONSTRAINT salary_release_sealed_length CHECK (octet_length(file_sealed) BETWEEN 40 AND 16777216),
  CONSTRAINT salary_release_sha256_length CHECK (octet_length(file_sha256) = 32),
  CONSTRAINT salary_release_lines_positive CHECK (lines > 0),
  CONSTRAINT salary_release_total CHECK (total <> 'NaN' AND total = trunc(total, 2) AND total > 0)
);
CREATE TABLE fyniq.salary_release_line (
  organisation_id uuid NOT NULL DEFAULT fyniq.current_org(),
  id              uuid NOT NULL DEFAULT uuidv7(),
  release_id      uuid NOT NULL,
  batch_id        uuid NOT NULL,
  line_ref        uuid NOT NULL,
  line_id         uuid NOT NULL,   -- the version of the line that went in the file
  instruction_id  text NOT NULL,   -- what the bank's instruction is called: the batch's number, the release and the line (FR-PPL-12)
  CONSTRAINT salary_release_line_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT salary_release_line_once UNIQUE (organisation_id, batch_id, line_ref),
  CONSTRAINT salary_release_line_instruction UNIQUE (organisation_id, instruction_id),
  CONSTRAINT salary_release_line_release_fkey FOREIGN KEY (organisation_id, release_id) REFERENCES fyniq.salary_release (organisation_id, id),
  CONSTRAINT salary_release_line_line_fkey FOREIGN KEY (organisation_id, line_id) REFERENCES fyniq.salary_batch_line (organisation_id, id),
  CONSTRAINT salary_release_line_instruction_shape CHECK (instruction_id ~ '^[A-Z0-9-]{3,60}$')
);

CREATE TABLE fyniq.salary_line_event (
  organisation_id uuid           NOT NULL DEFAULT fyniq.current_org(),
  id              uuid           NOT NULL DEFAULT uuidv7(),
  batch_id        uuid           NOT NULL,
  line_ref        uuid           NOT NULL,
  person_id       uuid           NOT NULL,
  run_id          uuid           NOT NULL,        -- an unknown outcome blocks by run and person, not by version or line (FR-PPL-12)
  state           text           NOT NULL,
  source          text           NOT NULL,
  instruction_id  text,
  amount          numeric(20, 4),
  on_date         date,
  bank_line_id    uuid,
  evidence_id     uuid,
  recorded_by     text           NOT NULL,
  at              timestamptz    NOT NULL,
  CONSTRAINT salary_line_event_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT salary_line_event_batch_fkey FOREIGN KEY (organisation_id, batch_id) REFERENCES fyniq.salary_batch (organisation_id, id),
  CONSTRAINT salary_line_event_state_known CHECK (state IN ('scheduled', 'released', 'sent', 'accepted', 'paid', 'rejected', 'returned', 'unknown')),
  CONSTRAINT salary_line_event_source_known CHECK (source IN ('arrival', 'release', 'bank_feed', 'manual')),
  -- Paid, rejected and returned come from the bank feed or from a person with evidence, never from the file or the release (FR-PPL-12).
  CONSTRAINT salary_line_event_paid_source CHECK (state NOT IN ('paid', 'rejected', 'returned', 'unknown') OR source IN ('bank_feed', 'manual')),
  CONSTRAINT salary_line_event_manual_evidence CHECK (source <> 'manual' OR evidence_id IS NOT NULL),
  CONSTRAINT salary_line_event_bank_feed_line CHECK ((source = 'bank_feed') = (bank_line_id IS NOT NULL)),
  CONSTRAINT salary_line_event_by_form CHECK (recorded_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$'),
  CONSTRAINT salary_line_event_amount CHECK (amount IS NULL OR (amount <> 'NaN' AND amount >= 0))
);
CREATE INDEX salary_line_event_line ON fyniq.salary_line_event (organisation_id, batch_id, line_ref, at);
CREATE INDEX salary_line_event_run_person ON fyniq.salary_line_event (organisation_id, run_id, person_id);

-- ------------------------------------------------------------------ guards
-- Lines, releases and events are written once and never changed, deleted or truncated (FY912).
-- +goose StatementBegin
CREATE FUNCTION fyniq.salary_append_only() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'A salary batch''s lines, events and releases are never changed or deleted' USING ERRCODE = 'FY912',
    HINT = 'A revised line is a new version of it; a payment that came back is a new event (FR-PPL-12, FR-PPL-14).';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER salary_batch_line_guard BEFORE UPDATE OR DELETE ON fyniq.salary_batch_line FOR EACH ROW EXECUTE FUNCTION fyniq.salary_append_only();
CREATE TRIGGER salary_release_guard BEFORE UPDATE OR DELETE ON fyniq.salary_release FOR EACH ROW EXECUTE FUNCTION fyniq.salary_append_only();
CREATE TRIGGER salary_release_line_guard BEFORE UPDATE OR DELETE ON fyniq.salary_release_line FOR EACH ROW EXECUTE FUNCTION fyniq.salary_append_only();
CREATE TRIGGER salary_line_event_guard BEFORE UPDATE OR DELETE ON fyniq.salary_line_event FOR EACH ROW EXECUTE FUNCTION fyniq.salary_append_only();
CREATE TRIGGER salary_batch_no_delete BEFORE DELETE ON fyniq.salary_batch FOR EACH ROW EXECUTE FUNCTION fyniq.salary_append_only();
CREATE TRIGGER salary_batch_line_no_truncate BEFORE TRUNCATE ON fyniq.salary_batch_line FOR EACH STATEMENT EXECUTE FUNCTION fyniq.salary_append_only();
CREATE TRIGGER salary_release_no_truncate BEFORE TRUNCATE ON fyniq.salary_release FOR EACH STATEMENT EXECUTE FUNCTION fyniq.salary_append_only();
CREATE TRIGGER salary_release_line_no_truncate BEFORE TRUNCATE ON fyniq.salary_release_line FOR EACH STATEMENT EXECUTE FUNCTION fyniq.salary_append_only();
CREATE TRIGGER salary_line_event_no_truncate BEFORE TRUNCATE ON fyniq.salary_line_event FOR EACH STATEMENT EXECUTE FUNCTION fyniq.salary_append_only();
CREATE TRIGGER salary_batch_no_truncate BEFORE TRUNCATE ON fyniq.salary_batch FOR EACH STATEMENT EXECUTE FUNCTION fyniq.salary_append_only();

-- A batch moves forward only: open to approved (under an approval, below) or cancelled; approved to cancelled while nothing is released; its run, receipt and lines never change (FY912).
-- +goose StatementBegin
CREATE FUNCTION fyniq.salary_batch_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF (NEW.id, NEW.seq, NEW.link_id, NEW.source_system, NEW.run_id, NEW.run_version, NEW.receipt_id, NEW.wage_month, NEW.created_at)
     IS DISTINCT FROM (OLD.id, OLD.seq, OLD.link_id, OLD.source_system, OLD.run_id, OLD.run_version, OLD.receipt_id, OLD.wage_month, OLD.created_at) THEN
    RAISE EXCEPTION 'A salary batch''s run never changes' USING ERRCODE = 'FY912';
  END IF;
  IF OLD.status = 'cancelled' THEN
    RAISE EXCEPTION 'A cancelled salary batch is not changed' USING ERRCODE = 'FY912';
  END IF;
  IF NEW.version < OLD.version OR NEW.lines_version < OLD.lines_version THEN
    RAISE EXCEPTION 'A salary batch''s version goes up, never down' USING ERRCODE = 'FY912';
  END IF;
  IF NEW.status = 'cancelled' AND EXISTS (SELECT 1 FROM fyniq.salary_release r WHERE r.organisation_id = NEW.organisation_id AND r.batch_id = NEW.id) THEN
    RAISE EXCEPTION 'Salary batch % has lines released, so it is not cancelled', NEW.id USING ERRCODE = 'FY912',
      HINT = 'A payment that goes wrong is recorded on its line (FR-PPL-12).';
  END IF;
  IF NEW.status = 'approved' AND OLD.status <> 'approved' THEN
    -- Approved only under an approved request that neither its requester nor the run's preparer or approver in Nemura signed (SOD-4, FM-1): by issuer and subject, never by a name.
    IF NOT EXISTS (
         SELECT 1 FROM fyniq.approval_request r
          WHERE r.organisation_id = NEW.organisation_id AND r.object_type = 'salary_batch' AND r.operation = 'salary_batch.approve'
            AND r.object_key = NEW.id::text AND r.status = 'approved'
            AND NOT EXISTS (SELECT 1 FROM fyniq.approval_signature s
                             WHERE s.organisation_id = r.organisation_id AND s.request_id = r.id
                               AND (s.signer = r.requested_by OR fyniq.salary_is_run_party(NEW.organisation_id, NEW.receipt_id, s.signer)))) THEN
      RAISE EXCEPTION 'Salary batch % is approved only by a second person who did not prepare or approve the run in Nemura', NEW.id USING ERRCODE = 'FY912',
        HINT = 'Ask for approval: someone who neither asked for it nor prepared or approved the pay run signs it (FR-PPL-10, SOD-4).';
    END IF;
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

-- Whether a Fyniq user is, by issuer and subject, the run's preparer or approver in Nemura (SOD-4). The identity table is global, so this reads it as its owner.
-- +goose StatementBegin
CREATE FUNCTION fyniq.salary_is_run_party(org uuid, receipt uuid, who uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT EXISTS (
    SELECT 1 FROM fyniq.payroll_run_receipt r
      JOIN fyniq.user_identity i ON i.user_id = who
     WHERE r.organisation_id = org AND r.id = receipt
       AND ((i.issuer, i.subject) = (r.preparer_issuer, r.preparer_subject) OR (i.issuer, i.subject) = (r.approver_issuer, r.approver_subject)))
$$;
-- +goose StatementEnd
GRANT EXECUTE ON FUNCTION fyniq.salary_is_run_party(uuid, uuid, uuid) TO fyniq_api, fyniq_worker;
CREATE TRIGGER salary_batch_guard BEFORE UPDATE ON fyniq.salary_batch FOR EACH ROW EXECUTE FUNCTION fyniq.salary_batch_guard();

-- A release is written for an approved batch only, under an approved request at the batch's current version... the approval's own request is of this batch; the releaser is neither the run's
-- preparer nor its approver (SOD-4), and a line is not released twice (the unique key). No break-glass reaches a salary: there is no column for one.
-- +goose StatementBegin
CREATE FUNCTION fyniq.salary_release_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  b fyniq.salary_batch%ROWTYPE;
BEGIN
  SELECT * INTO b FROM fyniq.salary_batch s WHERE s.organisation_id = NEW.organisation_id AND s.id = NEW.batch_id;
  IF b.status IS DISTINCT FROM 'approved' OR b.version <> NEW.batch_version THEN
    RAISE EXCEPTION 'Salary batch % is released only once approved, at its current version', NEW.batch_id USING ERRCODE = 'FY912',
      HINT = 'Ask for the batch''s approval; another person approves it first (FR-PAY-11, FM-1).';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM fyniq.approval_request r WHERE r.organisation_id = NEW.organisation_id AND r.id = NEW.approval_id
                    AND r.object_type = 'salary_batch' AND r.object_key = NEW.batch_id::text AND r.status = 'approved') THEN
    RAISE EXCEPTION 'Salary batch % is released under its own approval', NEW.batch_id USING ERRCODE = 'FY912';
  END IF;
  IF fyniq.salary_is_run_party(NEW.organisation_id, b.receipt_id, NEW.released_by) THEN
    RAISE EXCEPTION 'The person who prepared or approved the pay run in Nemura does not release its salary batch' USING ERRCODE = 'FY912',
      HINT = 'Ask another Payment releaser (FR-PPL-10, SOD-4).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER salary_release_insert_guard BEFORE INSERT ON fyniq.salary_release FOR EACH ROW EXECUTE FUNCTION fyniq.salary_release_guard();

-- A line is added to an open or approved batch only (never a cancelled one), and not once its line was released: a released line is not revised.
-- +goose StatementBegin
CREATE FUNCTION fyniq.salary_line_insert_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF EXISTS (SELECT 1 FROM fyniq.salary_batch s WHERE s.organisation_id = NEW.organisation_id AND s.id = NEW.batch_id AND s.status = 'cancelled') THEN
    RAISE EXCEPTION 'A cancelled salary batch takes no line' USING ERRCODE = 'FY912';
  END IF;
  IF EXISTS (SELECT 1 FROM fyniq.salary_release_line l WHERE l.organisation_id = NEW.organisation_id AND l.batch_id = NEW.batch_id AND l.line_ref = NEW.line_ref) THEN
    RAISE EXCEPTION 'A line that was released is not revised' USING ERRCODE = 'FY912', HINT = 'Record what became of its payment on the line (FR-PPL-12).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER salary_batch_line_insert_guard BEFORE INSERT ON fyniq.salary_batch_line FOR EACH ROW EXECUTE FUNCTION fyniq.salary_line_insert_guard();

-- ------------------------------------------------------------------ audit, row-level security, grants
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.salary_batch
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('salary_batch', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.salary_batch_line
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('salary_batch', 'batch_id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.salary_release
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('salary_batch', 'batch_id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.salary_line_event
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('salary_batch', 'batch_id');

ALTER TABLE fyniq.salary_batch ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.salary_batch FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.salary_batch USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.salary_batch_line ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.salary_batch_line FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.salary_batch_line USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.salary_release ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.salary_release FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.salary_release USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.salary_release_line ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.salary_release_line FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.salary_release_line USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.salary_line_event ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.salary_line_event FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.salary_line_event USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

REVOKE ALL ON fyniq.salary_batch, fyniq.salary_batch_line, fyniq.salary_release, fyniq.salary_release_line, fyniq.salary_line_event FROM PUBLIC;
GRANT SELECT ON fyniq.salary_batch, fyniq.salary_batch_line, fyniq.salary_release, fyniq.salary_release_line, fyniq.salary_line_event TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (seq, link_id, source_system, run_id, run_version, receipt_id, wage_month, lines_version, created_at, changed_at) ON fyniq.salary_batch TO fyniq_api;
GRANT UPDATE (pay_date, money_account_id, method, prepared_by, status, version, lines_version, cancel_reason, changed_at) ON fyniq.salary_batch TO fyniq_api;
GRANT INSERT (batch_id, line_ref, line_version, person_id, amount, bank_detail_id, bank_detail_version, bank_detail_digest, sealed, event_id, created_at) ON fyniq.salary_batch_line TO fyniq_api;
GRANT INSERT (batch_id, batch_version, approval_id, released_by, released_at, writer, file_name, media_type, file_sealed, file_sha256, lines, total) ON fyniq.salary_release TO fyniq_api;
GRANT INSERT (release_id, batch_id, line_ref, line_id, instruction_id) ON fyniq.salary_release_line TO fyniq_api;
GRANT INSERT (batch_id, line_ref, person_id, run_id, state, source, instruction_id, amount, on_date, bank_line_id, evidence_id, recorded_by, at) ON fyniq.salary_line_event TO fyniq_api;

-- ------------------------------------------------------------------ the approval of a salary batch (FM-1, SOD-4)
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
                               ('payment_batch', 'payment_batch.approve'), ('payment_batch', 'payment_batch.reapprove'),
                               ('contact_member_link', 'contact_member_link.change'),
                               ('director_fee', 'director_fee.post'),
                               ('director_settlement', 'director_settlement.approve'),
                               ('director_repayment', 'director_repayment.void'),
                               ('account', 'account.merge'),
                               ('bank_reconciliation', 'bank_reconciliation.sign'),
                               ('holder_record', 'holder_record.record'), ('share_class', 'share_class.record'),
                               ('equity_transaction', 'equity_transaction.approve'),
                               ('instrument', 'instrument.record'),
                               ('instrument_template', 'instrument_template.enable'),
                               ('option_pool', 'option_pool.record'), ('option_grant', 'option_grant.record'),
                               ('option_event', 'option_event.record'), ('round_scenario', 'round_scenario.approve'),
                               ('investor_right', 'investor_right.record'),
                               ('controller_entry', 'controller_entry.record'),
                               ('budget', 'budget.record'),
                               ('migration_commit', 'migration.commit'),
                               ('tenant_export', 'tenant_export.prepare'),
                               ('ai_connection', 'ai_connection.create'), ('ai_connection', 'ai_connection.change'),
                               ('ai_connection', 'ai_connection.credential'), ('ai_connection', 'ai_connection.enable'),
                               ('ai_data_policy', 'ai_data_policy.change'),
                               ('ai_connection', 'ai_connection.allow_model'),
                               ('ai_connection', 'ai_connection.price'),
                               ('ai_route', 'ai_route.change'),
                               ('ai_proposal', 'ai_proposal.execute'),
                               ('corporate_detail', 'corporate_detail.record'),
                               ('corporate_officer', 'corporate_officer.record'),
                               ('nominee_entry', 'nominee_entry.record'),
                               ('statements_approval', 'statements_approval.record'),
                               ('corporate_document', 'corporate_document.record'),
                               ('api_client', 'api_client.create'), ('api_client', 'api_client.reclass'),
                               ('kb_publication', 'kb_publication.publish'),
                               ('people_link', 'people_link.link'), ('people_link', 'people_link.unlink'),
                               ('salary_batch', 'salary_batch.approve'))) NOT VALID;

ALTER TABLE fyniq.approval_policy DROP CONSTRAINT approval_policy_object_known;
ALTER TABLE fyniq.approval_policy ADD CONSTRAINT approval_policy_object_known CHECK (
  object_type IN ('organisation_setting', 'membership', 'member_invitation', 'statutory_pack', 'team_mode', 'period', 'journal',
                  'opening_balance', 'bill', 'bill_payment', 'credit_application', 'supplier_refund', 'purchase_order',
                  'document_template', 'invoice', 'customer_receipt', 'write_off', 'customer_credit_application',
                  'customer_refund', 'customer_deposit', 'recurring_invoice', 'connector', 'bank_rule', 'expense_claim',
                  'claim_reimbursement', 'cash_advance', 'asset_disposal', 'fixed_asset',
                  'payee_bank_change', 'claimant_bank_change', 'payment_batch', 'contact_member_link',
                  'director_fee', 'director_settlement', 'director_repayment', 'account', 'bank_reconciliation',
                  'holder_record', 'share_class', 'equity_transaction', 'instrument', 'instrument_template',
                  'option_pool', 'option_grant', 'option_event', 'round_scenario', 'investor_right', 'controller_entry', 'budget',
                  'migration_commit', 'tenant_export', 'ai_connection', 'ai_data_policy', 'ai_route', 'ai_proposal',
                  'corporate_detail', 'corporate_officer', 'nominee_entry', 'statements_approval',
                  'corporate_document', 'api_client', 'kb_publication', 'people_link', 'salary_batch')) NOT VALID;
