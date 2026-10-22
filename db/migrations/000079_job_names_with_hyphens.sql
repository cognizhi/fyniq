-- A platform job's name may hold hyphens (PRD-001 FR-AUT-01, FR-GL-02).
--
-- 000009 let a job's name be lower-case words joined by dots, each of letters, digits and underscores. The automatic
-- reversals' job, journals.auto-reverse (rev 0.55), has a hyphen, so the tick could never claim its slot: every tick
-- since then reported STORAGE-INPUT-01 for it, and no automatic reversal posted on its own. Its tests ran the reversals
-- directly, never through the tick. A word of a job's name may now hold hyphens as well; the check still refuses
-- anything else. Added NOT VALID and validated by 000080 (squawk: constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.job_slot DROP CONSTRAINT job_slot_job_format;
ALTER TABLE fyniq.job_slot ADD CONSTRAINT job_slot_job_format CHECK (job ~ '^[a-z][a-z0-9_-]*(\.[a-z][a-z0-9_-]*)+$' AND length(job) <= 100) NOT VALID;
