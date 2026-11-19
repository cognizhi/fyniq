-- name: InsertPeopleLinkKey :exec
INSERT INTO fyniq.people_link_key (link_id, nemura_client_id, link_public, client_public)
VALUES (sqlc.arg(link_id), sqlc.arg(nemura_client_id), sqlc.arg(link_public), sqlc.arg(client_public));

-- name: InsertPeopleLinkSecret :exec
INSERT INTO fyniq.people_link_secret (link_id, link_private, client_private)
VALUES (sqlc.arg(link_id), sqlc.arg(link_private), sqlc.arg(client_private));

-- name: PeopleLinkKeys :one
SELECT link_id, nemura_client_id, link_public, client_public, sequence FROM fyniq.people_link_key WHERE link_id = sqlc.arg(link_id);

-- name: NextPeopleLinkSequence :one
UPDATE fyniq.people_link_key SET sequence = sequence + 1 WHERE link_id = sqlc.arg(link_id) RETURNING sequence;

-- name: PeopleLinkSecret :one
SELECT link_id, link_private, client_private FROM fyniq.people_link_secret WHERE link_id = sqlc.arg(link_id);
