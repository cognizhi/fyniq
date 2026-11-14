-- name: InsertAIConversation :one
INSERT INTO fyniq.ai_conversation (user_id, title, created_at)
VALUES (sqlc.arg(user_id), sqlc.arg(title), sqlc.arg(created_at))
RETURNING id;

-- name: GetAIConversation :one
SELECT id, user_id, title, created_at, deleted_at FROM fyniq.ai_conversation WHERE id = sqlc.arg(id);

-- name: AIConversationsOfUser :many
SELECT a.id, a.user_id, a.title, a.created_at, a.deleted_at FROM fyniq.ai_conversation a
WHERE a.user_id = sqlc.arg(user_id) AND a.deleted_at IS NULL
  AND (sqlc.arg(after)::uuid = '00000000-0000-0000-0000-000000000000'::uuid
       OR (a.created_at, a.id) < (SELECT c.created_at, c.id FROM fyniq.ai_conversation c WHERE c.id = sqlc.arg(after)::uuid))
ORDER BY a.created_at DESC, a.id DESC
LIMIT sqlc.arg(max_rows);

-- name: DeleteAIConversation :execrows
UPDATE fyniq.ai_conversation SET deleted_at = sqlc.arg(at) WHERE id = sqlc.arg(id) AND deleted_at IS NULL;

-- name: InsertAITurn :one
INSERT INTO fyniq.ai_turn (conversation_id, user_id, question, context, asked_at)
VALUES (sqlc.arg(conversation_id), sqlc.arg(user_id), sqlc.arg(question), sqlc.narg(context), sqlc.arg(asked_at))
RETURNING id;

-- name: FinishAITurn :execrows
UPDATE fyniq.ai_turn SET status = sqlc.arg(status), code = sqlc.arg(code), answer = sqlc.arg(answer), segments = sqlc.arg(segments),
  cited = sqlc.arg(cited), facts = sqlc.arg(facts), prompt = sqlc.arg(prompt), prompt_sha256 = sqlc.arg(prompt_sha256), runs = sqlc.arg(runs),
  input_tokens = sqlc.arg(input_tokens), output_tokens = sqlc.arg(output_tokens), regenerated = sqlc.arg(regenerated),
  withheld = sqlc.arg(withheld), classes = sqlc.arg(classes), answered_at = sqlc.arg(answered_at)
WHERE id = sqlc.arg(id) AND status = 'answering';

-- name: AITurnsOfConversation :many
SELECT id, conversation_id, user_id, question, context, status, code, answer, segments, cited, facts, prompt, prompt_sha256, runs,
  input_tokens, output_tokens, regenerated, withheld, classes, asked_at, answered_at
FROM fyniq.ai_turn WHERE conversation_id = sqlc.arg(conversation_id) ORDER BY asked_at, id;

-- name: AITurnsPage :many
SELECT id, conversation_id, user_id, question, context, status, code, answer, segments, cited, facts, prompt, prompt_sha256, runs,
  input_tokens, output_tokens, regenerated, withheld, classes, asked_at, answered_at
FROM fyniq.ai_turn
WHERE (sqlc.narg(user_id)::uuid IS NULL OR user_id = sqlc.narg(user_id)::uuid)
  AND asked_at >= sqlc.arg(since) AND asked_at < sqlc.arg(until)
  AND (sqlc.narg(before_at)::timestamptz IS NULL OR (asked_at, id) < (sqlc.narg(before_at)::timestamptz, sqlc.arg(before_id)::uuid))
ORDER BY asked_at DESC, id DESC
LIMIT sqlc.arg(max_rows);

-- name: AIToolCallsOfTurns :many
SELECT id, turn_id, n, tool, version, input, output_sha256, outcome, code, facts, omitted, withheld, duration_ms
FROM fyniq.ai_tool_call WHERE turn_id = ANY(sqlc.arg(turns)::uuid[]) ORDER BY turn_id, n;

-- name: InsertAIToolCall :exec
INSERT INTO fyniq.ai_tool_call (turn_id, n, tool, version, input, output_sha256, outcome, code, facts, omitted, withheld, duration_ms)
VALUES (sqlc.arg(turn_id), sqlc.arg(n), sqlc.arg(tool), sqlc.arg(version), sqlc.arg(input), sqlc.arg(output_sha256), sqlc.arg(outcome),
  sqlc.arg(code), sqlc.arg(facts), sqlc.arg(omitted), sqlc.arg(withheld), sqlc.arg(duration_ms));

-- name: AIToolCallsOfTurn :many
SELECT id, turn_id, n, tool, version, input, output_sha256, outcome, code, facts, omitted, withheld, duration_ms
FROM fyniq.ai_tool_call WHERE turn_id = sqlc.arg(turn_id) ORDER BY n;

-- name: AITurnsAskedSince :one
SELECT count(*)::int AS asked FROM fyniq.ai_turn WHERE user_id = sqlc.arg(user_id) AND asked_at >= sqlc.arg(since);

-- name: InsertAIFeedback :one
INSERT INTO fyniq.ai_feedback (turn_id, user_id, rating, reason, given_at)
VALUES (sqlc.arg(turn_id), sqlc.arg(user_id), sqlc.arg(rating), sqlc.arg(reason), sqlc.arg(given_at))
RETURNING id;
