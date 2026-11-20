-- name: SavePeopleLeave :one
-- A newer (epoch, version) of the same entry replaces the older; an older or equal one changes nothing and returns no row.
INSERT INTO fyniq.people_leave (entry_id, person_id, start_date, end_date, start_half, end_half, days, cancelled, version, epoch, received_at)
VALUES (sqlc.arg(entry_id), sqlc.arg(person_id), sqlc.arg(start_date), sqlc.arg(end_date), sqlc.arg(start_half), sqlc.arg(end_half), sqlc.arg(days), sqlc.arg(cancelled), sqlc.arg(version), sqlc.arg(epoch), sqlc.arg(received_at))
ON CONFLICT (organisation_id, entry_id) DO UPDATE
SET start_date = EXCLUDED.start_date, end_date = EXCLUDED.end_date, start_half = EXCLUDED.start_half, end_half = EXCLUDED.end_half, days = EXCLUDED.days, cancelled = EXCLUDED.cancelled,
    version = EXCLUDED.version, epoch = EXCLUDED.epoch, received_at = EXCLUDED.received_at
WHERE (fyniq.people_leave.epoch, fyniq.people_leave.version) < (EXCLUDED.epoch, EXCLUDED.version) AND fyniq.people_leave.person_id = EXCLUDED.person_id
RETURNING id;

-- name: PeopleLeaveBetween :many
-- The approved leave that overlaps a range, a cancelled entry not among it.
SELECT entry_id, person_id, start_date, end_date, start_half, end_half, days, version, epoch, received_at
FROM fyniq.people_leave WHERE NOT cancelled AND end_date >= sqlc.arg(from_date) AND start_date <= sqlc.arg(to_date)
ORDER BY start_date, entry_id;

-- name: PeopleLeaveOf :one
SELECT entry_id, person_id, start_date, end_date, start_half, end_half, days, cancelled, version, epoch, received_at FROM fyniq.people_leave WHERE entry_id = sqlc.arg(entry_id);
