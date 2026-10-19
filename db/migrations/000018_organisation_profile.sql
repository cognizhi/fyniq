-- The organisation's profile, financial year and time zone (PRD-001 FR-ORG-01, FR-ORG-02;
-- p1-organisation-chart task 2.1).
--
-- The profile is what documents print about the organisation: its legal name and UEN, addresses,
-- contact details, the bank and PayNow details customers pay to, and the document footer. The
-- financial year starts in any month; the first one runs from the books' start to its own end, which may
-- make it shorter or longer than twelve months. These are mutable: the row's version counts every
-- change (FR-INT-09), and every change is an audit event (require_audit, 000006), with the bank and
-- PayNow numbers masked (SEC-15). The GST registration, which has effective dates, is its own table
-- (FR-ORG-03). The base currency never changes (organisation_guard, 000002).
--
-- The checks are added NOT VALID and validated by 000019 in a transaction of their own, so this one takes
-- only a brief lock on the table (squawk: constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.organisation
  ADD COLUMN legal_name          text,
  ADD COLUMN uen                 text,
  ADD COLUMN registered_address  text,
  ADD COLUMN mailing_address     text,
  ADD COLUMN email               text,
  ADD COLUMN phone               text,
  ADD COLUMN website             text,
  ADD COLUMN bank_name           text,
  ADD COLUMN bank_account_name   text,
  ADD COLUMN bank_account_number text,
  ADD COLUMN paynow_uen          text,
  ADD COLUMN document_footer     text,
  ADD COLUMN fy_start_month      smallint NOT NULL DEFAULT 1,
  ADD COLUMN books_start         date,
  ADD COLUMN first_year_end      date,
  ADD COLUMN time_zone           text NOT NULL DEFAULT 'Asia/Singapore';

ALTER TABLE fyniq.organisation
  ADD CONSTRAINT organisation_profile_lengths CHECK (
    length(legal_name) BETWEEN 1 AND 200 AND length(registered_address) BETWEEN 1 AND 500
    AND length(mailing_address) BETWEEN 1 AND 500 AND length(email) BETWEEN 3 AND 254
    AND length(phone) BETWEEN 1 AND 40 AND length(website) BETWEEN 1 AND 200 AND length(bank_name) BETWEEN 1 AND 100
    AND length(bank_account_name) BETWEEN 1 AND 140 AND length(bank_account_number) BETWEEN 1 AND 34
    AND length(document_footer) BETWEEN 1 AND 1000 AND length(time_zone) BETWEEN 1 AND 64) NOT VALID,
  -- A UEN is 9 or 10 letters and digits (ACRA), and PayNow by UEN takes the same form.
  ADD CONSTRAINT organisation_uen_format CHECK (uen ~ '^[0-9A-Z]{9,10}$' AND paynow_uen ~ '^[0-9A-Z]{9,10}$') NOT VALID,
  ADD CONSTRAINT organisation_fy_start_month_range CHECK (fy_start_month BETWEEN 1 AND 12) NOT VALID,
  -- The first financial year ends after the books start, on the last day of the month before a financial
  -- year starts.
  ADD CONSTRAINT organisation_first_year CHECK (first_year_end > books_start
    AND EXTRACT(DAY FROM first_year_end + 1) = 1 AND EXTRACT(MONTH FROM first_year_end + 1) = fy_start_month) NOT VALID;

GRANT UPDATE (legal_name, uen, registered_address, mailing_address, email, phone, website, bank_name, bank_account_name,
  bank_account_number, paynow_uen, document_footer, fy_start_month, books_start, first_year_end, time_zone)
  ON fyniq.organisation TO fyniq_api;
