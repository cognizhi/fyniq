-- The organisation's knowledge base (PRD-001 §9.11, AI-50, AI-54, AI-56, AI-58; p1-knowledge-base task 4.1, design D5).
--
-- kb_note is what an Owner or Administrator writes: a note, a policy or a decision, the source an organisation page is
-- compiled from. It is a mutable record with a version counter, audited like any record.
--
-- kb_page_version is each version of an organisation page: append-only, one row per version, kept as it was made. Origins:
-- `rendered` from settings with no model, `compiled` by the compiler as a draft, `human_edited` by a person (which a
-- recompile never overwrites), `overlay` (a person's edit of a platform page, for this organisation only) and `rollback` (a new
-- version restoring an old one's content). A version is published when the newest snapshot lists it, and a draft when it is
-- newer than the published one: there is no status column to forget to change.
--
-- kb_snapshot and kb_snapshot_page are the numbered snapshots: append-only, each the full list of what is published, key and
-- version. The live knowledge base is the newest. A publish makes the next one, naming the approval that signed it.
--
-- kb_lint_finding is what the lint and the weekly semantic pass found: open until a person resolves it, once.
--
-- Every table has the organisation, FORCE row-level security and an audit event for each change (AUD-01). The search is
-- PostgreSQL full-text search over the published versions; there is no vector column and there never will be (AI-58).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.kb_note (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  key             text        NOT NULL,
  kind            text        NOT NULL,
  title           text        NOT NULL,
  body            text        NOT NULL,
  data_class      text        NOT NULL DEFAULT 'general',
  version         integer     NOT NULL DEFAULT 1,
  created_by      uuid        NOT NULL,
  created_at      timestamptz NOT NULL,
  updated_by      uuid        NOT NULL,
  updated_at      timestamptz NOT NULL,
  CONSTRAINT kb_note_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT kb_note_key UNIQUE (organisation_id, key),
  CONSTRAINT kb_note_created_by_fkey FOREIGN KEY (created_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT kb_note_updated_by_fkey FOREIGN KEY (updated_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT kb_note_key_format CHECK (key ~ '^[a-z0-9][a-z0-9.-]{0,99}$'),
  CONSTRAINT kb_note_kind CHECK (kind IN ('note', 'policy', 'decision')),
  CONSTRAINT kb_note_text CHECK (length(btrim(title)) BETWEEN 1 AND 200 AND length(body) BETWEEN 1 AND 20000),
  CONSTRAINT kb_note_class CHECK (data_class IN ('general', 'personal', 'payroll', 'equity', 'identity')),
  CONSTRAINT kb_note_version CHECK (version >= 1),
  CONSTRAINT kb_note_updated CHECK (updated_at >= created_at)
);

CREATE TABLE fyniq.kb_page_version (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  key             text        NOT NULL,
  number          integer     NOT NULL,
  type            text        NOT NULL,
  title           text        NOT NULL,
  summary         text        NOT NULL,
  aliases         text[]      NOT NULL DEFAULT '{}',
  body            text        NOT NULL,
  sources         jsonb       NOT NULL DEFAULT '[]', -- [{kind, ref, sha256}]: a note, a setting, or a platform page for an overlay
  data_class      text        NOT NULL DEFAULT 'general',
  taint           text        NOT NULL DEFAULT 'trusted',
  origin          text        NOT NULL,
  human_edited    boolean     NOT NULL DEFAULT false,
  base_key        text,                               -- an overlay: the platform page it replaces for this organisation
  base_digest     text,                               -- and the digest of that page's file when the overlay was made
  restores        integer,                            -- a rollback: the version whose content it restores
  compiler        text        NOT NULL DEFAULT '',    -- the model that compiled it, when one did
  prompt_version  text        NOT NULL DEFAULT '',
  author_kind     text        NOT NULL,
  author_user     uuid,
  created_at      timestamptz NOT NULL,
  search          tsvector    NOT NULL,
  CONSTRAINT kb_page_version_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT kb_page_version_number UNIQUE (organisation_id, key, number),
  CONSTRAINT kb_page_version_author_fkey FOREIGN KEY (author_user) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT kb_page_version_key_format CHECK (key ~ '^[a-z0-9][a-z0-9.-]{0,99}$'),
  CONSTRAINT kb_page_version_number_positive CHECK (number >= 1),
  CONSTRAINT kb_page_version_type CHECK (type IN ('concept', 'task', 'reference', 'policy', 'decision')),
  CONSTRAINT kb_page_version_text CHECK (length(btrim(title)) BETWEEN 1 AND 200 AND length(summary) BETWEEN 1 AND 160
    AND length(body) BETWEEN 1 AND 60000),
  CONSTRAINT kb_page_version_class CHECK (data_class IN ('general', 'personal', 'payroll', 'equity', 'identity')),
  CONSTRAINT kb_page_version_taint CHECK (taint IN ('trusted', 'untrusted')),
  CONSTRAINT kb_page_version_origin CHECK (origin IN ('rendered', 'compiled', 'human_edited', 'overlay', 'rollback')),
  CONSTRAINT kb_page_version_author CHECK (author_kind IN ('person', 'compiler', 'system')
    AND (author_kind = 'person') = (author_user IS NOT NULL)),
  -- Who may write which origin (AI-56, AI-57): the compiler writes compiled drafts, the platform renders, people edit.
  CONSTRAINT kb_page_version_origin_author CHECK (
    (origin = 'compiled' AND author_kind = 'compiler') OR (origin = 'rendered' AND author_kind = 'system')
    OR (origin IN ('human_edited', 'overlay', 'rollback') AND author_kind = 'person')),
  CONSTRAINT kb_page_version_human CHECK (human_edited = (origin IN ('human_edited', 'overlay', 'rollback'))),
  CONSTRAINT kb_page_version_overlay CHECK ((origin = 'overlay') = (base_key IS NOT NULL AND base_digest IS NOT NULL)
    AND (base_digest IS NULL OR base_digest ~ '^[0-9a-f]{64}$')),
  CONSTRAINT kb_page_version_rollback CHECK ((origin = 'rollback') = (restores IS NOT NULL) AND (restores IS NULL OR restores < number)),
  -- An organisation's own page is org.<name>; only an overlay takes a platform page's key.
  CONSTRAINT kb_page_version_org_key CHECK (origin IN ('overlay', 'rollback') OR key LIKE 'org.%'),
  -- AI-54: an untrusted page is never made live without a person's approval, and P1 compiles trusted sources only.
  CONSTRAINT kb_page_version_trusted CHECK (taint = 'trusted'),
  CONSTRAINT kb_page_version_sources CHECK (jsonb_typeof(sources) = 'array' AND jsonb_array_length(sources) <= 50),
  CONSTRAINT kb_page_version_alias_count CHECK (cardinality(aliases) <= 30)
);
-- organisation_id leads every tenant index (§11.4); btree_gin lets it lead a full-text one.
CREATE INDEX kb_page_version_search ON fyniq.kb_page_version USING gin (organisation_id, search);

CREATE TABLE fyniq.kb_snapshot (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  number          integer     NOT NULL,
  approval_id     uuid,                          -- the approval request that signed the publish; none for a rendered change
  created_by      uuid,
  reason          text        NOT NULL DEFAULT '',
  created_at      timestamptz NOT NULL,
  CONSTRAINT kb_snapshot_pkey PRIMARY KEY (organisation_id, number),
  CONSTRAINT kb_snapshot_id UNIQUE (organisation_id, id),
  CONSTRAINT kb_snapshot_number CHECK (number >= 1),
  CONSTRAINT kb_snapshot_approval_fkey FOREIGN KEY (organisation_id, approval_id) REFERENCES fyniq.approval_request (organisation_id, id),
  CONSTRAINT kb_snapshot_created_by_fkey FOREIGN KEY (created_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT kb_snapshot_reason CHECK (length(reason) <= 1000)
);

CREATE TABLE fyniq.kb_snapshot_page (
  organisation_id uuid    NOT NULL DEFAULT fyniq.current_org(),
  id              uuid    NOT NULL DEFAULT uuidv7(),
  snapshot_number integer NOT NULL,
  key             text    NOT NULL,
  version         integer NOT NULL,
  CONSTRAINT kb_snapshot_page_pkey PRIMARY KEY (organisation_id, snapshot_number, key),
  CONSTRAINT kb_snapshot_page_id UNIQUE (organisation_id, id),
  CONSTRAINT kb_snapshot_page_snapshot_fkey FOREIGN KEY (organisation_id, snapshot_number)
    REFERENCES fyniq.kb_snapshot (organisation_id, number),
  CONSTRAINT kb_snapshot_page_version_fkey FOREIGN KEY (organisation_id, key, version)
    REFERENCES fyniq.kb_page_version (organisation_id, key, number)
);

CREATE TABLE fyniq.kb_lint_finding (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  kind            text        NOT NULL,
  page_key        text,
  page_version    integer,
  detail          text        NOT NULL,
  status          text        NOT NULL DEFAULT 'open',
  created_at      timestamptz NOT NULL,
  resolved_by     uuid,
  resolved_at     timestamptz,
  resolution      text        NOT NULL DEFAULT '',
  CONSTRAINT kb_lint_finding_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT kb_lint_finding_resolver_fkey FOREIGN KEY (resolved_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT kb_lint_finding_kind CHECK (kind IN ('stale', 'contradiction', 'number_not_in_source', 'source_without_page',
    'orphan', 'human_edit_conflict', 'superseded', 'missing_cross_reference', 'citation', 'front_matter')),
  CONSTRAINT kb_lint_finding_text CHECK (length(btrim(detail)) BETWEEN 1 AND 1000 AND length(resolution) <= 1000),
  CONSTRAINT kb_lint_finding_state CHECK (status IN ('open', 'resolved')
    AND (status = 'resolved') = (resolved_at IS NOT NULL) AND (status = 'resolved') = (resolved_by IS NOT NULL)),
  CONSTRAINT kb_lint_finding_version CHECK (page_version IS NULL OR (page_key IS NOT NULL AND page_version >= 1))
);
CREATE INDEX kb_lint_finding_open ON fyniq.kb_lint_finding (organisation_id, status, created_at);

-- A version, a snapshot and its pages are kept as they were made, and never removed. A note changes only forward, by a
-- later version of its body (the counter must go up by one); a finding is resolved once.
-- +goose StatementBegin
CREATE FUNCTION fyniq.kb_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'The organisation''s knowledge base keeps what it recorded: % is never removed', TG_TABLE_NAME USING ERRCODE = 'FY561';
  END IF;
  CASE TG_TABLE_NAME
  WHEN 'kb_note' THEN
    IF (NEW.organisation_id, NEW.id, NEW.key, NEW.created_by, NEW.created_at)
         IS DISTINCT FROM (OLD.organisation_id, OLD.id, OLD.key, OLD.created_by, OLD.created_at)
       OR NEW.version <> OLD.version + 1 THEN
      RAISE EXCEPTION 'A note changes only by its next version' USING ERRCODE = 'FY561';
    END IF;
  WHEN 'kb_lint_finding' THEN
    IF (NEW.organisation_id, NEW.id, NEW.kind, NEW.page_key, NEW.page_version, NEW.detail, NEW.created_at)
         IS DISTINCT FROM (OLD.organisation_id, OLD.id, OLD.kind, OLD.page_key, OLD.page_version, OLD.detail, OLD.created_at)
       OR OLD.status <> 'open' THEN
      RAISE EXCEPTION 'A finding is only resolved, once' USING ERRCODE = 'FY561';
    END IF;
  ELSE
    RAISE EXCEPTION 'The organisation''s knowledge base keeps what it recorded: % is never changed', TG_TABLE_NAME USING ERRCODE = 'FY561';
  END CASE;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

CREATE TRIGGER kb_note_guard BEFORE UPDATE OR DELETE ON fyniq.kb_note FOR EACH ROW EXECUTE FUNCTION fyniq.kb_guard();
CREATE TRIGGER kb_page_version_guard BEFORE UPDATE OR DELETE ON fyniq.kb_page_version FOR EACH ROW EXECUTE FUNCTION fyniq.kb_guard();
CREATE TRIGGER kb_snapshot_guard BEFORE UPDATE OR DELETE ON fyniq.kb_snapshot FOR EACH ROW EXECUTE FUNCTION fyniq.kb_guard();
CREATE TRIGGER kb_snapshot_page_guard BEFORE UPDATE OR DELETE ON fyniq.kb_snapshot_page FOR EACH ROW EXECUTE FUNCTION fyniq.kb_guard();
CREATE TRIGGER kb_lint_finding_guard BEFORE UPDATE OR DELETE ON fyniq.kb_lint_finding FOR EACH ROW EXECUTE FUNCTION fyniq.kb_guard();
CREATE TRIGGER kb_note_no_truncate BEFORE TRUNCATE ON fyniq.kb_note FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE TRIGGER kb_page_version_no_truncate BEFORE TRUNCATE ON fyniq.kb_page_version FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE TRIGGER kb_snapshot_no_truncate BEFORE TRUNCATE ON fyniq.kb_snapshot FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE TRIGGER kb_snapshot_page_no_truncate BEFORE TRUNCATE ON fyniq.kb_snapshot_page FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE TRIGGER kb_lint_finding_no_truncate BEFORE TRUNCATE ON fyniq.kb_lint_finding FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();

-- Every saved change is audited in its transaction (AUD-01). A snapshot's pages are audited with the snapshot, which names each page that moved.
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.kb_note
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('kb_note', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.kb_snapshot
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('kb_snapshot', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.kb_page_version
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('kb_page_version', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.kb_lint_finding
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('kb_lint_finding', 'id');

ALTER TABLE fyniq.kb_note ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.kb_note FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.kb_note
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.kb_page_version ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.kb_page_version FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.kb_page_version
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.kb_snapshot ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.kb_snapshot FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.kb_snapshot
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.kb_snapshot_page ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.kb_snapshot_page FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.kb_snapshot_page
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.kb_lint_finding ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.kb_lint_finding FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.kb_lint_finding
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

REVOKE ALL ON fyniq.kb_note, fyniq.kb_page_version, fyniq.kb_snapshot, fyniq.kb_snapshot_page, fyniq.kb_lint_finding FROM PUBLIC;
GRANT SELECT ON fyniq.kb_note, fyniq.kb_page_version, fyniq.kb_snapshot, fyniq.kb_snapshot_page, fyniq.kb_lint_finding
  TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (key, kind, title, body, data_class, created_by, created_at, updated_by, updated_at),
      UPDATE (kind, title, body, data_class, version, updated_by, updated_at) ON fyniq.kb_note TO fyniq_api;
GRANT INSERT (key, number, type, title, summary, aliases, body, sources, data_class, taint, origin, human_edited, base_key,
              base_digest, restores, compiler, prompt_version, author_kind, author_user, created_at, search)
  ON fyniq.kb_page_version TO fyniq_api;
GRANT INSERT (number, approval_id, created_by, reason, created_at) ON fyniq.kb_snapshot TO fyniq_api;
GRANT INSERT (snapshot_number, key, version) ON fyniq.kb_snapshot_page TO fyniq_api;
GRANT INSERT (kind, page_key, page_version, detail, created_at), UPDATE (status, resolved_by, resolved_at, resolution)
  ON fyniq.kb_lint_finding TO fyniq_api;

-- ------------------------------------------------------------------ the live knowledge base
-- What is published: the pages of the newest snapshot. A view that runs as its caller, so each organisation's row-level
-- security applies; the services read the organisation's pages only through it and the functions below.
CREATE VIEW fyniq.kb_live_page WITH (security_invoker = true) AS
  SELECT v.organisation_id, v.key, v.number, v.type, v.title, v.summary, v.aliases, v.body, v.sources, v.data_class, v.taint,
         v.origin, v.human_edited, v.base_key, v.base_digest, v.created_at, v.search, s.number AS snapshot_number
    FROM fyniq.kb_snapshot s
    JOIN fyniq.kb_snapshot_page sp ON sp.organisation_id = s.organisation_id AND sp.snapshot_number = s.number
    JOIN fyniq.kb_page_version v ON v.organisation_id = sp.organisation_id AND v.key = sp.key AND v.number = sp.version
   WHERE s.number = (SELECT max(number) FROM fyniq.kb_snapshot);
GRANT SELECT ON fyniq.kb_live_page TO fyniq_api, fyniq_worker, fyniq_readonly;

-- The reader's search of the organisation's published pages (AI-58): the terms ORed, ranked by ts_rank_cd over the weighted
-- text, with trigram similarity of the title and aliases; only the data classes the reader may read (AI-54). SECURITY DEFINER so
-- that the extension's similarity runs; the tables are FORCE row-level security, so the organisation is still the caller's.
-- +goose StatementBegin
CREATE FUNCTION fyniq.kb_org_search(p_terms text[], p_classes text[], p_limit integer)
RETURNS TABLE (key text, title text, summary text, snippet text, rank real)
LANGUAGE plpgsql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  q tsquery;
  phrase text := lower(array_to_string(p_terms, ' '));
BEGIN
  IF p_limit < 1 OR p_limit > 50 THEN
    RAISE EXCEPTION 'a search returns between 1 and 50 hits' USING ERRCODE = 'FY561';
  END IF;
  SELECT string_agg(plainto_tsquery('simple', t)::text, ' | ')::tsquery INTO q
    FROM unnest(p_terms) t WHERE plainto_tsquery('simple', t)::text <> '';
  IF q IS NULL THEN
    RETURN;
  END IF;
  RETURN QUERY
  SELECT l.key, l.title, l.summary, ts_headline('simple', l.body, q, 'MaxFragments=1, MaxWords=30, MinWords=10'),
         (ts_rank_cd(l.search, q) + 0.5 * greatest(fyniq_ext.similarity(lower(l.title), phrase),
                                                    fyniq_ext.similarity(lower(array_to_string(l.aliases, ' ')), phrase)))::real
    FROM fyniq.kb_live_page l
   WHERE l.data_class = ANY (p_classes)
     AND (l.search @@ q OR fyniq_ext.similarity(lower(l.title), phrase) > 0.3
          OR fyniq_ext.similarity(lower(array_to_string(l.aliases, ' ')), phrase) > 0.3)
   ORDER BY 5 DESC, l.key
   LIMIT p_limit;
END
$$;
-- +goose StatementEnd
GRANT EXECUTE ON FUNCTION fyniq.kb_org_search(text[], text[], integer) TO fyniq_api;
