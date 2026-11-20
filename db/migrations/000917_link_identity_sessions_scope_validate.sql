-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.api_client VALIDATE CONSTRAINT api_client_scopes_format;
