-- The company secretary's assistant is an agent of AI-74 (PRD-001 §9.12, AI-61; p1-scheduled-agents task 1.1, design
-- D1): routed and budgeted as its own, so its runs are seen, metered and paused apart from the assistant's turns. It
-- reads ledger summaries, the calendar and the registers, which hold holders' and officers' names (personal data), and
-- never payroll.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

INSERT INTO fyniq.ai_agent (id, name, description, classes, line, position) VALUES
  ('company_secretary', 'Company secretary assistant',
   'Checks the company''s obligations on its schedule, reports, raises tasks and drafts reminders for people to approve.',
   ARRAY['know-how', 'business-figures', 'personal-data'], 'main', 45);
