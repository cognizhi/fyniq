-- Validates what 000042 added: the ask client differs from the events' client, and a route says which of the two it is.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE nemura.fyniq_link VALIDATE CONSTRAINT fyniq_link_ask_differs;
ALTER TABLE nemura.link_client_route VALIDATE CONSTRAINT link_client_route_purpose;
