-- Validates the profile checks that 000018 added NOT VALID, in a transaction of their own so the scan
-- holds only a SHARE UPDATE EXCLUSIVE lock (squawk: constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.organisation VALIDATE CONSTRAINT organisation_profile_lengths;
ALTER TABLE fyniq.organisation VALIDATE CONSTRAINT organisation_uen_format;
ALTER TABLE fyniq.organisation VALIDATE CONSTRAINT organisation_fy_start_month_range;
ALTER TABLE fyniq.organisation VALIDATE CONSTRAINT organisation_first_year;
