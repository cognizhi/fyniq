-- Validates what 000901 widened: the approval engine's object type and the API client's kind, scopes and key rules.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.approval_request VALIDATE CONSTRAINT approval_request_object_known;
ALTER TABLE fyniq.approval_policy VALIDATE CONSTRAINT approval_policy_object_known;
ALTER TABLE fyniq.api_client VALIDATE CONSTRAINT api_client_kind_known;
ALTER TABLE fyniq.api_client VALIDATE CONSTRAINT api_client_scopes_format;
ALTER TABLE fyniq.api_client VALIDATE CONSTRAINT api_client_integration_key;
