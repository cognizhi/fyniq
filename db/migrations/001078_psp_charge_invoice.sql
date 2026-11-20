-- Which provider invoice a charge paid (PRD-001 FR-PSP-07; p1-payment-providers task 3.2): the provider's charge names its invoice, the worker records the
-- pair once, and auto-reconcile takes the charge's statement line to the AR document made of that invoice, once, by the existing receipt command. The row is a
-- mirror of what the provider reported: written once, never changed or deleted.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.psp_charge_invoice (
  organisation_id     uuid        NOT NULL DEFAULT fyniq.current_org(),
  id                  uuid        NOT NULL DEFAULT uuidv7(),
  connector_id        uuid        NOT NULL,
  charge_id           text        NOT NULL,
  provider_invoice_id text        NOT NULL,
  seen_at             timestamptz NOT NULL,
  CONSTRAINT psp_charge_invoice_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT psp_charge_invoice_once UNIQUE (organisation_id, connector_id, charge_id),
  CONSTRAINT psp_charge_invoice_connector_fkey FOREIGN KEY (organisation_id, connector_id) REFERENCES fyniq.organisation_connector (organisation_id, id),
  CONSTRAINT psp_charge_invoice_lengths CHECK (length(charge_id) BETWEEN 1 AND 255 AND length(provider_invoice_id) BETWEEN 1 AND 255)
);
-- +goose StatementBegin
CREATE FUNCTION fyniq.psp_charge_invoice_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'The invoice a charge paid is never changed or deleted' USING ERRCODE = 'FYP32', HINT = 'It is what the provider reported (FR-PSP-07).';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER psp_charge_invoice_guard BEFORE UPDATE OR DELETE ON fyniq.psp_charge_invoice FOR EACH ROW EXECUTE FUNCTION fyniq.psp_charge_invoice_guard();
CREATE TRIGGER psp_charge_invoice_no_truncate BEFORE TRUNCATE ON fyniq.psp_charge_invoice FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();
ALTER TABLE fyniq.psp_charge_invoice ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.psp_charge_invoice FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.psp_charge_invoice USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.psp_charge_invoice
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('psp_charge_invoice', 'id');
GRANT SELECT ON fyniq.psp_charge_invoice TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (connector_id, charge_id, provider_invoice_id, seen_at) ON fyniq.psp_charge_invoice TO fyniq_worker;
