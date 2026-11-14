package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) AIConversations() storage.AIConversations { return aiConversations{u} }

type aiConversations struct{ u *unitOfWork }

func (r aiConversations) Create(ctx context.Context, c storage.AIConversation) (storage.AIConversation, error) {
	if err := r.u.check(); err != nil {
		return storage.AIConversation{}, err
	}
	id, err := r.u.q.InsertAIConversation(ctx, sqlcdb.InsertAIConversationParams{UserID: c.UserID, Title: c.Title, CreatedAt: c.CreatedAt})
	if err != nil {
		return storage.AIConversation{}, mapError(err)
	}
	c.ID = id
	return c, r.u.event(ctx, "ai_conversation.created", "ai_conversation", id, nil, map[string][2]any{"conversation": {nil, map[string]any{
		"user_id": c.UserID.String(), "title": c.Title}}})
}

func (r aiConversations) Get(ctx context.Context, id uuid.UUID) (storage.AIConversation, error) {
	if err := r.u.check(); err != nil {
		return storage.AIConversation{}, err
	}
	row, err := r.u.q.GetAIConversation(ctx, id)
	if err != nil {
		return storage.AIConversation{}, mapError(err)
	}
	return storage.AIConversation{ID: row.ID, UserID: row.UserID, Title: row.Title, CreatedAt: row.CreatedAt, DeletedAt: row.DeletedAt}, nil
}

func (r aiConversations) OfUser(ctx context.Context, user uuid.UUID, after uuid.UUID, limit int) ([]storage.AIConversation, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.AIConversationsOfUser(ctx, sqlcdb.AIConversationsOfUserParams{UserID: user, After: after,
		MaxRows: int32(min(max(limit, 1), 500))}) // #nosec G115 -- capped
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.AIConversation, 0, len(rows))
	for _, row := range rows {
		out = append(out, storage.AIConversation{ID: row.ID, UserID: row.UserID, Title: row.Title, CreatedAt: row.CreatedAt, DeletedAt: row.DeletedAt})
	}
	return out, nil
}

func (r aiConversations) Delete(ctx context.Context, id uuid.UUID, now time.Time) error {
	if err := r.u.check(); err != nil {
		return err
	}
	n, err := r.u.q.DeleteAIConversation(ctx, sqlcdb.DeleteAIConversationParams{At: &now, ID: id})
	if err != nil {
		return mapError(err)
	}
	if n == 0 {
		return ports.ErrNotFound
	}
	return r.u.event(ctx, "ai_conversation.deleted", "ai_conversation", id, nil, map[string][2]any{"deleted_at": {nil, now.UTC().Format(time.RFC3339)}})
}

func (r aiConversations) Ask(ctx context.Context, t storage.AITurn) (storage.AITurn, error) {
	if err := r.u.check(); err != nil {
		return storage.AITurn{}, err
	}
	id, err := r.u.q.InsertAITurn(ctx, sqlcdb.InsertAITurnParams{ConversationID: t.Conversation, UserID: t.UserID, Question: t.Question,
		Context: []byte(t.Context), AskedAt: t.AskedAt})
	if err != nil {
		return storage.AITurn{}, mapError(err)
	}
	t.ID, t.Status = id, storage.TurnAnswering
	return t, nil
}

