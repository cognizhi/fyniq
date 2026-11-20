-- Validates 000054's checks (squawk: constraint-missing-not-valid).

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE nemura.person_secret VALIDATE CONSTRAINT person_secret_bank_code;
ALTER TABLE nemura.person_secret VALIDATE CONSTRAINT person_secret_bank_name;
ALTER TABLE nemura.bank_account_change VALIDATE CONSTRAINT bank_account_change_bank_code;
ALTER TABLE nemura.bank_account_change VALIDATE CONSTRAINT bank_account_change_bank_name;
