-- Payment links on invoices, Model A (PRD-001 FR-AR-13, FR-PSP-02, rev 0.510; p2-bank-feeds-payments tasks 1.2 and 1.3).
--
-- A payment link is a request made at the payment provider (HitPay) for an issued invoice's open balance. It is asked for by a
-- person with a right on Invoices (the API), once live per invoice; the provider tells Fyniq nothing but which charge was made
-- under it, and the worker's sweep, which already reads the provider's charges, records that charge as a payment of the link
-- (payment_link_payment). Auto-reconcile then takes the charge's statement line to the invoice the link names, once. The worker
-- posts nothing and holds no right to change a link.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.payment_link (
  organisation_id     uuid          NOT NULL DEFAULT fyniq.current_org(),
  id                  uuid          NOT NULL DEFAULT uuidv7(),
  connector_id        uuid          NOT NULL,
  invoice_id          uuid          NOT NULL,
  provider_request_id text          NOT NULL,  -- the provider's payment request, which its charges name
  url                 text          NOT NULL,
  reference           text          NOT NULL,  -- the invoice's number, as sent to the provider
  currency            text          NOT NULL,
  amount              numeric(20,4) NOT NULL,  -- the invoice's open balance when the link was made
  status              text          NOT NULL DEFAULT 'live',
  closed_reason       text,
  expires_at          timestamptz,
  version             integer       NOT NULL DEFAULT 1,
  created_by          text          NOT NULL,
  created_at          timestamptz   NOT NULL,
  closed_at           timestamptz,
  CONSTRAINT payment_link_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT payment_link_connector_fkey FOREIGN KEY (organisation_id, connector_id) REFERENCES fyniq.organisation_connector (organisation_id, id),
  CONSTRAINT payment_link_invoice_fkey FOREIGN KEY (organisation_id, invoice_id) REFERENCES fyniq.document (organisation_id, id),
  CONSTRAINT payment_link_request_once UNIQUE (organisation_id, connector_id, provider_request_id),
  CONSTRAINT payment_link_status_known CHECK (status IN ('live', 'cancelled', 'paid', 'expired')),
  CONSTRAINT payment_link_closed_form CHECK ((status = 'live') = (closed_at IS NULL)),
  CONSTRAINT payment_link_currency_form CHECK (currency ~ '^[A-Z]{3}$'),
  CONSTRAINT payment_link_amount CHECK (amount <> 'NaN' AND amount > 0),
  CONSTRAINT payment_link_lengths CHECK (length(provider_request_id) BETWEEN 1 AND 255 AND length(url) BETWEEN 1 AND 2048
    AND length(reference) BETWEEN 1 AND 255 AND (closed_reason IS NULL OR length(closed_reason) BETWEEN 1 AND 500)),
  CONSTRAINT payment_link_url_https CHECK (url ~ '^https://[^[:space:]]+$'),
  CONSTRAINT payment_link_by_form CHECK (created_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);
-- One live link per invoice (FR-AR-13): a second is refused by the database.
CREATE UNIQUE INDEX payment_link_one_live ON fyniq.payment_link (organisation_id, invoice_id) WHERE status = 'live';
CREATE INDEX payment_link_invoice ON fyniq.payment_link (organisation_id, invoice_id, created_at);

-- A payment under a link: a charge the provider made under its payment request, as the sweep read it. Once per provider charge.
CREATE TABLE fyniq.payment_link_payment (
  organisation_id     uuid          NOT NULL DEFAULT fyniq.current_org(),
  id                  uuid          NOT NULL DEFAULT uuidv7(),
  link_id             uuid          NOT NULL,
  connector_id        uuid          NOT NULL,
  provider_payment_id text          NOT NULL,  -- the charge's ID: the reference of its statement line
  currency            text          NOT NULL,
  amount              numeric(20,4) NOT NULL,  -- gross, as the provider reports it
  paid_at             timestamptz   NOT NULL,
  seen_at             timestamptz   NOT NULL,
  CONSTRAINT payment_link_payment_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT payment_link_payment_link_fkey FOREIGN KEY (organisation_id, link_id) REFERENCES fyniq.payment_link (organisation_id, id),
  CONSTRAINT payment_link_payment_once UNIQUE (organisation_id, connector_id, provider_payment_id),
  CONSTRAINT payment_link_payment_currency_form CHECK (currency ~ '^[A-Z]{3}$'),
  CONSTRAINT payment_link_payment_amount CHECK (amount <> 'NaN' AND amount > 0),
  CONSTRAINT payment_link_payment_lengths CHECK (length(provider_payment_id) BETWEEN 1 AND 255)
);
CREATE INDEX payment_link_payment_link ON fyniq.payment_link_payment (organisation_id, link_id);

-- A link changes only by closing: live to paid, expired or cancelled, with its reason, once (version moves with it); nothing else
-- of it is ever changed, and no link or payment is deleted.
-- +goose StatementBegin
CREATE FUNCTION fyniq.payment_link_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A payment link is never deleted' USING ERRCODE = 'FYP01', HINT = 'Cancel it instead (FR-AR-13).';
  END IF;
  IF OLD.status <> 'live'
     OR (NEW.organisation_id, NEW.id, NEW.connector_id, NEW.invoice_id, NEW.provider_request_id, NEW.url, NEW.reference, NEW.currency, NEW.amount, NEW.expires_at, NEW.created_by, NEW.created_at)
        IS DISTINCT FROM (OLD.organisation_id, OLD.id, OLD.connector_id, OLD.invoice_id, OLD.provider_request_id, OLD.url, OLD.reference, OLD.currency, OLD.amount, OLD.expires_at, OLD.created_by, OLD.created_at) THEN
    RAISE EXCEPTION 'A payment link only closes, once' USING ERRCODE = 'FYP01', HINT = 'Ask for a new link on the invoice (FR-AR-13).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER payment_link_guard BEFORE UPDATE OR DELETE ON fyniq.payment_link FOR EACH ROW EXECUTE FUNCTION fyniq.payment_link_guard();
CREATE TRIGGER payment_link_no_truncate BEFORE TRUNCATE ON fyniq.payment_link FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();

-- +goose StatementBegin
CREATE FUNCTION fyniq.payment_link_payment_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'A recorded payment under a link is never changed' USING ERRCODE = 'FYP02',
    HINT = 'The receipt it led to is voided through its own steps (FR-PSP-02).';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER payment_link_payment_guard BEFORE UPDATE OR DELETE ON fyniq.payment_link_payment FOR EACH ROW EXECUTE FUNCTION fyniq.payment_link_payment_guard();
CREATE TRIGGER payment_link_payment_no_truncate BEFORE TRUNCATE ON fyniq.payment_link_payment FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();

ALTER TABLE fyniq.payment_link ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.payment_link FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.payment_link USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.payment_link_payment ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.payment_link_payment FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.payment_link_payment USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.payment_link
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('payment_link', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.payment_link_payment
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('payment_link_payment', 'id');

GRANT SELECT ON fyniq.payment_link, fyniq.payment_link_payment TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (connector_id, invoice_id, provider_request_id, url, reference, currency, amount, expires_at, created_by, created_at) ON fyniq.payment_link TO fyniq_api;
GRANT UPDATE (status, closed_reason, closed_at, version) ON fyniq.payment_link TO fyniq_api;
GRANT INSERT (link_id, connector_id, provider_payment_id, currency, amount, paid_at, seen_at) ON fyniq.payment_link_payment TO fyniq_worker;
