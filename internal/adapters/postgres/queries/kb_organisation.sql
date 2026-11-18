-- name: KBNoteByKey :one
SELECT id, key, kind, title, body, data_class, version, created_by, created_at, updated_by, updated_at
FROM fyniq.kb_note WHERE key = sqlc.arg(key);

-- name: KBNotes :many
SELECT id, key, kind, title, body, data_class, version, created_by, created_at, updated_by, updated_at
FROM fyniq.kb_note ORDER BY key;

-- name: InsertKBNote :one
INSERT INTO fyniq.kb_note (key, kind, title, body, data_class, created_by, created_at, updated_by, updated_at)
VALUES (sqlc.arg(key), sqlc.arg(kind), sqlc.arg(title), sqlc.arg(body), sqlc.arg(data_class), sqlc.arg(by), sqlc.arg(at), sqlc.arg(by), sqlc.arg(at))
RETURNING id, version;

-- name: UpdateKBNote :execrows
UPDATE fyniq.kb_note SET kind = sqlc.arg(kind), title = sqlc.arg(title), body = sqlc.arg(body), data_class = sqlc.arg(data_class),
  version = version + 1, updated_by = sqlc.arg(by), updated_at = sqlc.arg(at)
WHERE key = sqlc.arg(key) AND version = sqlc.arg(version);

-- name: InsertKBPageVersion :one
INSERT INTO fyniq.kb_page_version (key, number, type, title, summary, aliases, body, sources, data_class, taint, origin, human_edited,
  base_key, base_digest, restores, compiler, prompt_version, author_kind, author_user, created_at, search)
SELECT sqlc.arg(key)::text, coalesce(max(number), 0) + 1, sqlc.arg(type), sqlc.arg(title)::text, sqlc.arg(summary), sqlc.arg(aliases)::text[],
  sqlc.arg(body), sqlc.arg(sources)::jsonb, sqlc.arg(data_class), 'trusted', sqlc.arg(origin), sqlc.arg(human_edited),
  sqlc.narg(base_key), sqlc.narg(base_digest), sqlc.narg(restores)::integer, sqlc.arg(compiler), sqlc.arg(prompt_version), sqlc.arg(author_kind),
  sqlc.narg(author_user), sqlc.arg(at),
  setweight(to_tsvector('simple', sqlc.arg(key)::text || ' ' || sqlc.arg(title)::text || ' ' || array_to_string(sqlc.arg(aliases)::text[], ' ')), 'A')
  || setweight(to_tsvector('simple', sqlc.arg(summary)::text), 'B') || setweight(to_tsvector('simple', sqlc.arg(search_body)::text), 'C')
FROM fyniq.kb_page_version WHERE key = sqlc.arg(key)::text
RETURNING id, number;

-- name: KBPageVersions :many
SELECT id, key, number, type, title, summary, aliases, body, sources, data_class, taint, origin, human_edited, base_key, base_digest, restores,
  compiler, prompt_version, author_kind, author_user, created_at
FROM fyniq.kb_page_version WHERE key = sqlc.arg(key)::text ORDER BY number DESC;

-- name: KBLatestPageVersions :many
SELECT DISTINCT ON (key) id, key, number, type, title, summary, aliases, body, sources, data_class, taint, origin, human_edited, base_key, base_digest,
  restores, compiler, prompt_version, author_kind, author_user, created_at
FROM fyniq.kb_page_version ORDER BY key, number DESC;

-- name: KBLiveVersions :many
SELECT key, version FROM fyniq.kb_snapshot_page WHERE snapshot_number = (SELECT max(number) FROM fyniq.kb_snapshot) ORDER BY key;

-- name: InsertKBSnapshot :one
INSERT INTO fyniq.kb_snapshot (number, approval_id, created_by, reason, created_at)
SELECT coalesce(max(number), 0) + 1, sqlc.narg(approval_id)::uuid, sqlc.narg(created_by)::uuid, sqlc.arg(reason), sqlc.arg(at) FROM fyniq.kb_snapshot
RETURNING id, number;

-- name: InsertKBSnapshotPage :exec
INSERT INTO fyniq.kb_snapshot_page (snapshot_number, key, version) VALUES (sqlc.arg(snapshot_number), sqlc.arg(key), sqlc.arg(version));

-- name: KBSnapshots :many
SELECT id, number, approval_id, created_by, reason, created_at FROM fyniq.kb_snapshot ORDER BY number DESC LIMIT sqlc.arg(max_rows);

-- name: KBLiveIndex :many
SELECT key, type, title, summary, data_class, origin, number, base_key, snapshot_number
FROM fyniq.kb_live_page WHERE data_class = ANY(sqlc.arg(classes)::text[]) ORDER BY array_position(ARRAY['concept', 'task', 'reference', 'policy', 'decision'], type), key;

-- name: KBLivePage :one
SELECT key, type, title, summary, aliases, body, sources, data_class, origin, number, base_key, base_digest, created_at, snapshot_number
FROM fyniq.kb_live_page WHERE key = sqlc.arg(key)::text AND data_class = ANY(sqlc.arg(classes)::text[]);

-- name: KBOrgSearch :many
SELECT d.key::text AS key, d.title::text AS title, d.summary::text AS summary, d.snippet::text AS snippet, d.rank::real AS rank
FROM fyniq.kb_org_search(sqlc.arg(terms)::text[], sqlc.arg(classes)::text[], sqlc.arg(max_hits)::integer) AS d(key, title, summary, snippet, rank);
