-- When a connector's secret expires (PRD-001 FR-INT-20, C.2 #38; rev 0.166, the owner's decision of 2 Nov 2026).
--
-- Whoever sets an organisation connector's secret may give the date the provider says it expires, such as an API key's
-- or a bank certificate's; C.2 #38 warns 14 days before. The date is set with the secret's version, through the same
-- approval, and never changes: a new date comes with a new version. Connectors that can read their own token's expiry
-- fill it in later (P2).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.organisation_connector_secret ADD COLUMN expires_on date;

GRANT INSERT (expires_on) ON fyniq.organisation_connector_secret TO fyniq_api;

-- +goose StatementBegin
-- A secret's version is only replaced and destroyed, each once (000141); its expiry date never changes.
CREATE OR REPLACE FUNCTION fyniq.connector_secret_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE'
     OR (TG_OP = 'UPDATE' AND ((NEW.id, NEW.connector_id, NEW.name, NEW.ref, NEW.set_by, NEW.set_at)
            IS DISTINCT FROM (OLD.id, OLD.connector_id, OLD.name, OLD.ref, OLD.set_by, OLD.set_at)
         OR (to_jsonb(NEW) -> 'expires_on') IS DISTINCT FROM (to_jsonb(OLD) -> 'expires_on')
         OR (OLD.valid_until IS NOT NULL AND NEW.valid_until IS DISTINCT FROM OLD.valid_until)
         OR (OLD.destroyed_at IS NOT NULL AND NEW.destroyed_at IS DISTINCT FROM OLD.destroyed_at))) THEN
    RAISE EXCEPTION 'A connector''s secret version is only replaced and destroyed, each once' USING ERRCODE = 'FY208',
      HINT = 'Set a new version instead (FR-INT-26).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
