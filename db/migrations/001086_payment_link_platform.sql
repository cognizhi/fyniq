-- A payment link made by the platform when an invoice is issued (PRD-001 FR-AR-13, rev 0.588; p2-bank-feeds-payments task 5.2).
--
-- When an organisation has switched on automatic payment links, issuing an invoice queues a job that asks the payment provider for the link, outside the request, and records it through
-- this one function, which makes a live link and nothing else: it posts nothing and changes no invoice. It refuses an invoice that is not a posted invoice, a connector that is not the
-- organisation's own enabled one, a URL that is not https, and a second live link for the invoice (the unique index). The worker is granted the function, not the table. (The pattern is
-- create_scheduled_draft, 000064.)

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- +goose StatementBegin
CREATE FUNCTION fyniq.create_payment_link_by_platform(p_invoice uuid, p_connector uuid, p_request text, p_url text, p_reference text, p_currency text,
                                                      p_amount numeric, p_expires timestamptz) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  org uuid := fyniq.require_org();
  id  uuid;
BEGIN
  IF NOT EXISTS (SELECT 1 FROM fyniq.document d WHERE d.organisation_id = org AND d.id = p_invoice AND d.type = 'invoice' AND d.status = 'posted') THEN
    RAISE EXCEPTION 'Document % is not an issued invoice', p_invoice USING ERRCODE = 'FYP26', HINT = 'A payment link is for an issued invoice (FR-AR-13).';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM fyniq.organisation_connector c WHERE c.organisation_id = org AND c.id = p_connector AND c.state = 'enabled') THEN
    RAISE EXCEPTION 'Connector % is not an enabled connector of the organisation', p_connector USING ERRCODE = 'FYP26';
  END IF;
  INSERT INTO fyniq.payment_link (organisation_id, connector_id, invoice_id, provider_request_id, url, reference, currency, amount, expires_at, created_by, created_at)
    VALUES (org, p_connector, p_invoice, p_request, p_url, p_reference, p_currency, p_amount, p_expires, 'scheduler:payment_link', now())
    RETURNING payment_link.id INTO id;
  RETURN id;
END
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION fyniq.create_payment_link_by_platform(uuid, uuid, text, text, text, text, numeric, timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.create_payment_link_by_platform(uuid, uuid, text, text, text, text, numeric, timestamptz) TO fyniq_worker;
