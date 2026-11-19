-- The registered apps an organisation's allowlist names, with their names and status (PRD-001 AI-90; p1-mcp-first-party task 9.1): what
-- Connected apps shows beside each entry. Only the apps asked for are returned, so an organisation reads no registration it did not list.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- +goose StatementBegin
CREATE FUNCTION fyniq.mcp_clients_named(p_ids text[]) RETURNS SETOF fyniq.mcp_client
LANGUAGE sql SECURITY DEFINER STABLE
SET search_path = pg_catalog, fyniq, pg_temp
AS $$ SELECT * FROM fyniq.mcp_client c WHERE c.client_id = ANY (p_ids) ORDER BY c.registered_at DESC, c.client_id LIMIT 50 $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION fyniq.mcp_clients_named(text[]) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.mcp_clients_named(text[]) TO fyniq_api;
