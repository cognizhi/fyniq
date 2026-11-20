-- name: InsertAIProposalUndo :one
INSERT INTO fyniq.ai_proposal_undo (proposal_id, undone_by, undone_at, reason, reversed)
VALUES (sqlc.arg(proposal_id), sqlc.arg(undone_by), sqlc.arg(undone_at), sqlc.arg(reason), sqlc.arg(reversed))
RETURNING id, proposal_id, undone_by, undone_at, reason, reversed;

-- name: AIProposalUndoOf :many
SELECT id, proposal_id, undone_by, undone_at, reason, reversed FROM fyniq.ai_proposal_undo WHERE proposal_id = sqlc.arg(proposal_id);

-- name: InsertAIProposalView :exec
INSERT INTO fyniq.ai_proposal_view (proposal_id, user_id, first_viewed_at) VALUES (sqlc.arg(proposal_id), sqlc.arg(user_id), sqlc.arg(first_viewed_at))
ON CONFLICT DO NOTHING;

-- name: AIQualityRows :many
SELECT p.id, p.task_type, p.status, p.proposed_at, p.decided_by, p.decided_at, p.approved_sha256, p.payload_sha256, p.reason, v.first_viewed_at, un.undone_at
FROM fyniq.ai_proposal p
LEFT JOIN fyniq.ai_proposal_view v ON v.organisation_id = p.organisation_id AND v.proposal_id = p.id AND v.user_id = p.decided_by
LEFT JOIN fyniq.ai_proposal_undo un ON un.organisation_id = p.organisation_id AND un.proposal_id = p.id
WHERE p.proposed_at >= sqlc.arg(from_at) AND p.proposed_at < sqlc.arg(to_at)
ORDER BY p.id;

-- name: InsertAIQualityReview :one
INSERT INTO fyniq.ai_quality_review (month, signed_by, signed_at, report, note)
VALUES (sqlc.arg(month), sqlc.arg(signed_by), sqlc.arg(signed_at), sqlc.arg(report), sqlc.narg(note))
RETURNING id, month, signed_by, signed_at, report, note;

-- name: AIQualityReviewOf :many
SELECT id, month, signed_by, signed_at, report, note FROM fyniq.ai_quality_review WHERE month = sqlc.arg(month);
