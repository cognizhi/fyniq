-- A person's favourites and saved views (PRD-001 FR-UX-12; p1-app-shell task 3.3): a page they pinned, or the page with
-- the filters, columns and sort its address carries, with the name they gave it. Their own, per organisation, the same on
-- every device. Each change is a new row, audited; the latest row of a key is in force, and a row whose state is 'off'
-- takes the item away, so nothing is updated or deleted (no hard delete, FR-UX-14). A view's link is the address itself,
-- so sharing it shares no data: whoever opens it sees what their own roles let them see.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.user_saved_item (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  user_id         uuid        NOT NULL,
  kind            text        NOT NULL,
  item_key        text        NOT NULL,  -- a favourite's address; a view's own ID
  state           text        NOT NULL,
  title           text        NOT NULL,
  path            text        NOT NULL,
  query           text        NOT NULL DEFAULT '',
  set_at          timestamptz NOT NULL,
  CONSTRAINT user_saved_item_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT user_saved_item_member_fkey FOREIGN KEY (organisation_id, user_id)
    REFERENCES fyniq.user_membership (organisation_id, user_id),
  CONSTRAINT user_saved_item_kind_known CHECK (kind IN ('favourite', 'view')),
  CONSTRAINT user_saved_item_state_known CHECK (state IN ('on', 'off')),
  CONSTRAINT user_saved_item_title_length CHECK (char_length(title) BETWEEN 1 AND 80),
  CONSTRAINT user_saved_item_key_length CHECK (char_length(item_key) BETWEEN 1 AND 200),
  CONSTRAINT user_saved_item_path_shape CHECK (path ~ '^/[A-Za-z0-9/_.$-]{0,199}$' AND path !~ '//'),
  CONSTRAINT user_saved_item_query_shape CHECK (query ~ '^(\?[A-Za-z0-9_.%=&:,+~-]{0,254})?$')
);
CREATE INDEX user_saved_item_latest ON fyniq.user_saved_item (organisation_id, user_id, kind, item_key, set_at);

-- Append-only, as the preferences and the help state are (FY189): a new row is the new state.
CREATE TRIGGER user_saved_item_guard BEFORE UPDATE OR DELETE ON fyniq.user_saved_item
  FOR EACH ROW EXECUTE FUNCTION fyniq.notification_append_only();
CREATE TRIGGER user_saved_item_no_truncate BEFORE TRUNCATE ON fyniq.user_saved_item
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.user_saved_item
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('user_saved_item', 'id');

ALTER TABLE fyniq.user_saved_item ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.user_saved_item FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.user_saved_item
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

-- A person saves their own through the API, which saves only the caller's; the worker reads them for the tenant export.
GRANT SELECT ON fyniq.user_saved_item TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (user_id, kind, item_key, state, title, path, query, set_at) ON fyniq.user_saved_item TO fyniq_api;
