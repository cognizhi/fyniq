-- The platform knowledge base at runtime (PRD-001 §9.11, AI-50, AI-51, AI-54, AI-56, AI-58; p1-knowledge-base tasks 1.2 and
-- 1.3, design D1, D2, D3).
--
-- The compiled pages in kb/ and the guide pages they are compiled from ship in the release. The migrator copies them here
-- when it deploys (`migrate up`), as it copies the statutory packs (000033): the release is the audited write path
-- (CTL-CHG-01). The tables are global (§11.4 "Global tables"): no organisation_id, and no grant to any application role.
-- Unlike a pack version, a release's set is replaced whole by the next release's, in one transaction.
--
-- The services read them only through the functions below, which are the reader filter (AI-54): a page is open to a reader
-- when every guide page it was compiled from is open to one of the reader's roles (a page for `all`, one that names a role
-- of the reader, or any page for an Owner: the rule of the guide, FR-UX-32) and its data class is one the reader may read.
-- The filter is in the query, so a summary, a snippet, a count and the index never reveal a page the reader may not open.
--
-- Search is PostgreSQL full-text search over pages, their aliases and the raw guide, with trigram similarity for
-- misspellings (AI-58). There is no vector column and no vector index, and there never will be (AI-58, ADR-T6).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.kb_platform_release (
  singleton boolean     NOT NULL DEFAULT true,
  version   text        NOT NULL,
  schema    text        NOT NULL,
  digest    text        NOT NULL,
  loaded_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT kb_platform_release_pkey PRIMARY KEY (singleton),
  CONSTRAINT kb_platform_release_one CHECK (singleton),
  CONSTRAINT kb_platform_release_digest CHECK (digest ~ '^[0-9a-f]{64}$'),
  CONSTRAINT kb_platform_release_text CHECK (length(version) BETWEEN 1 AND 100 AND schema ~ '^kb-schema@[0-9]+$')
);

CREATE TABLE fyniq.kb_platform_page (
  key         text    NOT NULL,
  type        text    NOT NULL,
  title       text    NOT NULL,
  summary     text    NOT NULL,
  aliases     text[]  NOT NULL DEFAULT '{}',
  body        text    NOT NULL,
  sources     jsonb   NOT NULL,
  data_class  text    NOT NULL,
  taint       text    NOT NULL,
  status      text    NOT NULL,
  compiled_by text    NOT NULL,
  compiled_at date    NOT NULL,
  digest      text    NOT NULL, -- the page file's SHA-256: with the release, the version an answer used (AI-12, AI-51)
  search      tsvector NOT NULL, -- key, title and aliases (A), the summary (B), the body (C); no stemming, so the users' own words match
  CONSTRAINT kb_platform_page_pkey PRIMARY KEY (key),
  CONSTRAINT kb_platform_page_key_format CHECK (key ~ '^[a-z0-9][a-z0-9.-]{0,99}$'),
  CONSTRAINT kb_platform_page_type CHECK (type IN ('concept', 'task', 'reference', 'policy', 'decision')),
  CONSTRAINT kb_platform_page_text CHECK (length(title) BETWEEN 1 AND 200 AND length(summary) BETWEEN 1 AND 160),
  CONSTRAINT kb_platform_page_class CHECK (data_class IN ('general', 'personal', 'payroll', 'equity', 'identity')),
  CONSTRAINT kb_platform_page_taint CHECK (taint IN ('trusted', 'untrusted')),
  CONSTRAINT kb_platform_page_status CHECK (status IN ('draft', 'published')),
  CONSTRAINT kb_platform_page_sources CHECK (jsonb_typeof(sources) = 'array' AND jsonb_array_length(sources) > 0),
  CONSTRAINT kb_platform_page_digest CHECK (digest ~ '^[0-9a-f]{64}$'),
  -- AI-54: an untrusted page is never published without approval, and a release ships trusted pages only (P1).
  CONSTRAINT kb_platform_page_trusted CHECK (taint = 'trusted')
);
CREATE INDEX kb_platform_page_search ON fyniq.kb_platform_page USING gin (search);
CREATE INDEX kb_platform_page_title_trgm ON fyniq.kb_platform_page USING gin (lower(title) fyniq_ext.gin_trgm_ops);

