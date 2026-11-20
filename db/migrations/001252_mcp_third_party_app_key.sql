-- The third-party AI apps' client ID is unique within the organisation, not across the installation (PRD-001 AI-46; the tenant tables' rule, §11.4: organisation_id
-- leads every index). The ID is random (mcp- and 20 hex digits) and every read of it is inside the organisation's own unit of work, so a global key bought nothing and
-- failed the catalogue's rule. The table is new and has no row to scan.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.mcp_third_party_app DROP CONSTRAINT mcp_third_party_app_client_once;
-- squawk-ignore require-concurrent-index-creation
CREATE UNIQUE INDEX mcp_third_party_app_client_once ON fyniq.mcp_third_party_app (organisation_id, client_id);
