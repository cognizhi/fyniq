-- Validates the constraint 000050 added NOT VALID (PRD-002 NM-PAY-13, NM-PAY-14).

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE nemura.pay_export VALIDATE CONSTRAINT pay_export_kind;
