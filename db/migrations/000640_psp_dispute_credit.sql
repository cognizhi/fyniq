-- A lost dispute settled against a customer credit (PRD-001 FR-PSP-04, rev 0.308; p1-payment-providers task 4.5): the credit note
-- that credits the invoice, and the refund document (rule dispute_settlement@1) that settles its credit against the provider's
-- disputes clearing account, once per dispute. The settlement is an ordinary refund document and entry; this only records which
-- dispute it was for, so the dispute's money held is reduced by it and a second settlement is refused.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.psp_dispute_credit (
  organisation_id uuid          NOT NULL DEFAULT fyniq.current_org(),
  id              uuid          NOT NULL DEFAULT uuidv7(),
  connector_id    uuid          NOT NULL,
  dispute_id      text          NOT NULL,
  credit_note_id  uuid          NOT NULL,  -- the issued credit note whose credit it settles
  refund_id       uuid          NOT NULL,  -- the refund document, dispute_settlement@1
  currency        text          NOT NULL,
  amount          numeric(20,4) NOT NULL,  -- what it settled of the credit, positive
  created_by      text          NOT NULL,
  created_at      timestamptz   NOT NULL,
  CONSTRAINT psp_dispute_credit_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT psp_dispute_credit_connector_fkey FOREIGN KEY (organisation_id, connector_id) REFERENCES fyniq.organisation_connector (organisation_id, id),
  CONSTRAINT psp_dispute_credit_credit_fkey FOREIGN KEY (organisation_id, credit_note_id) REFERENCES fyniq.document (organisation_id, id),
  CONSTRAINT psp_dispute_credit_refund_fkey FOREIGN KEY (organisation_id, refund_id) REFERENCES fyniq.document (organisation_id, id),
  CONSTRAINT psp_dispute_credit_once UNIQUE (organisation_id, connector_id, dispute_id),
  CONSTRAINT psp_dispute_credit_currency_form CHECK (currency ~ '^[A-Z]{3}$'),
  CONSTRAINT psp_dispute_credit_amount CHECK (amount <> 'NaN' AND amount > 0),
  CONSTRAINT psp_dispute_credit_lengths CHECK (length(dispute_id) BETWEEN 1 AND 255),
  CONSTRAINT psp_dispute_credit_by_form CHECK (created_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);

-- +goose StatementBegin
CREATE FUNCTION fyniq.psp_dispute_credit_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'A recorded dispute settlement is never changed' USING ERRCODE = 'FY625',
    HINT = 'The refund document it names is voided through its own steps (FR-PSP-04).';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER psp_dispute_credit_guard BEFORE UPDATE OR DELETE ON fyniq.psp_dispute_credit FOR EACH ROW EXECUTE FUNCTION fyniq.psp_dispute_credit_guard();
CREATE TRIGGER psp_dispute_credit_no_truncate BEFORE TRUNCATE ON fyniq.psp_dispute_credit FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();
ALTER TABLE fyniq.psp_dispute_credit ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.psp_dispute_credit FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.psp_dispute_credit USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.psp_dispute_credit
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('psp_dispute_credit', 'id');
GRANT SELECT ON fyniq.psp_dispute_credit TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (connector_id, dispute_id, credit_note_id, refund_id, currency, amount, created_by, created_at) ON fyniq.psp_dispute_credit TO fyniq_api;