-- A page's sources, each with the audience of the guide page it came from: the strictest of them is the page's (AI-54).
CREATE TABLE fyniq.kb_platform_page_source (
  page_key text    NOT NULL,
  file     text    NOT NULL,
  sha256   text    NOT NULL,
  roles    text[]  NOT NULL,
  draft    boolean NOT NULL,
  CONSTRAINT kb_platform_page_source_pkey PRIMARY KEY (page_key, file),
  CONSTRAINT kb_platform_page_source_page FOREIGN KEY (page_key) REFERENCES fyniq.kb_platform_page (key) ON DELETE CASCADE,
  CONSTRAINT kb_platform_page_source_sha CHECK (sha256 ~ '^[0-9a-f]{64}$'),
  CONSTRAINT kb_platform_page_source_roles CHECK (cardinality(roles) >= 1)
);

-- The guide's pages as the raw sources: what kb.search falls back to when a page is stale or missing (AI-51).
CREATE TABLE fyniq.kb_platform_source (
  file    text     NOT NULL,
  key     text     NOT NULL, -- the guide page's own key, or '' when it has none
  title   text     NOT NULL,
  roles   text[]   NOT NULL,
  status  text     NOT NULL,
  body    text     NOT NULL,
  search  tsvector NOT NULL,
  CONSTRAINT kb_platform_source_pkey PRIMARY KEY (file),
  CONSTRAINT kb_platform_source_file CHECK (file ~ '^docs/user-guide/[a-z0-9/_.-]+\.md$'),
  CONSTRAINT kb_platform_source_roles CHECK (cardinality(roles) >= 1),
  CONSTRAINT kb_platform_source_status CHECK (status IN ('draft', 'published'))
);
CREATE INDEX kb_platform_source_search ON fyniq.kb_platform_source USING gin (search);

REVOKE ALL ON fyniq.kb_platform_release, fyniq.kb_platform_page, fyniq.kb_platform_page_source,
  fyniq.kb_platform_source FROM PUBLIC;

-- ------------------------------------------------------------------ the load
-- The release's whole set, replaced in one call so that it is one transaction: the migrator runs it, and no other role is
-- granted it. The document: {version, schema, digest, guides: [{file, key, title, roles, status, body}], pages: [{key, type,
-- title, summary, aliases, body, search_body, sources: [{path, sha256}], data_class, taint, status, compiled_by,
-- compiled_at, digest}]}. The search vectors are computed here: key, title and aliases weigh most, then the summary, then
-- the text; no stemming ('simple'), so the users' own words match.

-- +goose StatementBegin
CREATE FUNCTION fyniq.kb_platform_load(p_release jsonb)
RETURNS void
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  DELETE FROM fyniq.kb_platform_source;
  DELETE FROM fyniq.kb_platform_page;
  DELETE FROM fyniq.kb_platform_release;
  INSERT INTO fyniq.kb_platform_release (version, schema, digest)
  VALUES (p_release->>'version', p_release->>'schema', p_release->>'digest');
  INSERT INTO fyniq.kb_platform_source (file, key, title, roles, status, body, search)
  SELECT g.file, g.key, g.title, g.roles, g.status, g.body,
         setweight(to_tsvector('simple', g.key || ' ' || g.title), 'A') || setweight(to_tsvector('simple', g.body), 'C')
    FROM jsonb_to_recordset(p_release->'guides')
         AS g(file text, key text, title text, roles text[], status text, body text);
  INSERT INTO fyniq.kb_platform_page (key, type, title, summary, aliases, body, sources, data_class, taint, status,
                                      compiled_by, compiled_at, digest, search)
  SELECT p.key, p.type, p.title, p.summary, coalesce(p.aliases, '{}'), p.body, p.sources, p.data_class, p.taint, p.status,
         p.compiled_by, p.compiled_at, p.digest,
         setweight(to_tsvector('simple', p.key || ' ' || p.title || ' ' || array_to_string(coalesce(p.aliases, '{}'), ' ')), 'A')
         || setweight(to_tsvector('simple', p.summary), 'B') || setweight(to_tsvector('simple', p.search_body), 'C')
    FROM jsonb_to_recordset(p_release->'pages')
         AS p(key text, type text, title text, summary text, aliases text[], body text, search_body text, sources jsonb,
              data_class text, taint text, status text, compiled_by text, compiled_at date, digest text);
  INSERT INTO fyniq.kb_platform_page_source (page_key, file, sha256, roles, draft)
  SELECT p.key, s.path, s.sha256, g.roles, g.status = 'draft'
    FROM jsonb_to_recordset(p_release->'pages') AS p(key text, sources jsonb),
         jsonb_to_recordset(p.sources) AS s(path text, sha256 text)
    JOIN fyniq.kb_platform_source g ON g.file = s.path;
