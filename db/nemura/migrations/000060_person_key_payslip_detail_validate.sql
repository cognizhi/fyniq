-- Validates 000059's check (squawk: constraint-missing-not-valid).

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE nemura.payslip VALIDATE CONSTRAINT payslip_sealed_has_marker;
