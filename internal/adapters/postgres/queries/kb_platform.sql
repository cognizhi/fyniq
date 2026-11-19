-- name: KBPlatformDigest :one
-- The loaded release's version and digest, to skip loading the same release again.
SELECT version, digest FROM fyniq.kb_platform_release;

-- name: KBPlatformLoad :exec
-- A release's set replaces the last one's whole, in this one call (migration 000560): the migrator alone may run it.
SELECT fyniq.kb_platform_load(sqlc.arg(release)::jsonb);

-- name: KBPlatformInfo :one
SELECT d.version::text AS version, d.schema::text AS schema, d.digest::text AS digest, d.loaded_at::timestamptz AS loaded_at FROM fyniq.kb_platform_info() AS d(version, schema, digest, loaded_at);

-- name: KBPlatformIndex :many
SELECT d.key::text AS key, d.type::text AS type, d.title::text AS title, d.summary::text AS summary, d.status::text AS status, d.data_class::text AS data_class FROM fyniq.kb_platform_index(sqlc.arg(roles)::text[], sqlc.arg(classes)::text[]) AS d(key, type, title, summary, status, data_class);

-- name: KBPlatformPageGet :one
SELECT d.key::text AS key, d.type::text AS type, d.title::text AS title, d.summary::text AS summary, d.aliases::text[] AS aliases, d.body::text AS body, d.sources::jsonb AS sources, d.data_class::text AS data_class, d.taint::text AS taint, d.status::text AS status, d.compiled_by::text AS compiled_by, d.compiled_at::date AS compiled_at, d.digest::text AS digest, d.release::text AS release FROM fyniq.kb_platform_page_get(sqlc.arg(key)::text, sqlc.arg(roles)::text[], sqlc.arg(classes)::text[]) AS d(key, type, title, summary, aliases, body, sources, data_class, taint, status, compiled_by, compiled_at, digest, release);

-- name: KBPlatformPageGuides :many
SELECT d.file::text AS file, d.key::text AS key, d.title::text AS title, d.draft::boolean AS draft FROM fyniq.kb_platform_page_guides(sqlc.arg(key)::text, sqlc.arg(roles)::text[], sqlc.arg(classes)::text[]) AS d(file, key, title, draft);

-- name: KBPlatformSearch :many
SELECT d.kind::text AS kind, d.key::text AS key, d.title::text AS title, d.summary::text AS summary, d.snippet::text AS snippet, d.rank::real AS rank, d.draft::boolean AS draft FROM fyniq.kb_platform_search(sqlc.arg(terms)::text[], sqlc.arg(roles)::text[], sqlc.arg(classes)::text[], sqlc.arg(scope)::text,
                                sqlc.arg(max_hits)::integer) AS d(kind, key, title, summary, snippet, rank, draft);
