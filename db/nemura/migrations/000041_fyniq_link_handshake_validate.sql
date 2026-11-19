-- Validates what 000040 widened: the link's states, the keys' shape, the proposer and the clients' shape.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE nemura.fyniq_link VALIDATE CONSTRAINT fyniq_link_state_known;
ALTER TABLE nemura.fyniq_link VALIDATE CONSTRAINT fyniq_link_keys;
ALTER TABLE nemura.fyniq_link VALIDATE CONSTRAINT fyniq_link_proposer_fkey;
ALTER TABLE nemura.fyniq_link VALIDATE CONSTRAINT fyniq_link_clients_shape;
ALTER TABLE nemura.fyniq_link_secret VALIDATE CONSTRAINT fyniq_link_secret_client_size;
