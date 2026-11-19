-- Every request an AI app makes over MCP that reaches the API's gates (PRD-001 AI-44, AI-43, AI-45; p1-mcp-first-party task 8.1):
-- who, through which app, which tool, what became of it. One row per call, refusals and limits included, so the AI activity log
-- shows an app's whole use and the limits (requests a minute, calls an hour and a day) are counted from it. It holds no token,
-- no argument and no result: the input and the output are kept as hashes, and the app's token as a short identifier of its hash.
-- Append-only: a call is recorded once and never changed.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.mcp_call (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  user_id         uuid        NOT NULL,
  app             text        NOT NULL,                  -- the OAuth client: its name on the allowlist
  app_version     text        NOT NULL DEFAULT '',       -- as the app reports it (its user agent), cut short
  token_id        text        NOT NULL DEFAULT '',       -- the MCP token's identity: the first 16 hex characters of its SHA-256
  tool            text        NOT NULL,
  input_sha256    text        NOT NULL DEFAULT '',
  output_sha256   text        NOT NULL DEFAULT '',
  facts           integer     NOT NULL DEFAULT 0,
  duration_ms     integer     NOT NULL DEFAULT 0,
  outcome         text        NOT NULL,
  code            text        NOT NULL DEFAULT '',
  network         text        NOT NULL DEFAULT '',       -- the caller's address as a network: a /24 or a /48
  at              timestamptz NOT NULL,
  CONSTRAINT mcp_call_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT mcp_call_app CHECK (app ~ '^[a-z0-9][a-z0-9-]{0,63}$' AND length(app_version) <= 200),
  CONSTRAINT mcp_call_tool CHECK (tool ~ '^[a-z_]+(\.[a-z_]+)?$'),
  CONSTRAINT mcp_call_hashes CHECK (input_sha256 ~ '^([0-9a-f]{64})?$' AND output_sha256 ~ '^([0-9a-f]{64})?$' AND token_id ~ '^([0-9a-f]{16})?$'),
  CONSTRAINT mcp_call_outcome CHECK (outcome IN ('read', 'refused', 'limited', 'failed') AND (outcome = 'read') = (code = '')),
  CONSTRAINT mcp_call_code CHECK (code ~ '^([A-Z][A-Z0-9-]{1,40})?$'),
  CONSTRAINT mcp_call_counts CHECK (facts >= 0 AND duration_ms >= 0),
  CONSTRAINT mcp_call_network CHECK (length(network) <= 64)
);
CREATE INDEX mcp_call_of_user ON fyniq.mcp_call (organisation_id, user_id, app, at DESC);
CREATE INDEX mcp_call_recent ON fyniq.mcp_call (organisation_id, at DESC);

-- Append-only, as the opt-in and the help state are (FY189): a call is recorded as it ran.
CREATE TRIGGER mcp_call_guard BEFORE UPDATE OR DELETE ON fyniq.mcp_call
  FOR EACH ROW EXECUTE FUNCTION fyniq.notification_append_only();
CREATE TRIGGER mcp_call_no_truncate BEFORE TRUNCATE ON fyniq.mcp_call
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();

ALTER TABLE fyniq.mcp_call ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.mcp_call FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.mcp_call
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

-- The API records and reads them; the worker and the read-only role read them for the export and the access review. The AI's role
-- and the MCP service hold nothing: an app cannot write or erase its own trail.
GRANT SELECT ON fyniq.mcp_call TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (user_id, app, app_version, token_id, tool, input_sha256, output_sha256, facts, duration_ms, outcome, code, network, at)
  ON fyniq.mcp_call TO fyniq_api;
