-- The Model B mirror (PRD-001 FR-PSP-03, FR-PSP-07; p1-payment-providers tasks 3.1, 3.2, 4.1 to 4.3; the owner's decisions of 19 Nov 2026 and 20 Nov 2026):
-- a provider that bills the customers (Stripe Billing) is mirrored read-only: its customers, subscriptions and invoices, with each
-- invoice's collection state (attempts so far, the next retry, the outcome). A provider invoice becomes an AR document only through
-- create_provider_invoice_draft, a narrow SECURITY DEFINER function modelled on create_scheduled_invoice_draft (000134): it makes the
-- draft of one mirrored invoice, once, from the mirror row alone and the provider account's revenue account, saved by "psp:invoices", and
-- the customer's contact when the provider's customer has none. It is the worker's only way to a document; posting the draft is the API's,
-- as it is for the provider's money lines (rev 0.143).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- The chart account a provider's invoices book revenue to; none: its invoices wait for a person.
ALTER TABLE fyniq.psp_account ADD COLUMN revenue_account_id uuid;
ALTER TABLE fyniq.psp_account ADD CONSTRAINT psp_account_revenue_fkey FOREIGN KEY (organisation_id, revenue_account_id) REFERENCES fyniq.account (organisation_id, id) NOT VALID;
GRANT INSERT (revenue_account_id), UPDATE (revenue_account_id) ON fyniq.psp_account TO fyniq_api;

CREATE TABLE fyniq.psp_customer (
  organisation_id      uuid        NOT NULL DEFAULT fyniq.current_org(),
  id                   uuid        NOT NULL DEFAULT uuidv7(),
  connector_id         uuid        NOT NULL,
  provider_customer_id text        NOT NULL,
  name                 text,
  email                text,
  contact_id           uuid,       -- the customer's contact, made on first sight by create_provider_invoice_draft; never matched by name
  seen_at              timestamptz NOT NULL,
  CONSTRAINT psp_customer_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT psp_customer_provider_once UNIQUE (organisation_id, connector_id, provider_customer_id),
  CONSTRAINT psp_customer_connector_fkey FOREIGN KEY (organisation_id, connector_id) REFERENCES fyniq.organisation_connector (organisation_id, id),
  CONSTRAINT psp_customer_contact_fkey FOREIGN KEY (organisation_id, contact_id) REFERENCES fyniq.contact (organisation_id, id),
  CONSTRAINT psp_customer_lengths CHECK (length(provider_customer_id) BETWEEN 1 AND 255 AND length(name) BETWEEN 1 AND 200 AND length(email) BETWEEN 3 AND 254)
);

CREATE TABLE fyniq.psp_subscription (
  organisation_id          uuid          NOT NULL DEFAULT fyniq.current_org(),
  id                       uuid          NOT NULL DEFAULT uuidv7(),
  connector_id             uuid          NOT NULL,
  provider_subscription_id text          NOT NULL,
  provider_customer_id     text          NOT NULL,
  status                   text          NOT NULL,  -- the provider's: active, past_due, unpaid, canceled, paused, trialing, incomplete…
  currency                 text          NOT NULL,
  amount                   numeric(20,4) NOT NULL,  -- what the subscription bills each interval, before tax
  interval_unit            text          NOT NULL,
  interval_count           integer       NOT NULL,
  period_start             date,
  period_end               date,
  cancel_at_period_end     boolean       NOT NULL DEFAULT false,
  canceled_on              date,
  seen_at                  timestamptz   NOT NULL,
  CONSTRAINT psp_subscription_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT psp_subscription_provider_once UNIQUE (organisation_id, connector_id, provider_subscription_id),
  CONSTRAINT psp_subscription_connector_fkey FOREIGN KEY (organisation_id, connector_id) REFERENCES fyniq.organisation_connector (organisation_id, id),
  CONSTRAINT psp_subscription_currency_form CHECK (currency ~ '^[A-Z]{3}$'),
  CONSTRAINT psp_subscription_amount CHECK (amount <> 'NaN' AND amount >= 0),
  CONSTRAINT psp_subscription_interval CHECK (interval_unit IN ('day', 'week', 'month', 'year') AND interval_count BETWEEN 1 AND 3650),
  CONSTRAINT psp_subscription_lengths CHECK (length(provider_subscription_id) BETWEEN 1 AND 255 AND length(provider_customer_id) BETWEEN 1 AND 255 AND length(status) BETWEEN 1 AND 40)
);
CREATE INDEX psp_subscription_customer ON fyniq.psp_subscription (organisation_id, connector_id, provider_customer_id);

CREATE TABLE fyniq.psp_invoice (
  organisation_id          uuid          NOT NULL DEFAULT fyniq.current_org(),
  id                       uuid          NOT NULL DEFAULT uuidv7(),
  connector_id             uuid          NOT NULL,
  provider_invoice_id      text          NOT NULL,
  provider_customer_id     text          NOT NULL,
  provider_subscription_id text,
  number                   text,
  status                   text          NOT NULL,  -- the provider's: draft, open, paid, uncollectible, void
  currency                 text          NOT NULL,
  total                    numeric(20,4) NOT NULL,  -- as the provider reports it, tax included
  amount_paid              numeric(20,4) NOT NULL DEFAULT 0,
  attempt_count            integer       NOT NULL DEFAULT 0,
  next_attempt_on          date,
  outcome                  text,                     -- paid, uncollectible or void, once the provider says so
  hosted_url               text,
  finalized_on             date,
  due_on                   date,
  draft_lines              jsonb         NOT NULL DEFAULT '[]'::jsonb, -- the invoice's lines as the invoice rules read them, without an account
  state                    text          NOT NULL DEFAULT 'mirrored',
  parked_reason            text,
  document_id              uuid,                     -- the AR document, once made; one provider invoice is one document
  version                  integer       NOT NULL DEFAULT 1,
  seen_at                  timestamptz   NOT NULL,
  CONSTRAINT psp_invoice_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT psp_invoice_provider_once UNIQUE (organisation_id, connector_id, provider_invoice_id),
  CONSTRAINT psp_invoice_document_once UNIQUE (organisation_id, document_id),
  CONSTRAINT psp_invoice_connector_fkey FOREIGN KEY (organisation_id, connector_id) REFERENCES fyniq.organisation_connector (organisation_id, id),
  CONSTRAINT psp_invoice_document_fkey FOREIGN KEY (organisation_id, document_id) REFERENCES fyniq.document (organisation_id, id),
  CONSTRAINT psp_invoice_status_known CHECK (status IN ('draft', 'open', 'paid', 'uncollectible', 'void')),
  CONSTRAINT psp_invoice_state_known CHECK (state IN ('mirrored', 'drafted', 'posted', 'parked')),
  CONSTRAINT psp_invoice_state_document CHECK ((state IN ('drafted', 'posted')) = (document_id IS NOT NULL) OR state = 'parked'),
  CONSTRAINT psp_invoice_parked_reason CHECK ((state = 'parked') = (parked_reason IS NOT NULL)),
  CONSTRAINT psp_invoice_currency_form CHECK (currency ~ '^[A-Z]{3}$'),
  CONSTRAINT psp_invoice_amounts CHECK (total <> 'NaN' AND total >= 0 AND amount_paid <> 'NaN' AND amount_paid >= 0),
  CONSTRAINT psp_invoice_attempts CHECK (attempt_count BETWEEN 0 AND 1000),
  CONSTRAINT psp_invoice_outcome_known CHECK (outcome IS NULL OR outcome IN ('paid', 'uncollectible', 'void')),
  CONSTRAINT psp_invoice_lines_array CHECK (jsonb_typeof(draft_lines) = 'array' AND octet_length(draft_lines::text) <= 262144),
  CONSTRAINT psp_invoice_lengths CHECK (length(provider_invoice_id) BETWEEN 1 AND 255 AND length(provider_customer_id) BETWEEN 1 AND 255
    AND length(number) BETWEEN 1 AND 100 AND length(hosted_url) BETWEEN 1 AND 2048 AND length(parked_reason) BETWEEN 1 AND 500),
  CONSTRAINT psp_invoice_version_positive CHECK (version >= 1)
);
CREATE INDEX psp_invoice_customer ON fyniq.psp_invoice (organisation_id, connector_id, provider_customer_id);
CREATE INDEX psp_invoice_state ON fyniq.psp_invoice (organisation_id, state);

-- A mirror row's identity never changes, an AR document is set once (by the function below or never), and nothing is deleted.
-- +goose StatementBegin
CREATE FUNCTION fyniq.psp_mirror_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A mirrored provider record is never deleted' USING ERRCODE = 'FYP30', HINT = 'It is a mirror of what the provider reported (FR-PSP-03).';
  END IF;
  IF TG_TABLE_NAME = 'psp_invoice' THEN
    IF (NEW.connector_id, NEW.provider_invoice_id, NEW.provider_customer_id, NEW.currency) IS DISTINCT FROM
       (OLD.connector_id, OLD.provider_invoice_id, OLD.provider_customer_id, OLD.currency) THEN
      RAISE EXCEPTION 'A mirrored invoice keeps its provider, ID, customer and currency' USING ERRCODE = 'FYP30';
    END IF;
    IF OLD.document_id IS NOT NULL AND NEW.document_id IS DISTINCT FROM OLD.document_id THEN
      RAISE EXCEPTION 'A mirrored invoice has one AR document, made once' USING ERRCODE = 'FYP30';
    END IF;
    IF current_user = 'fyniq_worker' AND NEW.state IS DISTINCT FROM OLD.state AND NEW.state NOT IN ('mirrored', 'parked') THEN
      RAISE EXCEPTION 'The worker mirrors and parks a provider invoice; the draft is made by create_provider_invoice_draft and posted by the API' USING ERRCODE = 'FYP30';
    END IF;
  ELSIF TG_TABLE_NAME = 'psp_customer' THEN
    IF (NEW.connector_id, NEW.provider_customer_id) IS DISTINCT FROM (OLD.connector_id, OLD.provider_customer_id)
       OR (OLD.contact_id IS NOT NULL AND NEW.contact_id IS DISTINCT FROM OLD.contact_id) THEN
      RAISE EXCEPTION 'A mirrored customer keeps its provider ID and its contact' USING ERRCODE = 'FYP30';
    END IF;
  ELSE
    IF (NEW.connector_id, NEW.provider_subscription_id) IS DISTINCT FROM (OLD.connector_id, OLD.provider_subscription_id) THEN
      RAISE EXCEPTION 'A mirrored subscription keeps its provider and ID' USING ERRCODE = 'FYP30';
    END IF;
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER psp_mirror_guard BEFORE UPDATE OR DELETE ON fyniq.psp_invoice FOR EACH ROW EXECUTE FUNCTION fyniq.psp_mirror_guard();
CREATE TRIGGER psp_mirror_guard BEFORE UPDATE OR DELETE ON fyniq.psp_customer FOR EACH ROW EXECUTE FUNCTION fyniq.psp_mirror_guard();
CREATE TRIGGER psp_mirror_guard BEFORE UPDATE OR DELETE ON fyniq.psp_subscription FOR EACH ROW EXECUTE FUNCTION fyniq.psp_mirror_guard();
CREATE TRIGGER psp_invoice_no_truncate BEFORE TRUNCATE ON fyniq.psp_invoice FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();
CREATE TRIGGER psp_customer_no_truncate BEFORE TRUNCATE ON fyniq.psp_customer FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();
CREATE TRIGGER psp_subscription_no_truncate BEFORE TRUNCATE ON fyniq.psp_subscription FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();

-- The narrow function: the draft invoice of one mirrored provider invoice (an open or paid one, not yet made into a document), from the mirror row
-- alone: its lines (the revenue account of the provider account in the invoice's currency added), the customer's contact (made here, once, when the
-- provider's customer has none), dated on the day the provider finalised it, saved by "psp:invoices". It records the document on the mirror row; the
-- caller records the audit events. Returns the document, the contact and whether the contact was made now.
-- +goose StatementBegin
CREATE FUNCTION fyniq.create_provider_invoice_draft(p_connector uuid, p_provider_invoice text)
RETURNS TABLE (document_id uuid, contact_id uuid, contact_made boolean)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  org     uuid := fyniq.require_org();
  inv     fyniq.psp_invoice;
  cust    fyniq.psp_customer;
  revenue uuid;
  c       uuid;
  made    boolean := false;
  doc     uuid;
  lines   jsonb;
  body    jsonb;
BEGIN
  SELECT * INTO inv FROM fyniq.psp_invoice
   WHERE organisation_id = org AND connector_id = p_connector AND provider_invoice_id = p_provider_invoice FOR UPDATE;
  IF NOT FOUND OR inv.document_id IS NOT NULL OR inv.state <> 'mirrored' OR inv.status NOT IN ('open', 'paid')
     OR inv.finalized_on IS NULL OR jsonb_array_length(inv.draft_lines) = 0 THEN
    RAISE EXCEPTION 'Provider invoice % is not one to make an invoice of', p_provider_invoice
      USING ERRCODE = 'FYP31', HINT = 'Only a finalised or paid mirrored invoice with its lines, not yet made, becomes an AR document (FR-PSP-07).';
  END IF;
  SELECT a.revenue_account_id INTO revenue FROM fyniq.psp_account a
    JOIN fyniq.money_account m ON m.organisation_id = a.organisation_id AND m.id = a.money_account_id
   WHERE a.organisation_id = org AND a.connector_id = p_connector AND a.revenue_account_id IS NOT NULL AND m.currency = inv.currency
   ORDER BY a.created_at LIMIT 1;
  IF revenue IS NULL THEN
    RAISE EXCEPTION 'No provider account of this connector books % revenue', inv.currency
      USING ERRCODE = 'FYP31', HINT = 'Name the revenue account on the provider account first (FR-PSP-03).';
  END IF;
  SELECT * INTO cust FROM fyniq.psp_customer
   WHERE organisation_id = org AND connector_id = p_connector AND provider_customer_id = inv.provider_customer_id FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'Provider customer % is not mirrored', inv.provider_customer_id USING ERRCODE = 'FYP31';
  END IF;
  c := cust.contact_id;
  IF c IS NULL THEN
    INSERT INTO fyniq.contact (organisation_id, legal_name, is_customer, billing_email)
      VALUES (org, coalesce(nullif(btrim(cust.name), ''), nullif(btrim(cust.email), ''), cust.provider_customer_id), true, cust.email)
      RETURNING id INTO c;
    UPDATE fyniq.psp_customer SET contact_id = c WHERE organisation_id = org AND id = cust.id;
    made := true;
  END IF;
  SELECT jsonb_agg(l || jsonb_build_object('account_id', revenue, 'gst_code', 'SR')) INTO lines FROM jsonb_array_elements(inv.draft_lines) l;
  body := jsonb_build_object('customer_id', c, 'reference', coalesce(inv.number, inv.provider_invoice_id), 'date', to_char(inv.finalized_on, 'YYYY-MM-DD'),
            'currency', inv.currency, 'lines', lines, 'notes', 'Billed through the payment provider: ' || inv.provider_invoice_id,
            'provider_invoice', jsonb_build_object('connector_id', p_connector, 'id', inv.provider_invoice_id));
  IF inv.due_on IS NOT NULL THEN
    body := body || jsonb_build_object('due_date', to_char(inv.due_on, 'YYYY-MM-DD'));
  END IF;
  INSERT INTO fyniq.document (organisation_id, type) VALUES (org, 'invoice') RETURNING id INTO doc;
  INSERT INTO fyniq.document_version (organisation_id, document_id, version, payload, created_by) VALUES (org, doc, 1, body, 'psp:invoices');
  UPDATE fyniq.psp_invoice SET document_id = doc, state = 'drafted', version = version + 1 WHERE organisation_id = org AND id = inv.id;
  RETURN QUERY SELECT doc, c, made;
END
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION fyniq.create_provider_invoice_draft(uuid, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.create_provider_invoice_draft(uuid, text) TO fyniq_worker;

ALTER TABLE fyniq.psp_customer ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.psp_customer FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.psp_customer USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.psp_subscription ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.psp_subscription FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.psp_subscription USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.psp_invoice ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.psp_invoice FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.psp_invoice USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.psp_customer
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('psp_customer', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.psp_subscription
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('psp_subscription', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.psp_invoice
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('psp_invoice', 'id');

GRANT SELECT ON fyniq.psp_customer, fyniq.psp_subscription, fyniq.psp_invoice TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (connector_id, provider_customer_id, name, email, seen_at), UPDATE (name, email, seen_at) ON fyniq.psp_customer TO fyniq_worker;
GRANT INSERT (connector_id, provider_subscription_id, provider_customer_id, status, currency, amount, interval_unit, interval_count, period_start, period_end,
  cancel_at_period_end, canceled_on, seen_at),
  UPDATE (provider_customer_id, status, currency, amount, interval_unit, interval_count, period_start, period_end, cancel_at_period_end, canceled_on, seen_at)
  ON fyniq.psp_subscription TO fyniq_worker;
GRANT INSERT (connector_id, provider_invoice_id, provider_customer_id, provider_subscription_id, number, status, currency, total, amount_paid, attempt_count,
  next_attempt_on, outcome, hosted_url, finalized_on, due_on, draft_lines, seen_at),
  UPDATE (provider_subscription_id, number, status, total, amount_paid, attempt_count, next_attempt_on, outcome, hosted_url, finalized_on, due_on, draft_lines,
  state, parked_reason, version, seen_at) ON fyniq.psp_invoice TO fyniq_worker;
-- The API moves a drafted invoice to posted, or parks it, when it posts the draft (the worker never posts).
GRANT UPDATE (state, parked_reason, version) ON fyniq.psp_invoice TO fyniq_api;
