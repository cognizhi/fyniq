-- The executor adds a recurring journal (PRD-001 Appendix E recur.create_template; p1-ai-proposals task 4.1, design D8).
--
-- An approved recur.create_template proposal adds a recurring journal through the function a person's Add a recurring journal runs, as the approving
-- person: the row and its first content version, with exactly the columns the API inserts. The executor holds NO update or delete on either table, so it
-- cannot change a template's schedule, content or next occurrence, make a draft of one (create_scheduled_draft is the worker's) or touch a recurring bill or
-- invoice; the undo of the proposal is a person's pause, under the API's own role. Needs human review (grants).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

GRANT INSERT (name, status, every_months, day_of_month, starts_on, ends_on, next_on) ON fyniq.recurring_journal TO fyniq_executor;
GRANT INSERT (recurring_journal_id, version, content, edited_by) ON fyniq.recurring_journal_version TO fyniq_executor;

-- +goose Down
REVOKE INSERT (name, status, every_months, day_of_month, starts_on, ends_on, next_on) ON fyniq.recurring_journal FROM fyniq_executor;
REVOKE INSERT (recurring_journal_id, version, content, edited_by) ON fyniq.recurring_journal_version FROM fyniq_executor;
