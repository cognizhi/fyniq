-- The Data Protection Officer and their business contact (PRD-001 CMP-PD-01, rev 0.244; p1-pdpa-retention slice 2,
-- design D1): the member the Owner names as the DPO, if the DPO is a member, and the name and email address the company
-- publishes for them (PDPA s11(5)). An outsourced DPO has a contact and no member. A member named as the DPO needs the
-- contact too.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.organisation
  ADD COLUMN dpo_user_id uuid,
  ADD COLUMN dpo_name    text,
  ADD COLUMN dpo_email   text;

ALTER TABLE fyniq.organisation
  ADD CONSTRAINT organisation_dpo_contact CHECK (length(dpo_name) BETWEEN 1 AND 200 AND length(dpo_email) BETWEEN 3 AND 254
    AND (dpo_user_id IS NULL OR (dpo_name IS NOT NULL AND dpo_email IS NOT NULL))) NOT VALID;

GRANT UPDATE (dpo_user_id, dpo_name, dpo_email) ON fyniq.organisation TO fyniq_api;
