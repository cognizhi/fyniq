-- The worker drafts the month-end leave accrual by itself (PRD-002 NM-LV-09; p1-nemura-leave-time, left items).
--
-- A draft made by the worker has no member who made it: it says so (system_drafted), and the worker role is granted exactly this insert and nothing else of leave. The
-- rule that nobody approves what they drafted still holds: a person approves a system draft, and a system never approves.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE nemura.leave_accrual ADD COLUMN system_drafted boolean NOT NULL DEFAULT false;
-- squawk-ignore ban-drop-not-null
ALTER TABLE nemura.leave_accrual ALTER COLUMN drafted_by DROP NOT NULL;
ALTER TABLE nemura.leave_accrual ADD CONSTRAINT leave_accrual_drafter CHECK ((drafted_by IS NOT NULL) <> system_drafted) NOT VALID;

GRANT INSERT (id, month, person_id, carried_days, annual_left, system_drafted, drafted_at) ON nemura.leave_accrual TO nemura_worker;
