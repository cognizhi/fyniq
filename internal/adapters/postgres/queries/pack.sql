-- name: PackVersionDigest :one
-- The digest of a loaded version, to refuse loading it again with other contents (FY110).
SELECT digest FROM fyniq.statutory_pack_version WHERE id = sqlc.arg(id);

-- name: InsertPackVersion :exec
-- A version and all its rows in one statement, so it is loaded whole or not at all without a transaction
-- of its own (the unit of work is for tenant work; pack tables are global).
WITH version AS (
  INSERT INTO fyniq.statutory_pack_version (id, pack, title, effective_from, effective_to, supersedes, review_date,
                                            verifier, conditions, tax_point_rule, sources, digest)
  VALUES (sqlc.arg(id), sqlc.arg(pack), sqlc.arg(title), sqlc.arg(effective_from), sqlc.narg(effective_to),
          sqlc.narg(supersedes), sqlc.arg(review_date), sqlc.arg(verifier), sqlc.arg(conditions),
          sqlc.narg(tax_point_rule), sqlc.arg(sources), sqlc.arg(digest))
  RETURNING id
)
INSERT INTO fyniq.statutory_pack_row (version_id, table_name, pinned, row_no, cells)
SELECT version.id, r.table_name, r.pinned, r.row_no, r.cells
  FROM version, jsonb_to_recordset(sqlc.arg(rows)::jsonb) AS r(table_name text, pinned boolean, row_no integer, cells jsonb);

-- name: PackVersions :many
-- Every loaded version, through the services' read-only view (§11.4).
SELECT id, pack, effective_from, effective_to, review_date, digest, loaded_at
  FROM fyniq.statutory_pack_versions ORDER BY id;

-- name: ActivePackVersions :many
-- The organisation's activated versions (ENG-32), under its row-level security.
SELECT version_id, proposed_by, approved_by, approval_id, activated_at
  FROM fyniq.statutory_pack_activation ORDER BY version_id;
