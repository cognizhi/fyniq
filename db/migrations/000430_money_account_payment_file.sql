-- The payment file a money account gives its payment batches (PRD-001 FR-BNK-12, rev 0.157; p1-payment-files task 1.1,
-- design D1 and D2).
--
-- Each money account says which file a batch paying from it gives when it is released: the payment list (the default,
-- for any bank), Wise's batch payment file, or DBS IDEAL's UFF. Release reads the setting as it writes the file, and the
-- release row keeps the writer's name and version and the file sealed (000185), so a batch released before a change
-- keeps its file. The check names the three files the owner decided on 28 Oct 2026; the API takes one only once its
-- writer is built and the account's bank takes it, and release refuses any other (decided in task 1.1, so a writer
-- arrives without a migration). Changing it is changing the money account: whoever may, audited with the old and new
-- value. Which bank or provider an account is at is read from its statement connector, or else from its institution
-- matched against Fyniq's list (design D2, decided in task 1.1): no column, so the free text existing accounts were
-- given maps the same way as a bank picked from the list. The check is added NOT VALID and 000431 validates it (squawk:
-- constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.money_account ADD COLUMN payment_file text NOT NULL DEFAULT 'payment-list';
ALTER TABLE fyniq.money_account ADD CONSTRAINT money_account_payment_file_known
  CHECK (payment_file IN ('payment-list', 'wise-batch', 'dbs-uff')) NOT VALID;
-- Whoever sets up or changes a money account sets its payment file.
GRANT INSERT (payment_file), UPDATE (payment_file) ON fyniq.money_account TO fyniq_api;
