-- A disputed invoice and a promise to pay (PRD-001 FR-AR-08, P2 half, rev 0.521; p2-customer-portal-subscriptions task 2.1).
--
-- A posted invoice never changes, so a customer's dispute (an amount and a reason) or promise (a date) is a record beside it. An open
-- dispute pauses the invoice's reminders until it is resolved; an open promise until its date has passed; the AR ageing shows the
-- disputed amounts apart. At most one open dispute and one open promise per invoice. A record changes only by being resolved, once.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.invoice_dispute (
  organisation_id uuid          NOT NULL DEFAULT fyniq.current_org(),
  id              uuid          NOT NULL DEFAULT uuidv7(),
  invoice_id      uuid          NOT NULL,
  kind            text          NOT NULL,
  amount          numeric(20,4),          -- a dispute: the part of the invoice in dispute, in the invoice's currency
  currency        text,
  reason          text,                   -- a dispute: why
  promised_on     date,                   -- a promise: the day the customer says they will pay
  status          text          NOT NULL DEFAULT 'open',
  resolution      text,                   -- how it ended, when resolved
  version         integer       NOT NULL DEFAULT 1,
  created_by      text          NOT NULL,
  created_at      timestamptz   NOT NULL,
  resolved_by     text,
  resolved_at     timestamptz,
  CONSTRAINT invoice_dispute_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT invoice_dispute_invoice_fkey FOREIGN KEY (organisation_id, invoice_id) REFERENCES fyniq.document (organisation_id, id),
  CONSTRAINT invoice_dispute_kind_known CHECK (kind IN ('dispute', 'promise')),
  CONSTRAINT invoice_dispute_status_known CHECK (status IN ('open', 'resolved')),
  CONSTRAINT invoice_dispute_dispute_form CHECK (kind <> 'dispute' OR (amount IS NOT NULL AND amount <> 'NaN' AND amount > 0 AND currency ~ '^[A-Z]{3}$'
    AND reason IS NOT NULL AND length(reason) BETWEEN 1 AND 500 AND promised_on IS NULL)),
  CONSTRAINT invoice_dispute_promise_form CHECK (kind <> 'promise' OR (promised_on IS NOT NULL AND amount IS NULL AND currency IS NULL AND reason IS NULL)),
  CONSTRAINT invoice_dispute_resolved_form CHECK ((status = 'resolved') = (resolved_at IS NOT NULL AND resolved_by IS NOT NULL AND resolution IS NOT NULL
    AND length(resolution) BETWEEN 1 AND 500)),
  CONSTRAINT invoice_dispute_by_form CHECK (created_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$' AND (resolved_by IS NULL OR resolved_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$'))
);
-- One open dispute and one open promise per invoice.
CREATE UNIQUE INDEX invoice_dispute_one_open ON fyniq.invoice_dispute (organisation_id, invoice_id, kind) WHERE status = 'open';
CREATE INDEX invoice_dispute_invoice ON fyniq.invoice_dispute (organisation_id, invoice_id, created_at);

-- A record changes only by being resolved, once; it is never deleted.
-- +goose StatementBegin
CREATE FUNCTION fyniq.invoice_dispute_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR OLD.status <> 'open'
     OR (NEW.organisation_id, NEW.id, NEW.invoice_id, NEW.kind, NEW.amount, NEW.currency, NEW.reason, NEW.promised_on, NEW.created_by, NEW.created_at)
        IS DISTINCT FROM (OLD.organisation_id, OLD.id, OLD.invoice_id, OLD.kind, OLD.amount, OLD.currency, OLD.reason, OLD.promised_on, OLD.created_by, OLD.created_at) THEN
    RAISE EXCEPTION 'A dispute or promise only ends, once, and is never deleted' USING ERRCODE = 'FYP20',
      HINT = 'Record a new one if the customer says something new (FR-AR-08).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER invoice_dispute_guard BEFORE UPDATE OR DELETE ON fyniq.invoice_dispute FOR EACH ROW EXECUTE FUNCTION fyniq.invoice_dispute_guard();
CREATE TRIGGER invoice_dispute_no_truncate BEFORE TRUNCATE ON fyniq.invoice_dispute FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();

ALTER TABLE fyniq.invoice_dispute ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.invoice_dispute FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.invoice_dispute USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.invoice_dispute
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('invoice_dispute', 'id');

GRANT SELECT ON fyniq.invoice_dispute TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (invoice_id, kind, amount, currency, reason, promised_on, created_by, created_at) ON fyniq.invoice_dispute TO fyniq_api;
GRANT UPDATE (status, resolution, resolved_by, resolved_at, version) ON fyniq.invoice_dispute TO fyniq_api;
