-- A person's opt-in to AI apps over MCP (PRD-001 AI-35; p1-mcp-first-party task 2.2): MCP access is off for each person until
-- they opt in, on top of the organisation's switch (features.mcp) and the allowlist (mcp.clients). Each change is a new row,
-- audited; the latest is in force, so the history of opting in and out stays. The API's gates read the latest row on every
-- MCP call, so opting out cuts the person's AI apps off on the next call, whatever tokens they hold.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.mcp_user_optin (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  user_id         uuid        NOT NULL,
  opted_in        boolean     NOT NULL,
  set_at          timestamptz NOT NULL,
  CONSTRAINT mcp_user_optin_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT mcp_user_optin_member_fkey FOREIGN KEY (organisation_id, user_id)
    REFERENCES fyniq.user_membership (organisation_id, user_id)
);
CREATE INDEX mcp_user_optin_latest ON fyniq.mcp_user_optin (organisation_id, user_id, set_at);

-- Append-only, as the help state and the notification preferences are (FY189): a new row is the new state.
CREATE TRIGGER mcp_user_optin_guard BEFORE UPDATE OR DELETE ON fyniq.mcp_user_optin
  FOR EACH ROW EXECUTE FUNCTION fyniq.notification_append_only();
CREATE TRIGGER mcp_user_optin_no_truncate BEFORE TRUNCATE ON fyniq.mcp_user_optin
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.mcp_user_optin
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('mcp_user_optin', 'id');

ALTER TABLE fyniq.mcp_user_optin ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.mcp_user_optin FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.mcp_user_optin
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

-- A person sets their own through the API, which saves only the caller's; the worker reads it for the tenant export. The AI's
-- role and the MCP service hold no grant at all: nothing an AI app does can opt a person in.
GRANT SELECT ON fyniq.mcp_user_optin TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (user_id, opted_in, set_at) ON fyniq.mcp_user_optin TO fyniq_api;
