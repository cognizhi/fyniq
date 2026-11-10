-- A notification follows the role (PRD-001 SEC-34; rev 0.186; p1-users-roles task 3.2): one fanned out to the people
-- whose roles hold a right on a row is shown, counted and emailed only while the person still holds it. Email held for
-- a digest or for quiet hours that the person may no longer see is withdrawn when the digest runs: recorded once, and
-- never sent after.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.notification ADD COLUMN withdrawn_at timestamptz;
GRANT UPDATE (withdrawn_at) ON fyniq.notification TO fyniq_worker;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.notification_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE'
     OR (NEW.event_id, NEW.user_id, NEW.type, NEW.severity, NEW.actionable, NEW.email, NEW.created_at)
        IS DISTINCT FROM (OLD.event_id, OLD.user_id, OLD.type, OLD.severity, OLD.actionable, OLD.email, OLD.created_at)
     OR (OLD.email_message_id IS NOT NULL AND NEW.email_message_id IS DISTINCT FROM OLD.email_message_id)
     OR (OLD.digest_id IS NOT NULL AND NEW.digest_id IS DISTINCT FROM OLD.digest_id)
     OR (OLD.archived_at IS NOT NULL AND NEW.archived_at IS DISTINCT FROM OLD.archived_at)
     OR (OLD.withdrawn_at IS NOT NULL AND NEW.withdrawn_at IS DISTINCT FROM OLD.withdrawn_at)
     OR (NEW.withdrawn_at IS NOT NULL AND (NEW.email_message_id IS NOT NULL OR NEW.digest_id IS NOT NULL)) THEN
    RAISE EXCEPTION 'A notification changes only in being read, unread or archived' USING ERRCODE = 'FY188',
      HINT = 'Mark it read or unread, or archive it (FR-NTF-01); held email is withdrawn once, and never after it was sent (SEC-34).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
