-- name: InsertPeopleLinkAskKey :exec
INSERT INTO fyniq.people_link_ask_key (link_id, nemura_ask_client_id, ask_public, ask_private)
VALUES (sqlc.arg(link_id), sqlc.arg(nemura_ask_client_id), sqlc.arg(ask_public), sqlc.arg(ask_private));

-- name: PeopleLinkAskKey :one
SELECT link_id, nemura_ask_client_id, ask_public, ask_private FROM fyniq.people_link_ask_key WHERE link_id = sqlc.arg(link_id);
