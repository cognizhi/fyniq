-- Validates what 000903 added NOT VALID: the shape of the origin Nemura's web app is served from.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.people_link VALIDATE CONSTRAINT people_link_origin_shape;
