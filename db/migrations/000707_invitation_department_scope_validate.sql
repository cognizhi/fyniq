-- Validates the check 000706 added NOT VALID, in a transaction of its own (squawk: constraint-missing-not-valid).

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.member_invitation VALIDATE CONSTRAINT member_invitation_department_scope;
