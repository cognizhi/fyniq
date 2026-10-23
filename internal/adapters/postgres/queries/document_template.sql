-- name: InsertDocumentTemplate :one
INSERT INTO fyniq.document_template (kind, document_type, name, draft, saved_by, saved_at)
VALUES (sqlc.arg(kind), sqlc.arg(document_type), sqlc.arg(name), sqlc.arg(draft), sqlc.arg(saved_by), sqlc.arg(saved_at))
RETURNING id, kind, document_type, name, draft, version, coalesce(published, 0)::integer AS published, saved_by, saved_at, created_at;

-- name: GetDocumentTemplate :one
SELECT id, kind, document_type, name, draft, version, coalesce(published, 0)::integer AS published, saved_by, saved_at, created_at
FROM fyniq.document_template WHERE id = sqlc.arg(id);

-- name: LockDocumentTemplate :one
SELECT id, kind, document_type, name, draft, version, coalesce(published, 0)::integer AS published, saved_by, saved_at, created_at
FROM fyniq.document_template WHERE id = sqlc.arg(id) FOR UPDATE;

-- name: PageDocumentTemplates :many
-- By ID, which is time-ordered: the order templates were made (FR-INT-10's cursor needs a stable key).
SELECT id, kind, document_type, name, draft, version, coalesce(published, 0)::integer AS published, saved_by, saved_at, created_at
FROM fyniq.document_template
WHERE id > sqlc.arg(after)::uuid
  AND (sqlc.narg(kind)::text IS NULL OR kind = sqlc.narg(kind)::text)
  AND (sqlc.narg(document_type)::text IS NULL OR document_type = sqlc.narg(document_type)::text)
ORDER BY id LIMIT sqlc.arg(page_size);

-- name: SaveDocumentTemplateDraft :one
UPDATE fyniq.document_template SET name = sqlc.arg(name), draft = sqlc.arg(draft), version = version + 1,
  saved_by = sqlc.arg(saved_by), saved_at = sqlc.arg(saved_at)
WHERE id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING id, kind, document_type, name, draft, version, coalesce(published, 0)::integer AS published, saved_by, saved_at, created_at;

-- name: InsertDocumentTemplateVersion :one
INSERT INTO fyniq.document_template_version (template_id, number, content, content_hash, note, republished_from, published_by, published_at)
VALUES (sqlc.arg(template_id), sqlc.arg(number), sqlc.arg(content), sqlc.arg(content_hash), sqlc.narg(note), sqlc.narg(republished_from),
  sqlc.arg(published_by), sqlc.arg(published_at))
RETURNING id, template_id, number, content, content_hash, coalesce(note, '')::text AS note, coalesce(republished_from, 0)::integer AS republished_from,
  published_by, published_at;

-- name: MarkDocumentTemplatePublished :one
UPDATE fyniq.document_template SET published = sqlc.arg(number), version = version + 1
WHERE id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING id, kind, document_type, name, draft, version, coalesce(published, 0)::integer AS published, saved_by, saved_at, created_at;

-- name: DocumentTemplateVersions :many
-- Newest first, below a number (0 for the latest): one page of a cursor list.
SELECT id, template_id, number, content, content_hash, coalesce(note, '')::text AS note, coalesce(republished_from, 0)::integer AS republished_from,
  published_by, published_at
FROM fyniq.document_template_version
WHERE template_id = sqlc.arg(template_id) AND (sqlc.arg(before)::integer = 0 OR number < sqlc.arg(before)::integer)
ORDER BY number DESC LIMIT sqlc.arg(page_size);

-- name: GetDocumentTemplateVersion :one
SELECT id, template_id, number, content, content_hash, coalesce(note, '')::text AS note, coalesce(republished_from, 0)::integer AS republished_from,
  published_by, published_at
FROM fyniq.document_template_version WHERE template_id = sqlc.arg(template_id) AND number = sqlc.arg(number);

-- name: InsertDocumentTemplateDefault :one
INSERT INTO fyniq.document_template_default (kind, document_type, template_id, set_by, set_at)
VALUES (sqlc.arg(kind), sqlc.arg(document_type), sqlc.narg(template_id), sqlc.arg(set_by), sqlc.arg(set_at))
RETURNING id, kind, document_type, template_id, set_by, set_at;

-- name: DocumentTemplateDefaults :many
-- The latest choice of each kind and document type: the one in force.
SELECT DISTINCT ON (kind, document_type) id, kind, document_type, template_id, set_by, set_at
FROM fyniq.document_template_default
ORDER BY kind, document_type, set_at DESC, id DESC;

-- name: InsertContactDocumentTemplate :one
INSERT INTO fyniq.contact_document_template (contact_id, kind, document_type, template_id, set_by, set_at)
VALUES (sqlc.arg(contact_id), sqlc.arg(kind), sqlc.arg(document_type), sqlc.narg(template_id), sqlc.arg(set_by), sqlc.arg(set_at))
RETURNING id, contact_id, kind, document_type, template_id, set_by, set_at;

-- name: ContactDocumentTemplates :many
-- The latest choice of each kind and document type for a contact: the one in force.
SELECT DISTINCT ON (kind, document_type) id, contact_id, kind, document_type, template_id, set_by, set_at
FROM fyniq.contact_document_template
WHERE contact_id = sqlc.arg(contact_id)
ORDER BY kind, document_type, set_at DESC, id DESC;
