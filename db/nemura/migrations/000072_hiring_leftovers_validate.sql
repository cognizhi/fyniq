-- Validates what 000071 added NOT VALID: the foreign key of a requisition's hiring manager (a column that was empty when it was added).

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE nemura.requisition VALIDATE CONSTRAINT requisition_manager_fkey;
