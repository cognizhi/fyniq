package storage

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// CodeAIConversation is the database's refusal of a change to the assistant's conversations (FY341): a conversation
// changed or deleted twice, a turn answered twice, a tool call changed, feedback reviewed twice, or one removed but by
// the purge (p1-ai-assistant task 6.1).
const CodeAIConversation = "STORAGE-AI-CONVERSATION-01"

// The states of a turn.
const (
	TurnAnswering = "answering"
	TurnAnswered  = "answered"
	TurnRefused   = "refused"
	TurnFailed    = "failed"
)

// AIConversation is a person's conversation with the assistant, theirs alone (AI-P8).
type AIConversation struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	Title     string
	CreatedAt time.Time
	DeletedAt *time.Time
}

// AITurn is a question asked in a conversation and its answer (design D11): what was asked and about what, how it
// ended, the answer's words and segments, the facts it read and cites, the prompt it was told, the model calls' runs,
// its tokens and what grounding did.
type AITurn struct {
	ID           uuid.UUID
	Conversation uuid.UUID
	UserID       uuid.UUID
	Question     string
	Context      json.RawMessage // what the person asked about (FR-UX-17); nil for nothing
	Status       string
	Code         string
	Answer       string
	Segments     json.RawMessage
	Cited        []string
	Facts        json.RawMessage
	Prompt       string
	PromptSHA256 string
	Runs         []uuid.UUID
	InputTokens  int64
	OutputTokens int64
	Regenerated  bool
	Withheld     int
	AskedAt      time.Time
	AnsweredAt   *time.Time
}

// AIToolCall is a read a turn made, as it ran: the tool and its version, the input, a hash of what the model read, the
// outcome and its counts.
type AIToolCall struct {
	ID           uuid.UUID
	Turn         uuid.UUID
	N            int
	Tool         string
	Version      int
	Input        json.RawMessage
	OutputSHA256 string
	Outcome      string
	Code         string
	Facts        int
	Omitted      int
	Withheld     bool
	DurationMS   int
}

// AIFeedback is a person's thumbs up or down on a turn's answer, with a reason, and its review (QA-AI-05).
type AIFeedback struct {
	ID         uuid.UUID
	Turn       uuid.UUID
	UserID     uuid.UUID
	Rating     string
	Reason     string
	GivenAt    time.Time
	ReviewedBy *uuid.UUID
	ReviewedAt *time.Time
	Decision   string
}

// AIConversations keeps the assistant's conversations, their turns, the turns' tool calls and feedback.
type AIConversations interface {
	// Create keeps a new conversation, audited.
	Create(ctx context.Context, c AIConversation) (AIConversation, error)
	// Get is a conversation, deleted or not; ports.ErrNotFound when there is none.
	Get(ctx context.Context, id uuid.UUID) (AIConversation, error)
	// OfUser are a person's conversations not deleted, newest first, after the cursor's conversation (uuid.Nil: from the
	// newest), at most limit.
	OfUser(ctx context.Context, user uuid.UUID, after uuid.UUID, limit int) ([]AIConversation, error)
	// Delete marks a conversation deleted at now, once, audited; ports.ErrNotFound when it is not there or already deleted.
	Delete(ctx context.Context, id uuid.UUID, now time.Time) error
	// Ask keeps a turn as it is asked: answering.
	Ask(ctx context.Context, t AITurn) (AITurn, error)
	// Finish keeps a turn's end, once, audited, with the tool calls it made; ports.ErrNotFound when it is not answering.
	Finish(ctx context.Context, t AITurn, calls []AIToolCall) error
	// Turns are a conversation's turns, in the order they were asked.
	Turns(ctx context.Context, conversation uuid.UUID) ([]AITurn, error)
	// ToolCalls are a turn's tool calls, in order.
	ToolCalls(ctx context.Context, turn uuid.UUID) ([]AIToolCall, error)
	// Asked counts the turns a person asked since a time, whatever their end (design D9).
	Asked(ctx context.Context, user uuid.UUID, since time.Time) (int, error)
	// Give keeps a person's feedback on a turn, once, audited.
	Give(ctx context.Context, f AIFeedback) (AIFeedback, error)
}
