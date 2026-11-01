-- A ⚠ item marked reviewed (PRD-001 FR-CTL-01, ENG-21; rev 0.164, the owner's decision of 1 Nov 2026; p1-checks slice 2).
--
-- An item a check found to review (⚠) leaves the check once someone with A on "Checks and books health" marks it
-- reviewed, with an optional note; it returns if the item changes, as a review names the item's version. The database is
-- the backstop: the API writes a review once, audited on it in the same transaction (AUD-01), and nothing changes or
-- deletes it (FY246); the worker reads it when the checks run.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.check_review (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  key             text        NOT NULL,
  item_kind       text        NOT NULL,
  item_id         text        NOT NULL,
  item_version    text        NOT NULL,
  note            text,
  reviewed_by     text        NOT NULL,
  reviewed_at     timestamptz NOT NULL,
  CONSTRAINT check_review_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT check_review_item_key UNIQUE (organisation_id, key, item_kind, item_id, item_version),
  CONSTRAINT check_review_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT check_review_key_format CHECK (key ~ '^[a-z][a-z0-9-]*(\.[a-z0-9-]+)+$' AND length(key) <= 100),
  CONSTRAINT check_review_item CHECK (item_kind ~ '^[a-z][a-z_]*$' AND length(item_kind) <= 40 AND length(btrim(item_id)) BETWEEN 1 AND 100
    AND length(item_version) <= 100),
  CONSTRAINT check_review_note CHECK (length(note) BETWEEN 1 AND 500),
  CONSTRAINT check_review_reviewed_by CHECK (length(btrim(reviewed_by)) BETWEEN 1 AND 200)
);

-- +goose StatementBegin
-- A review is written once and never changed or deleted.
CREATE FUNCTION fyniq.check_review_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'A review of a check''s item is never changed or deleted'
    USING ERRCODE = 'FY246', HINT = 'A changed item is reviewed again as its new version (FR-CTL-01).';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER check_review_guard BEFORE UPDATE OR DELETE ON fyniq.check_review
  FOR EACH ROW EXECUTE FUNCTION fyniq.check_review_guard();
CREATE TRIGGER check_review_no_truncate BEFORE TRUNCATE ON fyniq.check_review
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.check_review_guard();

ALTER TABLE fyniq.check_review ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.check_review FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.check_review
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

-- Marked reviewed: audited on the review in the same transaction (AUD-01).
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.check_review
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('check_review', 'id');

REVOKE ALL ON fyniq.check_review FROM PUBLIC;
GRANT SELECT ON fyniq.check_review TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (key, item_kind, item_id, item_version, note, reviewed_by, reviewed_at) ON fyniq.check_review TO fyniq_api;
