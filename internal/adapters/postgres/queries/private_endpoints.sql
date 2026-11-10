-- name: PrivateEndpoints :many
SELECT * FROM fyniq.private_endpoints();

-- name: PrivateEndpointsFor :many
SELECT * FROM fyniq.private_endpoints_for(sqlc.arg(service)::text, sqlc.arg(credential)::uuid);

-- name: RegisterPrivateEndpoint :one
SELECT fyniq.register_private_endpoint(sqlc.arg(by_user)::uuid, sqlc.narg(change_id)::uuid, sqlc.arg(id)::uuid,
  sqlc.arg(version)::integer, sqlc.arg(address)::text, sqlc.arg(port)::integer, sqlc.arg(pin)::text, sqlc.arg(service)::text,
  sqlc.arg(serves)::uuid, sqlc.arg(credential)::uuid, sqlc.arg(retired)::boolean, sqlc.arg(reason)::text, sqlc.arg(at)::timestamptz)::integer AS version;
