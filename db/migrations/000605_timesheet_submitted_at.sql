-- A week is submitted at the app's clock, not the database's (DEV-05; p1-projects task 4.3): the checks that flag a week left waiting
-- read the same clock as the tests and the scheduler.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

GRANT INSERT (submitted_at) ON fyniq.timesheet_submission TO fyniq_api;
