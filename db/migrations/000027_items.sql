-- Items (PRD-001 FR-ITM-01; p1-organisation-chart task 4.3).
--
-- An item is a product or service the company sells: its code, name, printed description, type, unit,
-- revenue account and GST code, and a unit price in each currency it is sold in. Unit prices are
-- NUMERIC(20,6), §11.3's family for quantities and unit prices: an invoice line's amount reaches its
-- currency's scale through a named rounding policy (ENG-11). An item is mutable at its version, audited,
-- never deleted; its prices change with it, each audited as a change to the item. The item's quantity and
-- sales for the financial year come with the sales documents (p1-sales).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.item (
  organisation_id    uuid        NOT NULL DEFAULT fyniq.current_org(),
  id                 uuid        NOT NULL DEFAULT uuidv7(),
  code               text        NOT NULL,
  name               text        NOT NULL,
  description        text,
  type               text        NOT NULL,
  unit               text,
  revenue_account_id uuid,
  gst_code           text,
  is_active          boolean     NOT NULL DEFAULT true,
  version            integer     NOT NULL DEFAULT 1,
  created_at         timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT item_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT item_code_key UNIQUE (organisation_id, code),
  CONSTRAINT item_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT item_revenue_account_fkey FOREIGN KEY (organisation_id, revenue_account_id)
    REFERENCES fyniq.account (organisation_id, id),
  CONSTRAINT item_code_format CHECK (code ~ '^[0-9A-Za-z][0-9A-Za-z._/-]{0,29}$'),
  CONSTRAINT item_lengths CHECK (length(btrim(name)) BETWEEN 1 AND 200 AND length(description) BETWEEN 1 AND 2000
    AND length(unit) BETWEEN 1 AND 20),
  CONSTRAINT item_type_known CHECK (type IN ('product', 'service')),
  CONSTRAINT item_gst_code_format CHECK (gst_code ~ '^[A-Z]{2,4}$'),
  CONSTRAINT item_version_positive CHECK (version > 0)
);

CREATE TABLE fyniq.item_price (
  organisation_id uuid          NOT NULL DEFAULT fyniq.current_org(),
  id              uuid          NOT NULL DEFAULT uuidv7(),
  item_id         uuid          NOT NULL,
  currency        text          NOT NULL,
  unit_price      numeric(20,6) NOT NULL,
  CONSTRAINT item_price_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT item_price_currency_key UNIQUE (organisation_id, item_id, currency),
  CONSTRAINT item_price_item_fkey FOREIGN KEY (organisation_id, item_id)
    REFERENCES fyniq.item (organisation_id, id),
  CONSTRAINT item_price_currency_format CHECK (currency ~ '^[A-Z]{3}$'),
  CONSTRAINT item_price_unit_price_valid CHECK (unit_price <> 'NaN'::numeric AND unit_price >= 0)
);

ALTER TABLE fyniq.item ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.item FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.item
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.item_price ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.item_price FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.item_price
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.item
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('item', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.item_price
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('item', 'item_id');

GRANT SELECT ON fyniq.item, fyniq.item_price TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
GRANT INSERT (code, name, description, type, unit, revenue_account_id, gst_code),
      UPDATE (name, description, type, unit, revenue_account_id, gst_code, is_active, version) ON fyniq.item TO fyniq_api;
-- A price is replaced when it changes: the old row goes, the new one comes, both in the item's audited change.
GRANT INSERT (item_id, currency, unit_price), DELETE ON fyniq.item_price TO fyniq_api;
