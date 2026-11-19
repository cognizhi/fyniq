-- An invitation's department scope (PRD-001 FR-ADM-01, rev 0.253 and the owner's decisions of 20 Nov 2026 [owner to confirm];
-- p1-users-roles task 2.1, with 000702). An invitation that gives the Approver role can narrow it to values of the department
-- dimension: the person who accepts holds the role with that scope from the first minute, fixed as a role given later is.
-- The scope is carried by the invitation and applied by the API in the transaction that redeems it, which the role_assignment_scope
-- guard checks (a live Approver assignment, department values only); nothing here widens a grant or a policy. The check is
-- validated by 000707, in a transaction of its own.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.member_invitation ADD COLUMN department_value_ids uuid[] NOT NULL DEFAULT '{}';
-- Only an invitation that gives the Approver role is narrowed, and to at most 20 values (as a role given later is).
ALTER TABLE fyniq.member_invitation ADD CONSTRAINT member_invitation_department_scope
  CHECK (cardinality(department_value_ids) = 0 OR ('approver' = ANY (roles) AND cardinality(department_value_ids) <= 20)) NOT VALID;

GRANT INSERT (department_value_ids) ON fyniq.member_invitation TO fyniq_api;

