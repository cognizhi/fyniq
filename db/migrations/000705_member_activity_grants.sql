-- Members().List reads member_activity() (000704) beside member_names(), and the worker, the executor and the jobs list members
-- too, to name an audience or a role holder: the fan-out of a notification failed for want of the grant. Both functions name the
-- current organisation's members alone and return no secret (a name; a time and a count), so the roles that may call
-- member_names() may call member_activity(). The list also reads the department scopes (000702) of the live roles, which the executor, who holds
-- SELECT on role_assignment, may read too.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

GRANT EXECUTE ON FUNCTION fyniq.member_activity() TO fyniq_worker, fyniq_executor;
GRANT SELECT ON fyniq.role_assignment_scope TO fyniq_executor;
