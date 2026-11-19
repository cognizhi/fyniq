-- A lost dispute (PRD-001 FR-PSP-04; p1-payment-providers, the owner's decision of 19 Nov 2026): the account its loss is
-- expensed to, on the provider account, and the record that a draft journal moving a dispute's money from disputes
-- clearing to that account was prepared, once per dispute. Posting the draft is the journal's own approval (FM-1), never
-- this record's.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.psp_account ADD COLUMN dispute_loss_account_id uuid;
ALTER TABLE fyniq.psp_account ADD CONSTRAINT psp_account_loss_fkey FOREIGN KEY (organisation_id, dispute_loss_account_id)
  REFERENCES fyniq.account (organisation_id, id) NOT VALID;
GRANT INSERT (dispute_loss_account_id), UPDATE (dispute_loss_account_id) ON fyniq.psp_account TO fyniq_api;

CREATE TABLE fyniq.psp_dispute_loss (
  organisation_id uuid          NOT NULL DEFAULT fyniq.current_org(),
  id              uuid          NOT NULL DEFAULT uuidv7(),
  connector_id    uuid          NOT NULL,
  dispute_id      text          NOT NULL,  -- the provider's ID of the dispute
  document_id     uuid          NOT NULL,  -- the draft journal prepared: Dr the loss account · Cr disputes clearing
  currency        text          NOT NULL,
  amount          numeric(20,4) NOT NULL,  -- what it holds in disputes clearing and writes off, positive
  accepted_overstated_revenue boolean NOT NULL DEFAULT false, -- above the limit, expensed knowingly with its reason
  reason          text,
  created_by      text          NOT NULL,
  created_at      timestamptz   NOT NULL,
  CONSTRAINT psp_dispute_loss_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT psp_dispute_loss_connector_fkey FOREIGN KEY (organisation_id, connector_id) REFERENCES fyniq.organisation_connector (organisation_id, id),
  CONSTRAINT psp_dispute_loss_document_fkey FOREIGN KEY (organisation_id, document_id) REFERENCES fyniq.document (organisation_id, id),
  CONSTRAINT psp_dispute_loss_once UNIQUE (organisation_id, connector_id, dispute_id),
  CONSTRAINT psp_dispute_loss_currency_form CHECK (currency ~ '^[A-Z]{3}$'),
  CONSTRAINT psp_dispute_loss_amount CHECK (amount <> 'NaN' AND amount > 0),
  CONSTRAINT psp_dispute_loss_lengths CHECK (length(dispute_id) BETWEEN 1 AND 255 AND length(reason) BETWEEN 1 AND 500),
  CONSTRAINT psp_dispute_loss_by_form CHECK (created_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);

-- +goose StatementBegin
CREATE FUNCTION fyniq.psp_dispute_loss_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'A prepared dispute loss is never changed' USING ERRCODE = 'FY624',
    HINT = 'The draft journal it names is posted, or discarded, by the journal''s own steps (FR-PSP-04).';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER psp_dispute_loss_guard BEFORE UPDATE OR DELETE ON fyniq.psp_dispute_loss FOR EACH ROW EXECUTE FUNCTION fyniq.psp_dispute_loss_guard();
CREATE TRIGGER psp_dispute_loss_no_truncate BEFORE TRUNCATE ON fyniq.psp_dispute_loss FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();
ALTER TABLE fyniq.psp_dispute_loss ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.psp_dispute_loss FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.psp_dispute_loss USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.psp_dispute_loss
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('psp_dispute_loss', 'id');
GRANT SELECT ON fyniq.psp_dispute_loss TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (connector_id, dispute_id, document_id, currency, amount, accepted_overstated_revenue, reason, created_by, created_at)
  ON fyniq.psp_dispute_loss TO fyniq_api;
