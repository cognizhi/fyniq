-- Contacts (PRD-001 FR-CT-01, FR-CT-04; p1-organisation-chart task 4.1).
--
-- A contact is a customer, a vendor or both, with its legal name, UEN or tax ID, Peppol ID, billing and
-- remittance emails, phone, address, country, currency, payment terms, default category (an account) and
-- default GST code. It is mutable at its version, audited (require_audit), and never deleted: an inactive
-- contact is kept for its history. Vendor bank details, high-risk data under dual control (FR-CT-03), are a
-- separate table with task 4.2; a contact's balances (FR-CT-02) come with the sales and purchase documents
-- that post to it.
--
-- contact_legal_name_idx serves the list by name and duplicate detection on the name (FR-CT-04).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.contact (
  organisation_id    uuid        NOT NULL DEFAULT fyniq.current_org(),
  id                 uuid        NOT NULL DEFAULT uuidv7(),
  legal_name         text        NOT NULL,
  is_customer        boolean     NOT NULL DEFAULT false,
  is_vendor          boolean     NOT NULL DEFAULT false,
  uen_or_tax_id      text,
  peppol_id          text,
  billing_email      text,
  remittance_email   text,
  phone              text,
  address            text,
  country            text,
  currency           text,
  payment_terms_days smallint,
  default_account_id uuid,
  default_gst_code   text,
  is_active          boolean     NOT NULL DEFAULT true,
  version            integer     NOT NULL DEFAULT 1,
  created_at         timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT contact_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT contact_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT contact_default_account_fkey FOREIGN KEY (organisation_id, default_account_id)
    REFERENCES fyniq.account (organisation_id, id),
  CONSTRAINT contact_role_known CHECK (is_customer OR is_vendor),
  CONSTRAINT contact_lengths CHECK (length(btrim(legal_name)) BETWEEN 1 AND 200 AND length(uen_or_tax_id) BETWEEN 1 AND 30
    AND length(peppol_id) BETWEEN 1 AND 70 AND length(billing_email) BETWEEN 3 AND 254 AND length(remittance_email) BETWEEN 3 AND 254
    AND length(phone) BETWEEN 1 AND 40 AND length(address) BETWEEN 1 AND 500),
  CONSTRAINT contact_country_format CHECK (country ~ '^[A-Z]{2}$'),
  CONSTRAINT contact_currency_format CHECK (currency ~ '^[A-Z]{3}$'),
  CONSTRAINT contact_terms_range CHECK (payment_terms_days BETWEEN 0 AND 365),
  CONSTRAINT contact_gst_code_format CHECK (default_gst_code ~ '^[A-Z]{2,4}$'),
  CONSTRAINT contact_version_positive CHECK (version > 0)
);
CREATE INDEX contact_legal_name_idx ON fyniq.contact (organisation_id, lower(legal_name));

ALTER TABLE fyniq.contact ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.contact FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.contact
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.contact
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('contact', 'id');

GRANT SELECT ON fyniq.contact TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
GRANT INSERT (legal_name, is_customer, is_vendor, uen_or_tax_id, peppol_id, billing_email, remittance_email, phone, address,
              country, currency, payment_terms_days, default_account_id, default_gst_code),
      UPDATE (legal_name, is_customer, is_vendor, uen_or_tax_id, peppol_id, billing_email, remittance_email, phone, address,
              country, currency, payment_terms_days, default_account_id, default_gst_code, is_active, version)
  ON fyniq.contact TO fyniq_api;
