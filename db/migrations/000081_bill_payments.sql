-- Payments of bills recorded by hand, and allocations (PRD-001 FR-AP-02, FR-AR-18, ENG-41; the owner's decision of
-- 22 Oct 2026; p1-purchases task 3.1).
--
-- Until bank import exists, a payment the company made is recorded by hand as a document of its own, bill_payment,
-- numbered from its own sequence (PAY-, gap-free) and posted by the rule bill_payment@1. What it settles is recorded as
-- allocations (FR-AR-18): one per bill, with the amount settled in the bill's currency, that amount at the bill's stored
-- rate in the base currency, and the realised FX it made (ENG-41). An allocation never changes or goes (FY170): undoing
-- one is a reversing allocation, which negates it exactly, once (FY171). The amounts are at their currencies' scales,
-- as a journal line's are.
--
-- RND-FX of the parts of a bill paid in parts need not sum to RND-FX of the whole: the part that settles what is left
-- of a bill in another currency clears the residual, a base-only line on accounts payable (Appendix B, "Settlement
-- difference"; INV-05). journal_line_base_only now accepts such a line from a bill_payment entry, on the account with
-- the accounts-payable role, of at most 1.00 in the base currency (the base tolerance of open question 32; a cent a
-- part is all RND-FX leaves), and the revaluation's lines as before (FY008).
--
-- Voiding a payment is FM-1's "voiding a posted document": in Founder mode it waits for a second person, so the approval
-- engine covers the object type bill_payment with its operation bill_payment.void. The widened checks are added NOT
-- VALID and validated by 000082 (squawk: constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.document DROP CONSTRAINT document_type_known;
ALTER TABLE fyniq.document ADD CONSTRAINT document_type_known
  CHECK (type IN ('journal', 'opening_balance', 'fx_revaluation', 'bill', 'bill_payment')) NOT VALID;
ALTER TABLE fyniq.document_sequence DROP CONSTRAINT document_sequence_type_known;
ALTER TABLE fyniq.document_sequence ADD CONSTRAINT document_sequence_type_known
  CHECK (document_type IN ('journal', 'bill', 'bill_payment')) NOT VALID;

ALTER TABLE fyniq.approval_request DROP CONSTRAINT approval_request_object_known;
ALTER TABLE fyniq.approval_request ADD CONSTRAINT approval_request_object_known CHECK (
  (object_type, operation) IN (('organisation_setting', 'organisation_setting.change'), ('membership', 'role.assign'),
                               ('membership', 'role.revoke'), ('member_invitation', 'member_invitation.create'),
                               ('approval_policy', 'approval_policy.change'), ('statutory_pack', 'statutory_pack.activate'),
                               ('team_mode', 'team_mode.change'), ('period', 'period.reopen'),
                               ('journal', 'journal.post'), ('journal', 'journal.reverse'), ('journal', 'journal.edit'),
                               ('opening_balance', 'opening_balance.edit'),
                               ('bill', 'bill.post'), ('bill', 'bill.void'), ('bill', 'bill.edit'),
                               ('bill_payment', 'bill_payment.void'))) NOT VALID;
ALTER TABLE fyniq.approval_policy DROP CONSTRAINT approval_policy_object_known;
ALTER TABLE fyniq.approval_policy ADD CONSTRAINT approval_policy_object_known CHECK (
  object_type IN ('organisation_setting', 'membership', 'member_invitation', 'statutory_pack', 'team_mode', 'period', 'journal',
                  'opening_balance', 'bill', 'bill_payment')) NOT VALID;

CREATE TABLE fyniq.allocation (
  organisation_id  uuid           NOT NULL DEFAULT fyniq.current_org(),
  id               uuid           NOT NULL DEFAULT uuidv7(),
  source_id        uuid           NOT NULL,  -- the document that settles: a bill payment
  source_version   integer        NOT NULL,
  document_id      uuid           NOT NULL,  -- the document it settles: a bill
  on_date          date           NOT NULL,  -- the settlement's date
  currency         text           NOT NULL,  -- the settled document's currency
  minor_units      smallint       NOT NULL,
  amount           numeric(20, 4) NOT NULL,  -- settled, in the document's currency; a reversing allocation's is negative
  base_currency    text           NOT NULL,
  base_minor_units smallint       NOT NULL,
  base_amount      numeric(20, 4) NOT NULL,  -- the amount at the document's stored rate (RND-FX)
  realised_fx      numeric(20, 4) NOT NULL,  -- the realised gain (+) or loss (−) it made, in the base currency (ENG-41)
  reverses_id      uuid,                     -- a reversing allocation names the one it undoes
  written_by       text           NOT NULL,  -- "type:id", as the audit trail names the actor
  written_at       timestamptz    NOT NULL,
  CONSTRAINT allocation_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT allocation_source_fkey FOREIGN KEY (organisation_id, source_id) REFERENCES fyniq.document (organisation_id, id),
  CONSTRAINT allocation_document_fkey FOREIGN KEY (organisation_id, document_id) REFERENCES fyniq.document (organisation_id, id),
  CONSTRAINT allocation_reverses_fkey FOREIGN KEY (organisation_id, reverses_id) REFERENCES fyniq.allocation (organisation_id, id),
  CONSTRAINT allocation_reversed_once UNIQUE (organisation_id, reverses_id),
  CONSTRAINT allocation_currency_fkey FOREIGN KEY (currency, minor_units) REFERENCES fyniq.currency (code, minor_units),
  CONSTRAINT allocation_base_currency_fkey FOREIGN KEY (base_currency, base_minor_units) REFERENCES fyniq.currency (code, minor_units),
  CONSTRAINT allocation_amount_scale CHECK (amount <> 'NaN' AND amount <> 0 AND amount = trunc(amount, minor_units)),
  CONSTRAINT allocation_base_scale CHECK (base_amount <> 'NaN' AND base_amount = trunc(base_amount, base_minor_units)
    AND realised_fx <> 'NaN' AND realised_fx = trunc(realised_fx, base_minor_units)),
  CONSTRAINT allocation_sign CHECK ((reverses_id IS NULL) = (amount > 0)),
  CONSTRAINT allocation_version_positive CHECK (source_version > 0),
  CONSTRAINT allocation_actor_format CHECK (written_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);
CREATE INDEX allocation_document ON fyniq.allocation (organisation_id, document_id, on_date);
CREATE INDEX allocation_source ON fyniq.allocation (organisation_id, source_id);

-- An allocation settles a bill from a bill payment; it never changes or goes; a reversing allocation negates the one it
-- names exactly: the same source, document, date-or-later and amounts, negated.
-- +goose StatementBegin
CREATE FUNCTION fyniq.allocation_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  src text;
  dst text;
  orig fyniq.allocation;
BEGIN
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'An allocation never changes or goes' USING ERRCODE = 'FY170',
      HINT = 'Undo it with a reversing allocation (FR-AR-18).';
  END IF;
  SELECT type INTO src FROM fyniq.document WHERE organisation_id = NEW.organisation_id AND id = NEW.source_id;
  SELECT type INTO dst FROM fyniq.document WHERE organisation_id = NEW.organisation_id AND id = NEW.document_id;
  IF src IS DISTINCT FROM 'bill_payment' OR dst IS DISTINCT FROM 'bill' THEN
    RAISE EXCEPTION 'An allocation settles a bill from a bill payment, not a % from a %', dst, src USING ERRCODE = 'FY171';
  END IF;
  IF NEW.reverses_id IS NOT NULL THEN
    SELECT * INTO orig FROM fyniq.allocation WHERE organisation_id = NEW.organisation_id AND id = NEW.reverses_id;
    IF orig.reverses_id IS NOT NULL OR orig.source_id <> NEW.source_id OR orig.document_id <> NEW.document_id
       OR NEW.on_date < orig.on_date OR NEW.amount <> -orig.amount OR NEW.base_amount <> -orig.base_amount
       OR NEW.realised_fx <> -orig.realised_fx OR NEW.currency <> orig.currency THEN
      RAISE EXCEPTION 'A reversing allocation negates allocation % exactly', NEW.reverses_id USING ERRCODE = 'FY171',
        HINT = 'Reverse an allocation through the application, which negates every amount (FR-AR-18).';
    END IF;
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER allocation_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.allocation
  FOR EACH ROW EXECUTE FUNCTION fyniq.allocation_guard();
CREATE TRIGGER allocation_no_truncate BEFORE TRUNCATE ON fyniq.allocation
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.journal_line_base_only() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  source text;
  stray  integer;
  role   text;
BEGIN
  SELECT source_type INTO source FROM fyniq.journal_entry WHERE organisation_id = NEW.organisation_id AND id = NEW.entry_id;
  IF source = 'bill_payment' THEN
    SELECT a.role INTO role FROM fyniq.account a WHERE a.organisation_id = NEW.organisation_id AND a.id = NEW.account_id;
    IF role IS DISTINCT FROM 'accounts-payable' OR abs(NEW.base_amount) > 1 THEN
      RAISE EXCEPTION 'A bill payment changes a base value alone only to clear the residual RND-FX left of a bill on accounts payable, at most 1.00 %', NEW.base_currency
        USING ERRCODE = 'FY008', HINT = 'Post the line with its amount; a larger difference is a write-off (INV-05, FR-BNK-08).';
    END IF;
    RETURN NULL;
  END IF;
  IF source IS DISTINCT FROM 'fx_revaluation' THEN
    RAISE EXCEPTION 'A line with a base amount and no % amount comes only from FX revaluation, not from %', NEW.currency, source
      USING ERRCODE = 'FY008', HINT = 'Post the line with its amount; only the month-end revaluation changes a base value alone (INV-05, FR-GL-04).';
  END IF;
  SELECT count(*) INTO stray
    FROM fyniq.journal_line l
    JOIN fyniq.account a ON a.organisation_id = l.organisation_id AND a.id = l.account_id
   WHERE l.organisation_id = NEW.organisation_id AND l.entry_id = NEW.entry_id
     AND NOT ((l.amount = 0 AND l.base_amount <> 0 AND l.currency <> l.base_currency AND a.revalue)
           OR (l.currency = l.base_currency AND a.role IS NOT DISTINCT FROM 'unrealised-fx'));
  IF stray > 0 THEN
    RAISE EXCEPTION 'A revaluation revalues only accounts marked for it, against the unrealised-FX account: % line(s) of entry % are neither', stray, NEW.entry_id
      USING ERRCODE = 'FY008', HINT = 'Mark the account for revaluation in the chart, or leave its balance out (INV-05, FR-GL-04).';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd

ALTER TABLE fyniq.allocation ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.allocation FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.allocation
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.allocation
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('allocation', 'id');

GRANT SELECT ON fyniq.allocation TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
GRANT INSERT (source_id, source_version, document_id, on_date, currency, minor_units, amount, base_currency, base_minor_units,
              base_amount, realised_fx, reverses_id, written_by, written_at)
  ON fyniq.allocation TO fyniq_api;
