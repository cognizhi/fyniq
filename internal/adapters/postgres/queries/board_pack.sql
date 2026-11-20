-- name: GetBoardPack :one
SELECT * FROM fyniq.board_pack WHERE id = sqlc.arg(id);

-- name: ListBoardPacks :many
SELECT * FROM fyniq.board_pack WHERE NOT sqlc.arg(published_only)::boolean OR status = 'published' ORDER BY month DESC, version DESC;

-- name: LatestBoardPack :one
SELECT * FROM fyniq.board_pack WHERE month = sqlc.arg(month) ORDER BY version DESC LIMIT 1;

-- name: InsertBoardPackDraft :one
INSERT INTO fyniq.board_pack (month, version, supersedes, created_by, created_at, updated_by, updated_at)
VALUES (sqlc.arg(month), sqlc.arg(version), sqlc.narg(supersedes), sqlc.arg(by), sqlc.arg(at), sqlc.arg(by), sqlc.arg(at))
RETURNING *;

-- name: EditBoardPackDraft :one
UPDATE fyniq.board_pack SET highlights = sqlc.arg(highlights), commentary = sqlc.arg(commentary), asks = sqlc.arg(asks),
  updated_by = sqlc.arg(by), updated_at = sqlc.arg(at), edit = edit + 1
WHERE id = sqlc.arg(id) AND edit = sqlc.arg(edit) AND status = 'draft'
RETURNING *;

-- name: PublishBoardPack :one
UPDATE fyniq.board_pack SET status = 'published', figures = sqlc.arg(figures), ledger_revision = sqlc.arg(revision), published_by = sqlc.arg(by),
  published_at = sqlc.arg(at), updated_by = sqlc.arg(by), updated_at = sqlc.arg(at), edit = edit + 1
WHERE id = sqlc.arg(id) AND edit = sqlc.arg(edit) AND status = 'draft'
RETURNING *;

-- name: GetInvestorUpdate :one
SELECT * FROM fyniq.investor_update WHERE id = sqlc.arg(id);

-- name: ListInvestorUpdates :many
SELECT * FROM fyniq.investor_update WHERE NOT sqlc.arg(sent_only)::boolean OR status = 'sent' ORDER BY created_at DESC, id DESC;

-- name: InsertInvestorUpdate :one
INSERT INTO fyniq.investor_update (pack_id, kind, title, created_by, created_at, updated_by, updated_at)
VALUES (sqlc.arg(pack_id), sqlc.arg(kind), sqlc.arg(title), sqlc.arg(by), sqlc.arg(at), sqlc.arg(by), sqlc.arg(at))
RETURNING *;

-- name: EditInvestorUpdate :one
UPDATE fyniq.investor_update SET title = sqlc.arg(title), highlights = sqlc.arg(highlights), asks = sqlc.arg(asks), updated_by = sqlc.arg(by),
  updated_at = sqlc.arg(at), edit = edit + 1
WHERE id = sqlc.arg(id) AND edit = sqlc.arg(edit) AND status = 'draft'
RETURNING *;

-- name: SendInvestorUpdate :one
UPDATE fyniq.investor_update SET status = 'sent', recipients = sqlc.arg(recipients)::uuid[], sent_by = sqlc.arg(by), sent_at = sqlc.arg(at),
  updated_by = sqlc.arg(by), updated_at = sqlc.arg(at), edit = edit + 1
WHERE id = sqlc.arg(id) AND edit = sqlc.arg(edit) AND status = 'draft'
RETURNING *;

-- name: InsertInvestorUpdateRead :one
INSERT INTO fyniq.investor_update_read (update_id, user_id, read_at) VALUES (sqlc.arg(update_id), sqlc.arg(user_id), sqlc.arg(at))
ON CONFLICT (organisation_id, update_id, user_id) DO NOTHING
RETURNING id;

-- name: InvestorUpdateReads :many
SELECT * FROM fyniq.investor_update_read WHERE update_id = sqlc.arg(update_id) ORDER BY read_at, id;
