-- name: BrandVersions :many
SELECT * FROM fyniq.brand_version ORDER BY version DESC;

-- name: InsertBrandVersion :one
INSERT INTO fyniq.brand_version (version, light_type, light_sha256, light_size, dark_type, dark_sha256, dark_size, icon_type, icon_sha256,
  icon_size, print_sha256, print_size, accent, cleaned, created_by, created_at)
VALUES (sqlc.arg(version), sqlc.narg(light_type), sqlc.narg(light_sha256), sqlc.narg(light_size), sqlc.narg(dark_type), sqlc.narg(dark_sha256),
  sqlc.narg(dark_size), sqlc.narg(icon_type), sqlc.narg(icon_sha256), sqlc.narg(icon_size), sqlc.narg(print_sha256), sqlc.narg(print_size),
  sqlc.narg(accent), sqlc.arg(cleaned),
  sqlc.arg(created_by), sqlc.arg(created_at))
RETURNING *;