END
$$;
-- +goose StatementEnd

-- ------------------------------------------------------------------ the reader filter and the reads
-- A reader is their roles, as the guide names them, and the data classes their rights let them read (the caller works both
-- out from the person's live rights). Every function is SECURITY DEFINER over the tables that no application role can read.

-- +goose StatementBegin
CREATE FUNCTION fyniq.kb_platform_info()
RETURNS TABLE (version text, schema text, digest text, loaded_at timestamptz)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$ SELECT r.version, r.schema, r.digest, r.loaded_at FROM fyniq.kb_platform_release r $$;
-- +goose StatementEnd

-- Whether a reader may open a page: its data class is within theirs and every source's audience admits them.
-- +goose StatementBegin
CREATE FUNCTION fyniq.kb_platform_readable(p_key text, p_roles text[], p_classes text[])
RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT EXISTS (SELECT 1 FROM fyniq.kb_platform_page p WHERE p.key = p_key AND p.data_class = ANY (p_classes))
     AND NOT EXISTS (
       SELECT 1 FROM fyniq.kb_platform_page_source s
        WHERE s.page_key = p_key
          AND NOT ('all' = ANY (s.roles) OR s.roles && p_roles OR 'owner' = ANY (p_roles)))
$$;
-- +goose StatementEnd

-- The reader's index: one row per page they may open.
-- +goose StatementBegin
CREATE FUNCTION fyniq.kb_platform_index(p_roles text[], p_classes text[])
RETURNS TABLE (key text, type text, title text, summary text, status text, data_class text)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT p.key, p.type, p.title, p.summary, p.status, p.data_class
    FROM fyniq.kb_platform_page p
   WHERE fyniq.kb_platform_readable(p.key, p_roles, p_classes)
   ORDER BY array_position(ARRAY['concept', 'task', 'reference', 'policy', 'decision'], p.type), p.key
$$;
-- +goose StatementEnd

-- One page, if the reader may open it: nothing at all otherwise, so a page they may not open looks like a page that is
-- not there.
-- +goose StatementBegin
CREATE FUNCTION fyniq.kb_platform_page_get(p_key text, p_roles text[], p_classes text[])
RETURNS TABLE (key text, type text, title text, summary text, aliases text[], body text, sources jsonb,
               data_class text, taint text, status text, compiled_by text, compiled_at date, digest text, release text)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT p.key, p.type, p.title, p.summary, p.aliases, p.body, p.sources, p.data_class, p.taint, p.status,
         p.compiled_by, p.compiled_at, p.digest, (SELECT r.version FROM fyniq.kb_platform_release r)
    FROM fyniq.kb_platform_page p
   WHERE p.key = p_key AND fyniq.kb_platform_readable(p.key, p_roles, p_classes)
$$;
-- +goose StatementEnd

-- The guide pages a page cites, for the citation chips (AI-51): what the reader can open, never the compiled page. Only for
-- a page the reader may open.
-- +goose StatementBegin
CREATE FUNCTION fyniq.kb_platform_page_guides(p_key text, p_roles text[], p_classes text[])
RETURNS TABLE (file text, key text, title text, draft boolean)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT s.file, g.key, g.title, s.draft
    FROM fyniq.kb_platform_page_source s
    JOIN fyniq.kb_platform_source g ON g.file = s.file
   WHERE s.page_key = p_key AND fyniq.kb_platform_readable(p_key, p_roles, p_classes)
   ORDER BY s.file
$$;
-- +goose StatementEnd

-- Search (AI-58): the terms are ORed, ranked by ts_rank_cd over the weighted text, with trigram similarity of the title and
-- the aliases to the whole phrase for misspellings. Scope: 'pages', 'sources' (the raw guide, the fallback) or 'both'. Each
-- hit is a page or a guide page the reader may open.
-- +goose StatementBegin
CREATE FUNCTION fyniq.kb_platform_search(p_terms text[], p_roles text[], p_classes text[], p_scope text, p_limit integer)
RETURNS TABLE (kind text, key text, title text, summary text, snippet text, rank real, draft boolean)
LANGUAGE plpgsql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  q tsquery;
  phrase text := lower(array_to_string(p_terms, ' '));
BEGIN
  IF p_scope NOT IN ('pages', 'sources', 'both') THEN
    RAISE EXCEPTION 'unknown search scope %', p_scope USING ERRCODE = 'FY560';
  END IF;
  IF p_limit < 1 OR p_limit > 50 THEN
    RAISE EXCEPTION 'a search returns between 1 and 50 hits' USING ERRCODE = 'FY560';
  END IF;
  SELECT string_agg(plainto_tsquery('simple', t)::text, ' | ')::tsquery
    INTO q
    FROM unnest(p_terms) t
   WHERE plainto_tsquery('simple', t)::text <> '';
  IF q IS NULL THEN
    RETURN;
  END IF;
  RETURN QUERY
  SELECT * FROM (
    SELECT 'page'::text AS kind, p.key, p.title, p.summary,
           ts_headline('simple', p.body, q, 'MaxFragments=1, MaxWords=30, MinWords=10') AS snippet,
           (ts_rank_cd(p.search, q) + 0.5 * greatest(fyniq_ext.similarity(lower(p.title), phrase),
                                                     fyniq_ext.similarity(lower(array_to_string(p.aliases, ' ')), phrase)))::real AS rank,
           p.status = 'draft' AS draft
      FROM fyniq.kb_platform_page p
     WHERE p_scope IN ('pages', 'both')
       AND (p.search @@ q OR fyniq_ext.similarity(lower(p.title), phrase) > 0.3
            OR fyniq_ext.similarity(lower(array_to_string(p.aliases, ' ')), phrase) > 0.3)
       AND fyniq.kb_platform_readable(p.key, p_roles, p_classes)
    UNION ALL
    SELECT 'source'::text, s.key, s.title, ''::text,
           ts_headline('simple', s.body, q, 'MaxFragments=1, MaxWords=30, MinWords=10'),
           ts_rank_cd(s.search, q)::real, s.status = 'draft'
      FROM fyniq.kb_platform_source s
     WHERE p_scope IN ('sources', 'both')
       AND s.search @@ q
       AND ('all' = ANY (s.roles) OR s.roles && p_roles OR 'owner' = ANY (p_roles))
  ) hits
  ORDER BY hits.rank DESC, hits.kind, hits.key
  LIMIT p_limit;
END
$$;
-- +goose StatementEnd

GRANT EXECUTE ON FUNCTION fyniq.kb_platform_info(), fyniq.kb_platform_readable(text, text[], text[]),
  fyniq.kb_platform_index(text[], text[]), fyniq.kb_platform_page_get(text, text[], text[]),
  fyniq.kb_platform_page_guides(text, text[], text[]), fyniq.kb_platform_search(text[], text[], text[], text, integer)
  TO fyniq_api;
