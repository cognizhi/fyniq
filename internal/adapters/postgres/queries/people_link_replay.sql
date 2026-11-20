-- name: InsertPeopleLinkReplay :one
INSERT INTO fyniq.people_link_replay (link_id, from_sequence, to_sequence, asked_at)
VALUES (sqlc.arg(link_id), sqlc.arg(from_sequence), sqlc.arg(to_sequence), sqlc.arg(asked_at))
RETURNING id;

-- name: PeopleLinkReplaysSince :many
SELECT from_sequence, to_sequence FROM fyniq.people_link_replay WHERE link_id = sqlc.arg(link_id) AND asked_at > sqlc.arg(since) ORDER BY asked_at, id;
