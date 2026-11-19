-- A person's preferences, kept on the server so they are the same on every device (PRD-001 FR-UX-23, FR-UX-27, FR-UX-08,
-- FR-UX-06, FR-UX-38, NFR-L10N-01; p1-app-shell tasks 4.1, 2.3, 3.2 and 5.3): the experience mode, the Plain or
-- Accountant view, how the app looks, the keyboard shortcuts, and the number and date formats, and how negative amounts are written. They change no business
-- data, hold nothing secret and never change what a person may do. Kept per organisation, as the help state is. Each
-- save is a new row with the whole state, audited; the latest is in force, so the history stays. A column that is NULL
-- is a choice never made: the organisation's default for the person's roles applies (FR-UX-27), or the standard one.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.user_preference (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  user_id         uuid        NOT NULL,
  mode            text,
  ledger_view     text,
  theme           text,
  contrast        text,
  density         text,
  motion          text,
  shortcuts_on    boolean,
  shortcut_keys   jsonb       NOT NULL DEFAULT '{}',
  number_format   text,
  date_format     text,
  negative_format text,
  set_at          timestamptz NOT NULL,
  CONSTRAINT user_preference_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT user_preference_member_fkey FOREIGN KEY (organisation_id, user_id)
    REFERENCES fyniq.user_membership (organisation_id, user_id),
  CONSTRAINT user_preference_mode_known CHECK (mode IS NULL OR mode IN ('basic', 'expert')),
  CONSTRAINT user_preference_view_known CHECK (ledger_view IS NULL OR ledger_view IN ('plain', 'accountant')),
  CONSTRAINT user_preference_theme_known CHECK (theme IS NULL OR theme IN ('light', 'dark', 'system')),
  CONSTRAINT user_preference_contrast_known CHECK (contrast IS NULL OR contrast IN ('standard', 'more', 'system')),
  CONSTRAINT user_preference_density_known CHECK (density IS NULL OR density IN ('comfortable', 'compact')),
  CONSTRAINT user_preference_motion_known CHECK (motion IS NULL OR motion IN ('on', 'reduce')),
  CONSTRAINT user_preference_number_known CHECK (number_format IS NULL OR number_format IN ('comma_dot', 'dot_comma', 'space_comma', 'space_dot')),
  CONSTRAINT user_preference_date_known CHECK (date_format IS NULL OR date_format IN ('d_mmm_yyyy', 'dd_mm_yyyy', 'mm_dd_yyyy', 'iso')),
  CONSTRAINT user_preference_negative_known CHECK (negative_format IS NULL OR negative_format IN ('minus', 'parentheses')),
  CONSTRAINT user_preference_keys_object CHECK (jsonb_typeof(shortcut_keys) = 'object' AND octet_length(shortcut_keys::text) <= 4096)
);
CREATE INDEX user_preference_latest ON fyniq.user_preference (organisation_id, user_id, set_at);

-- Append-only, as notification preferences and the help state are (FY189): a new row is the new state.
CREATE TRIGGER user_preference_guard BEFORE UPDATE OR DELETE ON fyniq.user_preference
  FOR EACH ROW EXECUTE FUNCTION fyniq.notification_append_only();
CREATE TRIGGER user_preference_no_truncate BEFORE TRUNCATE ON fyniq.user_preference
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.user_preference
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('user_preference', 'id');

ALTER TABLE fyniq.user_preference ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.user_preference FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.user_preference
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

-- A person sets their own through the API, which saves only the caller's; the worker reads it for the tenant export.
GRANT SELECT ON fyniq.user_preference TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (user_id, mode, ledger_view, theme, contrast, density, motion, shortcuts_on, shortcut_keys, number_format, date_format, negative_format, set_at)
  ON fyniq.user_preference TO fyniq_api;
