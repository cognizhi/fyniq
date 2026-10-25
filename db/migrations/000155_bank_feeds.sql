-- The bank-data connectors' feeds (PRD-001 FR-BNK-15, FR-BNK-16; p1-bank-import-reconcile task 8.2, design D11).
--
-- A money account fed by an organisation's bank-data connector keeps where its feed got to: the provider's account it
-- is, the cursor after the last transaction imported, when it last synced and why it stopped, if it did. The worker's
-- sync writes it in the unit of work that imports the statement, so the cursor never runs ahead of the lines.
--
-- A provider's webhook names an installed connector by its ID before any tenant is set; the global registry
-- connector_route, kept by a trigger, finds its organisation, connector and state, and only the function
-- organisation_connector_route reads it (as capture_route does for email-in addresses).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.bank_feed (
  organisation_id  uuid        NOT NULL DEFAULT fyniq.current_org(),
  id               uuid        NOT NULL DEFAULT uuidv7(),
  money_account_id uuid        NOT NULL,
  connector_id     uuid        NOT NULL,
  provider_account text        NOT NULL,
  cursor           text        NOT NULL DEFAULT '',
  synced_at        timestamptz NOT NULL,
  last_error       text,
  version          integer     NOT NULL DEFAULT 1,
  CONSTRAINT bank_feed_pkey PRIMARY KEY (organisation_id, money_account_id),
  CONSTRAINT bank_feed_id_key UNIQUE (organisation_id, id),
  CONSTRAINT bank_feed_account_fkey FOREIGN KEY (organisation_id, money_account_id) REFERENCES fyniq.money_account (organisation_id, id),
  CONSTRAINT bank_feed_connector_fkey FOREIGN KEY (organisation_id, connector_id) REFERENCES fyniq.organisation_connector (organisation_id, id),
  CONSTRAINT bank_feed_provider_account_length CHECK (length(provider_account) BETWEEN 1 AND 200),
  CONSTRAINT bank_feed_cursor_length CHECK (length(cursor) <= 2000),
  CONSTRAINT bank_feed_last_error_length CHECK (length(last_error) <= 500),
  CONSTRAINT bank_feed_version_positive CHECK (version >= 1)
);
CREATE INDEX bank_feed_connector ON fyniq.bank_feed (organisation_id, connector_id);

-- A feed moves one version at a time, and never to another money account (FY230).
-- +goose StatementBegin
CREATE FUNCTION fyniq.bank_feed_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A bank feed is never deleted' USING ERRCODE = 'FY230', HINT = 'Its money account moves to another source instead.';
  END IF;
  IF TG_OP = 'INSERT' THEN
    IF NEW.version <> 1 THEN
      RAISE EXCEPTION 'A bank feed starts at version 1' USING ERRCODE = 'FY230';
    END IF;
    RETURN NEW;
  END IF;
  IF NEW.money_account_id <> OLD.money_account_id OR NEW.id <> OLD.id OR NEW.version <> OLD.version + 1 THEN
    RAISE EXCEPTION 'Bank feed of % changes one version at a time, on its own money account', OLD.money_account_id USING ERRCODE = 'FY230';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER bank_feed_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.bank_feed
  FOR EACH ROW EXECUTE FUNCTION fyniq.bank_feed_guard();
CREATE TRIGGER bank_feed_no_truncate BEFORE TRUNCATE ON fyniq.bank_feed
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();

ALTER TABLE fyniq.bank_feed ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.bank_feed FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.bank_feed
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.bank_feed
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('bank_feed', 'money_account_id');

GRANT SELECT ON fyniq.bank_feed TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (money_account_id, connector_id, provider_account, cursor, synced_at, last_error) ON fyniq.bank_feed TO fyniq_worker;
GRANT UPDATE (connector_id, provider_account, cursor, synced_at, last_error, version) ON fyniq.bank_feed TO fyniq_worker;

-- ------------------------------------------------------------------ the webhook's route to a connector
CREATE TABLE fyniq.connector_route (
  connector_id uuid NOT NULL,
  owner        uuid NOT NULL, -- the organisation that installed it
  connector    text NOT NULL,
  state        text NOT NULL,
  CONSTRAINT connector_route_pkey PRIMARY KEY (connector_id)
);
REVOKE ALL ON fyniq.connector_route FROM PUBLIC;

-- +goose StatementBegin
CREATE FUNCTION fyniq.organisation_connector_routed() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  INSERT INTO fyniq.connector_route (connector_id, owner, connector, state)
  VALUES (NEW.id, NEW.organisation_id, NEW.connector, NEW.state)
  ON CONFLICT (connector_id) DO UPDATE SET state = EXCLUDED.state;
  RETURN NULL;
END
$$;

-- The organisation, connector and state of an installed connector, before any tenant is set; nothing for an unknown ID.
CREATE FUNCTION fyniq.organisation_connector_route(p_id uuid) RETURNS TABLE (organisation_id uuid, connector text, state text)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT r.owner, r.connector, r.state FROM fyniq.connector_route r WHERE r.connector_id = p_id
$$;
-- +goose StatementEnd
CREATE TRIGGER organisation_connector_routed AFTER INSERT OR UPDATE OF state ON fyniq.organisation_connector
  FOR EACH ROW EXECUTE FUNCTION fyniq.organisation_connector_routed();

-- No connector reaches a bank-data webhook before this migration, so none is routed back: an installed connector is routed
-- at its next change of state (the table's rows are invisible here under row-level security).

REVOKE ALL ON FUNCTION fyniq.organisation_connector_route(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.organisation_connector_route(uuid) TO fyniq_api;
