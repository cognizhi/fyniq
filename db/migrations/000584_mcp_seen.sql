-- What AI-45's detection remembers (p1-mcp-first-party task 8.3): the first time a person used an app, and the first time from a
-- network, so a new app or a new place for a person raises an alert once. Append-only and unique: a pair is recorded once.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.mcp_seen (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  user_id         uuid        NOT NULL,
  kind            text        NOT NULL,                  -- app or network
  value           text        NOT NULL,
  first_seen      timestamptz NOT NULL,
  CONSTRAINT mcp_seen_pkey PRIMARY KEY (organisation_id, user_id, kind, value),
  CONSTRAINT mcp_seen_kind CHECK (kind IN ('app', 'network') AND length(value) BETWEEN 1 AND 64)
);

CREATE TRIGGER mcp_seen_guard BEFORE UPDATE OR DELETE ON fyniq.mcp_seen
  FOR EACH ROW EXECUTE FUNCTION fyniq.notification_append_only();
CREATE TRIGGER mcp_seen_no_truncate BEFORE TRUNCATE ON fyniq.mcp_seen
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();

ALTER TABLE fyniq.mcp_seen ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.mcp_seen FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.mcp_seen
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

GRANT SELECT ON fyniq.mcp_seen TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (user_id, kind, value, first_seen) ON fyniq.mcp_seen TO fyniq_api;
