-- The company secretary assistant's reminders (PRD-001 Appendix E sec.draft_reminder; p1-scheduled-agents task 6.2,
-- design D8).
--
-- An approved sec.draft_reminder proposal is sent by the executor, as the approving person: an email of purpose
-- secretary_reminder to each member holding the role it names, through the email outbox. The executor queues that
-- purpose alone, as the worker queues only notifications and digests (000105), and reads the members' sign-in emails
-- to address it. The widened check is validated by 000389.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.email_message DROP CONSTRAINT email_message_purpose_known;
ALTER TABLE fyniq.email_message ADD CONSTRAINT email_message_purpose_known
  CHECK (purpose IN ('test', 'sign_in_alert', 'notification', 'digest', 'invoice', 'quote', 'reminder', 'statement', 'remittance',
    'secretary_reminder')) NOT VALID;

-- It reads back the message it queued, as the outbox's job names it.
GRANT SELECT, INSERT (purpose, recipient, subject) ON fyniq.email_message TO fyniq_executor;
GRANT EXECUTE ON FUNCTION fyniq.member_emails() TO fyniq_executor;

-- +goose StatementBegin
CREATE FUNCTION fyniq.email_message_executor_purposes() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF current_user = 'fyniq_executor' AND NEW.purpose <> 'secretary_reminder' THEN
    RAISE EXCEPTION 'The executor queues only the company secretary assistant''s reminders, not %', NEW.purpose USING ERRCODE = '42501';
  END IF;
  -- And only the executor queues them: they are sent once a person approves the AI's draft.
  IF NEW.purpose = 'secretary_reminder' AND current_user <> 'fyniq_executor' THEN
    RAISE EXCEPTION 'Only an approved proposal sends a reminder the AI drafted' USING ERRCODE = '42501';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER email_message_executor_purposes BEFORE INSERT ON fyniq.email_message
  FOR EACH ROW EXECUTE FUNCTION fyniq.email_message_executor_purposes();
