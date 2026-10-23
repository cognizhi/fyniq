-- The address an organisation's email is sent from, per purpose (PRD-001 FR-INT-21; the owner's decision of 23 Oct 2026,
-- rev 0.102; p1-templates-email task 2.1).
--
-- The deployment sets the providers, their keys and the domains mail may be sent from; each organisation chooses the
-- sender of each purpose (notifications, invoices and quotes, reminders, security) on one of those domains. A choice is
-- append-only: the latest for a purpose is in force, and one with no address returns to the platform's sender. A choice
-- never changes or goes (FY179).
--
-- An attempt now records the provider it went to, so a send whose result is unknown is reconciled with that provider.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.email_sender (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  purpose         text        NOT NULL,
  address         text,                  -- none: back to the platform's sender
  display_name    text,
  set_by          text        NOT NULL,
  set_at          timestamptz NOT NULL,
  CONSTRAINT email_sender_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT email_sender_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT email_sender_purpose_known CHECK (purpose IN ('notifications', 'invoices', 'reminders', 'security')),
  CONSTRAINT email_sender_address_format CHECK (address ~ '^[^@\s<>"]{1,64}@[a-z0-9.-]{1,189}$'),
  CONSTRAINT email_sender_name_length CHECK (length(display_name) BETWEEN 1 AND 80 AND display_name !~ '[\r\n<>"]'),
  CONSTRAINT email_sender_name_needs_address CHECK (display_name IS NULL OR address IS NOT NULL),
  CONSTRAINT email_sender_set_by_format CHECK (set_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);
CREATE INDEX email_sender_latest ON fyniq.email_sender (organisation_id, purpose, set_at);

-- A choice never changes or goes (FY179).
-- +goose StatementBegin
CREATE FUNCTION fyniq.email_sender_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'An email sender never changes or goes' USING ERRCODE = 'FY179',
    HINT = 'Choose again: the latest sender of a purpose is in force (FR-INT-21).';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER email_sender_guard BEFORE UPDATE OR DELETE ON fyniq.email_sender
  FOR EACH ROW EXECUTE FUNCTION fyniq.email_sender_guard();
CREATE TRIGGER email_sender_no_truncate BEFORE TRUNCATE ON fyniq.email_sender
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();

ALTER TABLE fyniq.email_sender ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.email_sender FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.email_sender
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.email_sender
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('email_sender', 'id');

-- The API records a choice; the worker reads the one in force when it sends.
GRANT SELECT ON fyniq.email_sender TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (purpose, address, display_name, set_by, set_at) ON fyniq.email_sender TO fyniq_api;