func (r aiConversations) Finish(ctx context.Context, t storage.AITurn, calls []storage.AIToolCall) error {
	if err := r.u.check(); err != nil {
		return err
	}
	for i, c := range calls {
		input := []byte(c.Input)
		if !json.Valid(input) {
			input = []byte(`{}`)
		}
		if err := r.u.q.InsertAIToolCall(ctx, sqlcdb.InsertAIToolCallParams{TurnID: t.ID, N: int32(min(i+1, 100)), Tool: c.Tool, // #nosec G115 -- capped
			Version: int32(min(max(c.Version, 0), 1<<30)), Input: input, OutputSha256: c.OutputSHA256, Outcome: c.Outcome, Code: c.Code, // #nosec G115 -- capped
			Facts: int32(min(max(c.Facts, 0), 1<<30)), Omitted: int32(min(max(c.Omitted, 0), 1<<30)), Withheld: c.Withheld, // #nosec G115 -- capped
			DurationMs: int32(min(max(c.DurationMS, 0), 1<<30))}); err != nil { // #nosec G115 -- capped
			return mapError(err)
		}
	}
	or := func(b json.RawMessage, empty string) []byte {
		if len(b) == 0 {
			return []byte(empty)
		}
		return b
	}
	cited := t.Cited
	if cited == nil {
		cited = []string{}
	}
	runs := t.Runs
	if runs == nil {
		runs = []uuid.UUID{}
	}
	n, err := r.u.q.FinishAITurn(ctx, sqlcdb.FinishAITurnParams{Status: t.Status, Code: t.Code, Answer: t.Answer, Segments: or(t.Segments, "[]"),
		Cited: cited, Facts: or(t.Facts, "[]"), Prompt: t.Prompt, PromptSha256: t.PromptSHA256, Runs: runs, InputTokens: t.InputTokens,
		OutputTokens: t.OutputTokens, Regenerated: t.Regenerated, Withheld: int32(min(max(t.Withheld, 0), 1<<30)), // #nosec G115 -- capped
		AnsweredAt: t.AnsweredAt, ID: t.ID})
	if err != nil {
		return mapError(err)
	}
	if n == 0 {
		return ports.ErrNotFound
	}
	// The record names how the turn ended and what it used, never the question's or the answer's words (AUD-01, SEC-15).
	return r.u.event(ctx, "ai_turn."+t.Status, "ai_turn", t.ID, nil, map[string][2]any{"turn": {nil, map[string]any{
		"conversation_id": t.Conversation.String(), "status": t.Status, "code": t.Code, "prompt": t.Prompt, "prompt_sha256": t.PromptSHA256,
		"cited": len(cited), "tool_calls": len(calls), "regenerated": t.Regenerated, "withheld": t.Withheld}}})
}

func (r aiConversations) Turns(ctx context.Context, conversation uuid.UUID) ([]storage.AITurn, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.AITurnsOfConversation(ctx, conversation)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.AITurn, 0, len(rows))
	for _, row := range rows {
		out = append(out, storage.AITurn{ID: row.ID, Conversation: row.ConversationID, UserID: row.UserID, Question: row.Question,
			Context: row.Context, Status: row.Status, Code: row.Code, Answer: row.Answer, Segments: row.Segments, Cited: row.Cited, Facts: row.Facts,
			Prompt: row.Prompt, PromptSHA256: row.PromptSha256, Runs: row.Runs, InputTokens: row.InputTokens, OutputTokens: row.OutputTokens,
			Regenerated: row.Regenerated, Withheld: int(row.Withheld), AskedAt: row.AskedAt, AnsweredAt: row.AnsweredAt})
	}
	return out, nil
}

func (r aiConversations) ToolCalls(ctx context.Context, turn uuid.UUID) ([]storage.AIToolCall, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.AIToolCallsOfTurn(ctx, turn)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.AIToolCall, 0, len(rows))
	for _, row := range rows {
		out = append(out, storage.AIToolCall{ID: row.ID, Turn: row.TurnID, N: int(row.N), Tool: row.Tool, Version: int(row.Version), Input: row.Input,
			OutputSHA256: row.OutputSha256, Outcome: row.Outcome, Code: row.Code, Facts: int(row.Facts), Omitted: int(row.Omitted),
			Withheld: row.Withheld, DurationMS: int(row.DurationMs)})
	}
	return out, nil
}

func (r aiConversations) Asked(ctx context.Context, user uuid.UUID, since time.Time) (int, error) {
	if err := r.u.check(); err != nil {
		return 0, err
	}
	n, err := r.u.q.AITurnsAskedSince(ctx, sqlcdb.AITurnsAskedSinceParams{UserID: user, Since: since})
	return int(n), mapError(err)
}

func (r aiConversations) Give(ctx context.Context, f storage.AIFeedback) (storage.AIFeedback, error) {
	if err := r.u.check(); err != nil {
		return storage.AIFeedback{}, err
	}
	id, err := r.u.q.InsertAIFeedback(ctx, sqlcdb.InsertAIFeedbackParams{TurnID: f.Turn, UserID: f.UserID, Rating: f.Rating, Reason: f.Reason,
		GivenAt: f.GivenAt})
	if err != nil {
		return storage.AIFeedback{}, mapError(err)
	}
	f.ID = id
	return f, r.u.event(ctx, "ai_feedback.given", "ai_feedback", id, nil, map[string][2]any{"feedback": {nil, map[string]any{
		"turn_id": f.Turn.String(), "rating": f.Rating}}})
}
