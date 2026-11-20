-- The monthly board pack and the investor update built from it (PRD-001 FR-RPT-06, FR-EQ-23, rev 0.540; p2-reporting-planning tasks 3.1
-- and 3.3, design D4 to D6).
--
-- A board pack is one per month and version. A draft holds the text a person writes (highlights, commentary, asks) and is changed by
-- them as often as they like; publishing freezes it: the statements and KPIs of the month as they were read at one ledger revision are
-- kept in the row, so the pack a board read is the pack it keeps reading. A correction is a new version that supersedes the first, which
-- stays. An investor update is built from a published pack, with its own highlights and asks, and is sent once, to the people named on
-- its distribution list; each person's first open of it is recorded.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.board_pack (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  month           date        NOT NULL,
  version         integer     NOT NULL DEFAULT 1,
  status          text        NOT NULL DEFAULT 'draft',
  highlights      text        NOT NULL DEFAULT '',
  commentary      text        NOT NULL DEFAULT '',
  asks            text        NOT NULL DEFAULT '',
  figures         jsonb,                    -- the statements and KPIs as read when it was published; null for a draft
  ledger_revision bigint,
  supersedes      uuid,
  edit            integer     NOT NULL DEFAULT 1,  -- a draft's edits, for If-Match
  created_by      text        NOT NULL,
  created_at      timestamptz NOT NULL,
  updated_by      text        NOT NULL,
  updated_at      timestamptz NOT NULL,
  published_by    text,
  published_at    timestamptz,
  CONSTRAINT board_pack_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT board_pack_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT board_pack_supersedes_fkey FOREIGN KEY (organisation_id, supersedes) REFERENCES fyniq.board_pack (organisation_id, id),
  CONSTRAINT board_pack_month_version UNIQUE (organisation_id, month, version),
  CONSTRAINT board_pack_first_of_month CHECK (month = date_trunc('month', month)::date),
  CONSTRAINT board_pack_status CHECK (status IN ('draft', 'published')),
  CONSTRAINT board_pack_published_form CHECK ((status = 'published') = (figures IS NOT NULL AND ledger_revision IS NOT NULL AND published_by IS NOT NULL AND published_at IS NOT NULL)),
  CONSTRAINT board_pack_figures_object CHECK (figures IS NULL OR jsonb_typeof(figures) = 'object'),
  CONSTRAINT board_pack_size CHECK (pg_column_size(figures) <= 4194304 AND length(highlights) <= 5000 AND length(commentary) <= 20000 AND length(asks) <= 5000),
  CONSTRAINT board_pack_by_form CHECK (created_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$' AND updated_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$'
    AND (published_by IS NULL OR published_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')),
  CONSTRAINT board_pack_version_positive CHECK (version >= 1 AND edit >= 1)
);
-- One draft per month at a time.
CREATE UNIQUE INDEX board_pack_one_draft ON fyniq.board_pack (organisation_id, month) WHERE status = 'draft';

CREATE TABLE fyniq.investor_update (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  pack_id         uuid        NOT NULL,
  kind            text        NOT NULL,
  title           text        NOT NULL,
  highlights      text        NOT NULL DEFAULT '',
  asks            text        NOT NULL DEFAULT '',
  recipients      uuid[]      NOT NULL DEFAULT '{}',  -- member user IDs; fixed when it is sent
  status          text        NOT NULL DEFAULT 'draft',
  edit            integer     NOT NULL DEFAULT 1,
  created_by      text        NOT NULL,
  created_at      timestamptz NOT NULL,
  updated_by      text        NOT NULL,
  updated_at      timestamptz NOT NULL,
  sent_by         text,
  sent_at         timestamptz,
  CONSTRAINT investor_update_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT investor_update_pack_fkey FOREIGN KEY (organisation_id, pack_id) REFERENCES fyniq.board_pack (organisation_id, id),
  CONSTRAINT investor_update_kind CHECK (kind IN ('monthly', 'quarterly')),
  CONSTRAINT investor_update_status CHECK (status IN ('draft', 'sent')),
  CONSTRAINT investor_update_title CHECK (length(btrim(title)) BETWEEN 1 AND 120),
  CONSTRAINT investor_update_sent_form CHECK ((status = 'sent') = (sent_by IS NOT NULL AND sent_at IS NOT NULL AND cardinality(recipients) BETWEEN 1 AND 200)),
  CONSTRAINT investor_update_size CHECK (length(highlights) <= 5000 AND length(asks) <= 5000),
  CONSTRAINT investor_update_by_form CHECK (created_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$' AND updated_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$'
    AND (sent_by IS NULL OR sent_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')),
  CONSTRAINT investor_update_edit_positive CHECK (edit >= 1)
);

-- A person's first open of a sent update, once.
CREATE TABLE fyniq.investor_update_read (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  update_id       uuid        NOT NULL,
  user_id         uuid        NOT NULL,
  read_at         timestamptz NOT NULL,
  CONSTRAINT investor_update_read_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT investor_update_read_update_fkey FOREIGN KEY (organisation_id, update_id) REFERENCES fyniq.investor_update (organisation_id, id),
  CONSTRAINT investor_update_read_once UNIQUE (organisation_id, update_id, user_id)
);

-- +goose StatementBegin
-- A published pack never changes; a draft changes only in its texts and its edit count, or by being published, once; nothing is deleted.
CREATE FUNCTION fyniq.board_pack_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF NEW.status <> 'draft' OR NEW.edit <> 1 THEN
      RAISE EXCEPTION 'A board pack starts as a draft' USING ERRCODE = 'FYR04', HINT = 'Make the draft, then publish it (FR-RPT-06).';
    END IF;
    RETURN NEW;
  END IF;
  IF TG_OP <> 'UPDATE' THEN
    RAISE EXCEPTION 'A board pack is never deleted or truncated' USING ERRCODE = 'FYR04', HINT = 'Publish a corrected version that supersedes it (FR-RPT-06).';
  END IF;
  IF OLD.status = 'published' THEN
    RAISE EXCEPTION 'A published board pack never changes' USING ERRCODE = 'FYR04', HINT = 'Publish a corrected version that supersedes it (FR-RPT-06).';
  END IF;
  IF NEW.id IS DISTINCT FROM OLD.id OR NEW.organisation_id IS DISTINCT FROM OLD.organisation_id OR NEW.month IS DISTINCT FROM OLD.month
     OR NEW.version IS DISTINCT FROM OLD.version OR NEW.supersedes IS DISTINCT FROM OLD.supersedes OR NEW.created_by IS DISTINCT FROM OLD.created_by
     OR NEW.created_at IS DISTINCT FROM OLD.created_at OR NEW.edit <> OLD.edit + 1 THEN
    RAISE EXCEPTION 'A board pack draft changes only as its next edit, and keeps its month and version' USING ERRCODE = 'FYR04',
      HINT = 'Read the draft and save the change as its next edit (FR-RPT-06).';
  END IF;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER board_pack_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.board_pack FOR EACH ROW EXECUTE FUNCTION fyniq.board_pack_guard();
CREATE TRIGGER board_pack_no_truncate BEFORE TRUNCATE ON fyniq.board_pack FOR EACH STATEMENT EXECUTE FUNCTION fyniq.board_pack_guard();

-- +goose StatementBegin
-- An update changes while it is a draft; once sent it never does. Nothing is deleted.
CREATE FUNCTION fyniq.investor_update_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF NEW.status <> 'draft' OR NEW.edit <> 1 THEN
      RAISE EXCEPTION 'An investor update starts as a draft' USING ERRCODE = 'FYR05', HINT = 'Make the draft, then send it (FR-EQ-23).';
    END IF;
    RETURN NEW;
  END IF;
  IF TG_OP <> 'UPDATE' THEN
    RAISE EXCEPTION 'An investor update is never deleted or truncated' USING ERRCODE = 'FYR05', HINT = 'Send a new update instead (FR-EQ-23).';
  END IF;
  IF OLD.status = 'sent' THEN
    RAISE EXCEPTION 'A sent investor update never changes' USING ERRCODE = 'FYR05', HINT = 'Send a new update instead (FR-EQ-23).';
  END IF;
  IF NEW.id IS DISTINCT FROM OLD.id OR NEW.organisation_id IS DISTINCT FROM OLD.organisation_id OR NEW.pack_id IS DISTINCT FROM OLD.pack_id
     OR NEW.kind IS DISTINCT FROM OLD.kind OR NEW.created_by IS DISTINCT FROM OLD.created_by OR NEW.created_at IS DISTINCT FROM OLD.created_at
     OR NEW.edit <> OLD.edit + 1 THEN
    RAISE EXCEPTION 'An investor update draft changes only as its next edit, and keeps its pack' USING ERRCODE = 'FYR05',
      HINT = 'Read the draft and save the change as its next edit (FR-EQ-23).';
  END IF;
  IF NEW.status = 'sent' AND (SELECT status FROM fyniq.board_pack WHERE organisation_id = NEW.organisation_id AND id = NEW.pack_id) <> 'published' THEN
    RAISE EXCEPTION 'An investor update is sent from a published pack only' USING ERRCODE = 'FYR05', HINT = 'Publish the pack first (FR-EQ-23).';
  END IF;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER investor_update_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.investor_update FOR EACH ROW EXECUTE FUNCTION fyniq.investor_update_guard();
CREATE TRIGGER investor_update_no_truncate BEFORE TRUNCATE ON fyniq.investor_update FOR EACH STATEMENT EXECUTE FUNCTION fyniq.investor_update_guard();

-- +goose StatementBegin
CREATE FUNCTION fyniq.investor_update_read_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF NOT EXISTS (SELECT 1 FROM fyniq.investor_update u WHERE u.organisation_id = NEW.organisation_id AND u.id = NEW.update_id AND u.status = 'sent'
                   AND NEW.user_id = ANY (u.recipients)) THEN
      RAISE EXCEPTION 'A read is recorded for a person the update was sent to' USING ERRCODE = 'FYR06', HINT = 'Only a recipient of a sent update opens it as read (FR-EQ-23).';
    END IF;
    RETURN NEW;
  END IF;
  RAISE EXCEPTION 'A recorded read is never changed or deleted' USING ERRCODE = 'FYR06', HINT = 'A person is recorded as having read an update once (FR-EQ-23).';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER investor_update_read_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.investor_update_read FOR EACH ROW EXECUTE FUNCTION fyniq.investor_update_read_guard();
CREATE TRIGGER investor_update_read_no_truncate BEFORE TRUNCATE ON fyniq.investor_update_read FOR EACH STATEMENT EXECUTE FUNCTION fyniq.investor_update_read_guard();

ALTER TABLE fyniq.board_pack ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.board_pack FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.board_pack USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.investor_update ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.investor_update FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.investor_update USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.investor_update_read ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.investor_update_read FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.investor_update_read USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.board_pack
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('board_pack', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.investor_update
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('investor_update', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.investor_update_read
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('investor_update_read', 'id');

REVOKE ALL ON fyniq.board_pack, fyniq.investor_update, fyniq.investor_update_read FROM PUBLIC;
GRANT SELECT ON fyniq.board_pack, fyniq.investor_update, fyniq.investor_update_read TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (month, version, highlights, commentary, asks, supersedes, created_by, created_at, updated_by, updated_at) ON fyniq.board_pack TO fyniq_api;
GRANT UPDATE (highlights, commentary, asks, edit, updated_by, updated_at, status, figures, ledger_revision, published_by, published_at) ON fyniq.board_pack TO fyniq_api;
GRANT INSERT (pack_id, kind, title, highlights, asks, created_by, created_at, updated_by, updated_at) ON fyniq.investor_update TO fyniq_api;
GRANT UPDATE (title, highlights, asks, recipients, status, edit, updated_by, updated_at, sent_by, sent_at) ON fyniq.investor_update TO fyniq_api;
GRANT INSERT (update_id, user_id, read_at) ON fyniq.investor_update_read TO fyniq_api;
