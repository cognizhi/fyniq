-- The CPF submission file and the IR8A / AIS-API 2.0 payload are exports with a manifest of their own (PRD-002 NM-PAY-13, NM-PAY-14; p2-payroll-outputs): widen the
-- kind of nemura.pay_export from journal and payments to journal, payments, cpf and ais. The new constraint is added NOT VALID and validated by the next migration, as
-- squawk asks of a change to a table that holds rows.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE nemura.pay_export DROP CONSTRAINT pay_export_kind;
ALTER TABLE nemura.pay_export ADD CONSTRAINT pay_export_kind CHECK (kind IN ('journal', 'payments', 'cpf', 'ais')) NOT VALID;
