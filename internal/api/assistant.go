package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/sse"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ai"
	"github.com/cognizhi/fyniq/internal/aitool"
	"github.com/cognizhi/fyniq/internal/assistant"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/airoute"
	"github.com/cognizhi/fyniq/internal/egress"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The assistant's conversations (PRD-001 AI-P1, AI-P8, AI-12, FR-UX-17; p1-ai-assistant task 6.1, design D1, D11): a
// person's own conversations and their turns, asked through the API. A turn is admitted here (the AI switch, then the
// limits of task 5.3), kept as it is asked, handed to the AI service with a delegated read token for it alone, and its
// answer relayed as server-sent events and kept with the reads it made; the token ends with the turn. The operations
// are on the never-list for MCP and agent clients (§9.6): only a person signed in to the web app asks.

// AssistantService is how the API reaches the AI service (design D3): its URL, on internal ingress only, and the API's
// own token for it, of the class assistant.Class.
type AssistantService struct {
	URL   string
	Token func(ctx context.Context) (string, error)
	HTTP  *http.Client // through the egress guard; egress.Public when nil
}

// The codes of the assistant's refusals.
const (
	CodeAIPersonOnly        = "AI-ASSISTANT-01" // only a person signed in asks the assistant
	CodeAIService           = "AI-ASSISTANT-02" // the AI service is not reachable, or did not finish
	CodeAIPerMinute         = "AI-LIMIT-01"     // more questions a minute than ai.questions_per_minute
	CodeAIPerDay            = "AI-LIMIT-02"     // more questions in 24 hours than ai.questions_per_day
	CodeAIConversationLimit = "AI-LIMIT-03"     // the conversation has ai.conversation_turns questions
)

// The settings of the assistant's limits (AI-23, design D9).
const (
	settingAIPerMinute    = "ai.questions_per_minute"
	settingAIPerDay       = "ai.questions_per_day"
	settingAIReads        = "ai.reads_per_question"
	settingAIConversation = "ai.conversation_turns"
	verbatimTurns         = 20 // the earlier turns a turn carries whole; older ones are summarised, their facts kept
	maxEarlierFacts       = 200
)

// AIConversationIn starts a conversation.
type AIConversationIn struct {
	Title string `json:"title,omitempty" maxLength:"200" doc:"Its title; left out, it is untitled until its first question names it." example:"Cash runway"`
}

// AIConversation is a person's conversation with the assistant.
type AIConversation struct {
	ID        string   `json:"id" format:"uuid" example:"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2b"`
	Title     string   `json:"title" example:"Cash runway"`
	CreatedAt string   `json:"created_at" format:"date-time" example:"2026-11-15T08:00:00Z"`
	Turns     []AITurn `json:"turns,omitempty" doc:"Its turns, in the order they were asked; on getAIConversation."`
}

// AIConversationPage is a page of a person's conversations, newest first.
type AIConversationPage struct {
	Items      []AIConversation `json:"items" nullable:"false"`
	NextCursor string           `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2b"`
}

// AITurnContext is what a person asks about (FR-UX-17): a record, a report or a figure.
type AITurnContext struct {
	Kind  string `json:"kind" enum:"invoice,credit_note,bill,vendor_credit,journal,expense_claim,quote,sales_order,purchase_order,contact,account,item,report,figure" doc:"What it is." example:"invoice"`
	ID    string `json:"id,omitempty" maxLength:"60" doc:"A record's ID, or a figure's fact ID." example:"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2b"`
	Label string `json:"label" minLength:"1" maxLength:"200" doc:"How the screen names it." example:"INV-0012, Acme Pte Ltd"`
}

// AITurnIn is a question asked in a conversation.
type AITurnIn struct {
	Question string         `json:"question" minLength:"1" maxLength:"4000" doc:"The person's question." example:"What is our cash runway?"`
	Context  *AITurnContext `json:"context,omitempty" doc:"What the person asks about, from the screen they ask on."`
}

// AISegment is a piece of an answer: words, a figure as Fyniq rendered it from its fact, or what could not be
// established (design D7).
type AISegment struct {
	Text    string `json:"text,omitempty" doc:"Words, in Fyniq's Markdown subset." example:"Cash lasts "`
	Fact    string `json:"fact,omitempty" doc:"A figure's fact, among the turn's cited facts." example:"F3a9c0d1e2b4f5a6"`
	Figure  string `json:"figure,omitempty" doc:"The figure as Fyniq renders it from its fact." example:"18.0 months"`
	Missing string `json:"missing,omitempty" doc:"What could not be established." example:"this figure"`
}

// AITurn is a turn as its person reads it: the question and what it was about, how it ended, the answer in its words
// and segments, and the facts it cites with their sources.
type AITurn struct {
	ID          string         `json:"id" format:"uuid" example:"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2c"`
	Question    string         `json:"question" example:"What is our cash runway?"`
	Context     *AITurnContext `json:"context,omitempty"`
	Status      string         `json:"status" enum:"answering,answered,refused,failed" doc:"answering until it ends; refused with the code of what stopped it; failed when the AI service did not finish." example:"answered"`
	Code        string         `json:"code,omitempty" doc:"What stopped it." example:"AI-ROUTE-05"`
	Answer      string         `json:"answer,omitempty" doc:"The answer as the person reads it, in Fyniq's Markdown subset." example:"Cash lasts 18.0 months."`
	Segments    []AISegment    `json:"segments" nullable:"false"`
	Cited       []AIFact       `json:"cited" nullable:"false" doc:"The facts the answer cites, each with its source and lineage (AI-68)."`
	Regenerated bool           `json:"regenerated" doc:"true when the answer failed the figures' check once and was asked for again (AI-08)." example:"false"`
	Withheld    int            `json:"withheld" doc:"Figures or words withheld because they still failed the check." example:"0"`
	AskedAt     string         `json:"asked_at" format:"date-time" example:"2026-11-15T08:00:00Z"`
	AnsweredAt  string         `json:"answered_at,omitempty" format:"date-time" example:"2026-11-15T08:00:04Z"`
	Feedback    *AITurnRating  `json:"feedback,omitempty" doc:"Your rating of its answer, once given (QA-AI-05)."`
}

// AITurnRating is a person's rating of a turn's answer, as they read their conversation.
type AITurnRating struct {
	ID     string `json:"id" format:"uuid" example:"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a51"`
	Rating string `json:"rating" enum:"up,down" example:"down"`
	Reason string `json:"reason" example:"It used June's figures, not July's."`
	State  string `json:"state" enum:"waiting,golden,dismissed" doc:"Where its review is." example:"waiting"`
}

// AIFact is a fact an answer cites (AI-68), as the person's screen shows it with its source and lineage.
type AIFact struct {
	ID          string     `json:"id" example:"F3a9c0d1e2b4f5a6"`
	Measure     string     `json:"measure" example:"revenue"`
	Name        string     `json:"name,omitempty" example:"Revenue"`
	Value       string     `json:"value,omitempty" doc:"Exact decimal text; absent when the fact has no value." example:"125000.00"`
	Figure      string     `json:"figure" doc:"The value as the answer shows it, rounded once by RND-DISPLAY." example:"SGD 125,000.00"`
	Unit        string     `json:"unit" example:"amount"`
	Currency    string     `json:"currency,omitempty" example:"SGD"`
	From        string     `json:"from,omitempty" format:"date" example:"2026-07-01"`
	To          string     `json:"to,omitempty" format:"date" example:"2026-09-30"`
	Member      string     `json:"member,omitempty" example:"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2b"`
	MemberName  string     `json:"member_name,omitempty" example:"Acme Pte Ltd"`
	Revision    int64      `json:"ledger_revision" example:"1204"`
	Source      string     `json:"source" example:"the profit and loss's revenue"`
	Lineage     string     `json:"lineage" doc:"The API path it was read from: what Explain opens." example:"/v1/reports/statements/pl?from=2026-07&to=2026-09&as_at_revision=1204"`
	Inputs      []AIFactID `json:"inputs,omitempty" doc:"A calculated fact's input facts."`
	Pack        string     `json:"pack,omitempty" example:"sg-gst"`
	PackVersion string     `json:"pack_version,omitempty" example:"sg-gst@2026.3"`
}

// AIFactID names a fact.
type AIFactID string

// Schema implements huma.SchemaProvider.
func (AIFactID) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Description: "A fact's ID.", Examples: []any{"F3a9c0d1e2b4f5a6"}}
}

func citedFact(f aitool.Fact) AIFact {
	out := AIFact{ID: f.ID, Measure: f.Measure, Name: f.Name, Value: f.Value, Figure: assistant.Figure(f), Unit: f.Unit, Currency: f.Currency,
		From: f.From, To: f.To, Member: f.Member, MemberName: f.MemberName, Revision: f.Revision, Source: f.Source, Lineage: f.Lineage,
		Pack: f.Pack, PackVersion: f.PackVersion}
	for _, id := range f.Inputs {
		out.Inputs = append(out.Inputs, AIFactID(id))
	}
	return out
}

// AITurnStatus is what the assistant is doing while it answers.
type AITurnStatus struct {
	Text string `json:"text" example:"Reading the books"`
	Tool string `json:"tool,omitempty" doc:"The read tool it ran." example:"metrics.query"`
}

// AIConversationPath names one of the caller's conversations.
type AIConversationPath struct {
	ConversationID string `path:"conversation_id" format:"uuid" doc:"The conversation." example:"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2b"`
}

type (
	aiConversationCreateInput struct {
		KeyHeader
		PreferHeader
		Body AIConversationIn
	}
	aiConversationListInput struct{ PageQuery }
	aiConversationOutput    struct {
		Status   int
		Location string `header:"Location" doc:"The conversation's URL."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     AIConversation
	}
	aiConversationReadOutput struct{ Body AIConversation }
	aiConversationListOutput struct{ Body AIConversationPage }
	aiTurnInput              struct {
		AIConversationPath
		Body AITurnIn
	}
)

func conversationOf(c storage.AIConversation) AIConversation {
	return AIConversation{ID: c.ID.String(), Title: c.Title, CreatedAt: c.CreatedAt.UTC().Format(time.RFC3339)}
}

// turnOf is a turn as its person reads it: its figures rendered from its facts.
func turnOf(t storage.AITurn) AITurn {
	out := AITurn{ID: t.ID.String(), Question: t.Question, Status: t.Status, Code: t.Code, Answer: t.Answer, Segments: []AISegment{},
		Cited: []AIFact{}, Regenerated: t.Regenerated, Withheld: t.Withheld, AskedAt: t.AskedAt.UTC().Format(time.RFC3339)}
	if t.AnsweredAt != nil {
		out.AnsweredAt = t.AnsweredAt.UTC().Format(time.RFC3339)
	}
	if len(t.Context) > 0 {
		var c AITurnContext
		if json.Unmarshal(t.Context, &c) == nil {
			out.Context = &c
		}
	}
	var facts []aitool.Fact
	_ = json.Unmarshal(t.Facts, &facts)
	byID := map[string]aitool.Fact{}
	for _, f := range facts {
		byID[f.ID] = f
	}
	for _, id := range t.Cited {
		if f, ok := byID[id]; ok {
			out.Cited = append(out.Cited, citedFact(f))
		}
	}
	var segments []assistant.Segment
	_ = json.Unmarshal(t.Segments, &segments)
	for _, s := range segments {
		g := AISegment{Text: assistant.Markdown(s.Text), Fact: s.Fact, Missing: s.Missing}
		if f, ok := byID[s.Fact]; ok {
			g.Figure = assistant.Figure(f)
		}
		out.Segments = append(out.Segments, g)
	}
	return out
}

// ownConversation is the caller's conversation, not deleted; another's, or a deleted one, is not found.
func ownConversation(ctx context.Context, u storage.UnitOfWork, p authn.Principal, raw string) (storage.AIConversation, error) {
	id, err := parseID("conversation_id", raw)
	if err != nil {
		return storage.AIConversation{}, err
	}
	c, err := u.AIConversations().Get(ctx, id)
	if err != nil {
		return storage.AIConversation{}, err
	}
	if c.UserID != p.UserID || c.DeletedAt != nil {
		return storage.AIConversation{}, ports.ErrNotFound
	}
	return c, nil
}

// personOnly refuses anyone but a person signed in: the assistant's conversations are on the never-list for MCP and
// agent clients (§9.6).
func personOnly(p authn.Principal) error {
	if p.Class != authn.ClassUser || p.UserID == uuid.Nil {
		return problem(CodeAIPersonOnly, "Only a person signed in to Fyniq asks the assistant.", "Ask in the web app")
	}
	return nil
}

func registerAssistant(a *API) {
	register(a, authz.CreateAIConversation, huma.Operation{
		OperationID: "createAIConversation", Method: http.MethodPost, Path: "/v1/assistant/conversations", Tags: []string{"Assistant"},
		DefaultStatus: http.StatusCreated, Summary: "Start a conversation with the assistant",
		Description: "Starts a conversation of the caller's own with the assistant (AI-P8): only they read it. Side effects: the conversation " +
			"and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing. On the never-list for " +
			"MCP and agent clients (§9.6).",
	}, []string{CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeAIPersonOnly},
		func(ctx context.Context, in *aiConversationCreateInput) (*aiConversationOutput, error) {
			p, _ := authn.From(ctx)
			if err := personOnly(p); err != nil {
				return nil, err
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (AIConversation, error) {
				c, err := u.AIConversations().Create(ctx, storage.AIConversation{UserID: p.UserID, Title: strings.TrimSpace(in.Body.Title),
					CreatedAt: a.deps.Clock.Now()})
				return conversationOf(c), err
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &aiConversationOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &aiConversationOutput{Status: w.Status, Location: "/v1/assistant/conversations/" + w.Body.ID, Replayed: replayedHeader(w.Replayed),
				Body: w.Body}, nil
		})

	register(a, authz.ListAIConversations, huma.Operation{
		OperationID: "listAIConversations", Method: http.MethodGet, Path: "/v1/assistant/conversations", Tags: []string{"Assistant"},
		Summary:     "List your conversations with the assistant",
		Description: "The caller's own conversations with the assistant, newest first, without their turns; a deleted one is left out (AI-P8). Read-only; no side effects.",
	}, []string{CodeAIPersonOnly}, func(ctx context.Context, in *aiConversationListInput) (*aiConversationListOutput, error) {
		p, _ := authn.From(ctx)
		if err := personOnly(p); err != nil {
			return nil, err
		}
		size := in.PageSize
		if size <= 0 {
			size = 100
		}
		after := uuid.Nil
		if in.Cursor != "" {
			var err error
			if after, err = uuid.Parse(in.Cursor); err != nil {
				return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "cursor", Message: "is not a cursor this list gave", Fix: "Send next_cursor as it came"}})
			}
		}
		out := AIConversationPage{Items: []AIConversation{}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			list, err := u.AIConversations().OfUser(ctx, p.UserID, after, size+1)
			if err != nil {
				return err
			}
			for i, c := range list {
				if i == size {
					out.NextCursor = list[i-1].ID.String()
					break
				}
				out.Items = append(out.Items, conversationOf(c))
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		return &aiConversationListOutput{Body: out}, nil
	})

	register(a, authz.GetAIConversation, huma.Operation{
		OperationID: "getAIConversation", Method: http.MethodGet, Path: "/v1/assistant/conversations/{conversation_id}", Tags: []string{"Assistant"},
		Summary:     "Get a conversation with the assistant",
		Description: "One of the caller's own conversations with its turns: each question, how it ended, the answer with its figures rendered from their facts, and the facts it cites (AI-08, AI-68). Another's, or a deleted one, is not found. Read-only; no side effects.",
	}, []string{CodeNotFound, CodeAIPersonOnly}, func(ctx context.Context, in *AIConversationPath) (*aiConversationReadOutput, error) {
		p, _ := authn.From(ctx)
		if err := personOnly(p); err != nil {
			return nil, err
		}
		var out AIConversation
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			c, err := ownConversation(ctx, u, p, in.ConversationID)
			if err != nil {
				return err
			}
			turns, err := u.AIConversations().Turns(ctx, c.ID)
			if err != nil {
				return err
			}
			rated, err := u.AIConversations().Ratings(ctx, c.ID, p.UserID)
			if err != nil {
				return err
			}
			out = conversationOf(c)
			out.Turns = make([]AITurn, 0, len(turns))
			for _, t := range turns {
				turn := turnOf(t)
				if f, ok := rated[t.ID]; ok {
					state := storage.FeedbackWaiting
					if f.Decision != "" {
						state = f.Decision
					}
					turn.Feedback = &AITurnRating{ID: f.ID.String(), Rating: f.Rating, Reason: f.Reason, State: state}
				}
				out.Turns = append(out.Turns, turn)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		return &aiConversationReadOutput{Body: out}, nil
	})

	register(a, authz.DeleteAIConversation, exempt(exempt(huma.Operation{
		OperationID: "deleteAIConversation", Method: http.MethodDelete, Path: "/v1/assistant/conversations/{conversation_id}", Tags: []string{"Assistant"},
		DefaultStatus: http.StatusNoContent, Summary: "Delete a conversation with the assistant",
		Description: "Deletes one of the caller's own conversations: it is no longer listed or read, and the purge removes it (AI-P8). Side effects: the conversation is marked deleted, and its audit event.",
	}, "Prefer", "Deleting your own conversation is done at once; there is nothing to preview."),
		"If-Match", "A conversation has no version: deleting your own is done once, and the database refuses a second (FY341)."),
		[]string{CodeNotFound, CodeAIPersonOnly}, func(ctx context.Context, in *AIConversationPath) (*struct{}, error) {
			p, _ := authn.From(ctx)
			if err := personOnly(p); err != nil {
				return nil, err
			}
			err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				c, err := ownConversation(ctx, u, p, in.ConversationID)
				if err != nil {
					return err
				}
				return u.AIConversations().Delete(ctx, c.ID, a.deps.Clock.Now())
			})
			return nil, err
		})

	ask := a.prepare(authz.AskAIConversation, exempt(exempt(exempt(huma.Operation{
		OperationID: "askAIConversation", Method: http.MethodPost, Path: "/v1/assistant/conversations/{conversation_id}/turns", Tags: []string{"Assistant"},
		Summary: "Ask the assistant",
		Description: "Asks a question in one of the caller's own conversations, with what it is about (FR-UX-17), and streams the answer as " +
			"server-sent events: status while it reads the books, then answer with the turn as kept, or error with what stopped it. The " +
			"assistant reads as the caller, through the read tools, with a token for this turn alone (AI-01); every figure in the answer " +
			"is rendered from a fact it read (AI-08). Side effects: the turn, its reads and its audit event; the model calls in the AI " +
			"activity log. A refusal after the stream began, such as the AI switched off or a conversation not found, is its error event.",
	}, "Idempotency-Key", "Each question is a turn of its own: asked again, it is answered again."),
		"Prefer", "An answer cannot be previewed without asking."),
		"If-Match", "Asking adds a turn and leaves the conversation as it was: there is no version of it to match."),
		[]string{CodeNotFound, CodeAIPersonOnly, CodeAIService, airoute.CodeSwitchOff, CodeAIPerMinute, CodeAIPerDay, CodeAIConversationLimit})
	sse.Register(a.API, ask, map[string]any{"status": AITurnStatus{}, "answer": AITurn{}, "error": Problem{}},
		func(ctx context.Context, in *aiTurnInput, send sse.Sender) {
			a.askTurn(ctx, in, send)
		})
	// The stream's events, by example: a status while it reads, then the answer (NFR-API-01).
	if item := a.OpenAPI().Paths[ask.Path]; item != nil && item.Post != nil {
		if mt := item.Post.Responses["200"].Content["text/event-stream"]; mt != nil && mt.Schema != nil && mt.Schema.Items != nil {
			mt.Schema.Items.Examples = []any{map[string]any{"event": "status", "data": map[string]any{"text": "Reading the books"}}}
		}
	}
}

// askTurn admits a turn, hands it to the AI service and relays its answer.
func (a *API) askTurn(ctx context.Context, in *aiTurnInput, send sse.Sender) {
	fail := func(err error) {
		pr := problemFrom(ctx, err)
		if pr.Instance == "" {
			pr.Instance = instance(ctx)
		}
		_ = send.Data(*pr)
	}
	p, _ := authn.From(ctx)
	if err := personOnly(p); err != nil {
		fail(err)
		return
	}
	var turn storage.AITurn
	var token string
	var history []ports.ModelMessage
	var cited []aitool.Fact
	var limits aiLimits
	err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
		c, err := ownConversation(ctx, u, p, in.ConversationID)
		if err != nil {
			return err
		}
		today, err := orgToday(ctx, a, u)
		if err != nil {
			return err
		}
		if on, err := settingOn(ctx, u, ai.Switch, today); err != nil {
			return err
		} else if on != "true" {
			return problem(airoute.CodeSwitchOff, "The AI is switched off for the organisation.", "Ask an Owner to switch it on in Settings → AI")
		}
		earlier, err := u.AIConversations().Turns(ctx, c.ID)
		if err != nil {
			return err
		}
		if limits, err = a.admit(ctx, u, p.UserID, len(earlier), today); err != nil {
			return err
		}
		history, cited = historyOf(earlier)
		var about json.RawMessage
		if in.Body.Context != nil {
			about, _ = json.Marshal(in.Body.Context)
		}
		if turn, err = u.AIConversations().Ask(ctx, storage.AITurn{Conversation: c.ID, UserID: p.UserID, Question: strings.TrimSpace(in.Body.Question),
			Context: about, AskedAt: a.deps.Clock.Now()}); err != nil {
			return err
		}
		token, err = a.issueAIGrant(ctx, u, p.Organisation, p.UserID, turn.ID)
		return err
	})
	if err != nil {
		fail(err)
		return
	}
	// The turn is kept and its token ended whatever happens to the stream from here.
	keep := context.WithoutCancel(ctx)
	answer, err := a.askService(ctx, assistant.Question{Token: token, Text: turn.Question, Context: contextWords(in.Body.Context), History: history,
		Earlier: cited, MaxReads: limits.reads}, send)
	finished := a.deps.Clock.Now()
	turn.AnsweredAt = &finished
	var calls []storage.AIToolCall
	var pr *Problem
	switch {
	case err == nil:
		turn.Status = storage.TurnAnswered
		turn.Answer, turn.Prompt, turn.PromptSHA256 = answer.Text, answer.Prompt, answer.PromptSHA256
		turn.Segments, _ = json.Marshal(answer.Segments)
		turn.Facts, _ = json.Marshal(answer.Facts)
		turn.Cited, turn.Regenerated, turn.Withheld = answer.Cited, answer.Grounding.Regenerated, answer.Grounding.Withheld
		turn.InputTokens, turn.OutputTokens = answer.Usage.Input, answer.Usage.Output
		for _, r := range answer.Runs {
			if id, err := uuid.Parse(r); err == nil {
				turn.Runs = append(turn.Runs, id)
			}
		}
		for _, c := range answer.ToolCalls {
			calls = append(calls, storage.AIToolCall{Tool: c.Tool, Version: c.Version, Input: c.Input, OutputSHA256: c.OutputSHA256, Outcome: c.Outcome,
				Code: c.Code, Facts: c.Facts, Omitted: c.Omitted, Withheld: c.Withheld, DurationMS: int(c.Duration / time.Millisecond)})
		}
	case errors.As(err, &pr):
		turn.Status, turn.Code = storage.TurnRefused, pr.Code
	default:
		logFailure(ctx, err)
		turn.Status, turn.Code = storage.TurnFailed, CodeAIService
	}
	err = a.deps.Store.Do(keep, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
		// The classes the turn read, as its grant has them now: the activity log shows them and masks payroll (AI-12, AI-83).
		if classes, err := u.AIGrants().Classes(ctx, turn.ID, a.deps.Clock.Now()); err == nil {
			turn.Classes = classes
		} else if !errors.Is(err, ports.ErrNotFound) {
			return err
		}
		if err := u.AIConversations().Finish(ctx, turn, calls); err != nil {
			return err
		}
		if err := a.endAIGrant(ctx, u, turn.ID); err != nil && !errors.Is(err, ports.ErrNotFound) {
			return err
		}
		return nil
	})
	switch {
	case err != nil:
		fail(err)
	case turn.Status == storage.TurnAnswered:
		_ = send.Data(turnOf(turn))
	case pr != nil:
		fail(pr)
	default:
		fail(problem(CodeAIService, "The assistant did not finish the answer.", "Ask again in a moment"))
	}
}

// aiLimits are the limits a turn runs under.
type aiLimits struct{ reads int }

// admit refuses a turn past the person's limits or the conversation's (AI-23, design D9): questions a minute and in the
// last 24 hours, and questions in one conversation; a refused turn is not kept. It gives the reads a turn may make.
func (a *API) admit(ctx context.Context, u storage.UnitOfWork, user uuid.UUID, turns int, today time.Time) (aiLimits, error) {
	value := func(key string) (int, error) {
		v, err := settingOn(ctx, u, key, today)
		if err != nil {
			return 0, err
		}
		return strconv.Atoi(v)
	}
	perMinute, err := value(settingAIPerMinute)
	if err != nil {
		return aiLimits{}, err
	}
	perDay, err := value(settingAIPerDay)
	if err != nil {
		return aiLimits{}, err
	}
	most, err := value(settingAIConversation)
	if err != nil {
		return aiLimits{}, err
	}
	reads, err := value(settingAIReads)
	if err != nil {
		return aiLimits{}, err
	}
	now := a.deps.Clock.Now()
	if n, err := u.AIConversations().Asked(ctx, user, now.Add(-time.Minute)); err != nil {
		return aiLimits{}, err
	} else if n >= perMinute {
		return aiLimits{}, problem(CodeAIPerMinute, fmt.Sprintf("You asked %d questions in the last minute, the most the organisation allows.", n),
			"Wait a minute and ask again")
	}
	if n, err := u.AIConversations().Asked(ctx, user, now.Add(-24*time.Hour)); err != nil {
		return aiLimits{}, err
	} else if n >= perDay {
		return aiLimits{}, problem(CodeAIPerDay, fmt.Sprintf("You asked %d questions in the last 24 hours, the most the organisation allows.", n),
			"Ask again later, or ask an Owner to raise ai.questions_per_day")
	}
	if turns >= most {
		return aiLimits{}, problem(CodeAIConversationLimit, fmt.Sprintf("This conversation has %d questions, the most one may hold.", turns),
			"Start a new conversation")
	}
	return aiLimits{reads: reads}, nil
}

// historyOf is a conversation's earlier answered turns as the model reads them, and the facts they cited, which the
// turn may cite again (AI-08). The last 20 come whole, each question and its answer; older ones are summarised in one
// message, with the facts they cited kept by ID (design D9).
func historyOf(turns []storage.AITurn) ([]ports.ModelMessage, []aitool.Fact) {
	var answered []storage.AITurn
	for _, t := range turns {
		if t.Status == storage.TurnAnswered {
			answered = append(answered, t)
		}
	}
	var out []ports.ModelMessage
	var cited []aitool.Fact
	seen := map[string]bool{}
	keep := func(t storage.AITurn) []string {
		var facts []aitool.Fact
		_ = json.Unmarshal(t.Facts, &facts)
		byID := map[string]aitool.Fact{}
		for _, f := range facts {
			byID[f.ID] = f
		}
		var ids []string
		for _, id := range t.Cited {
			if f, ok := byID[id]; ok && !seen[id] && len(cited) < maxEarlierFacts {
				seen[id], cited = true, append(cited, f)
			}
			ids = append(ids, id)
		}
		return ids
	}
	split := max(len(answered)-verbatimTurns, 0)
	if split > 0 {
		var b strings.Builder
		b.WriteString("Earlier in this conversation, summarised (the facts they cited are still yours to cite by ID):")
		for _, t := range answered[:split] {
			ids := keep(t)
			b.WriteString("\n- Asked: " + t.Question)
			if len(ids) > 0 {
				b.WriteString(" Answered citing " + strings.Join(ids, ", ") + ".")
			}
		}
		out = append(out, ports.ModelMessage{Role: ports.RoleUser, Parts: []ports.ModelPart{{Kind: ports.PartText, Text: b.String()}}},
			ports.ModelMessage{Role: ports.RoleAssistant, Parts: []ports.ModelPart{{Kind: ports.PartText, Text: "Noted."}}})
	}
	for _, t := range answered[split:] {
		keep(t)
		out = append(out, ports.ModelMessage{Role: ports.RoleUser, Parts: []ports.ModelPart{{Kind: ports.PartText, Text: t.Question}}},
			ports.ModelMessage{Role: ports.RoleAssistant, Parts: []ports.ModelPart{{Kind: ports.PartText, Text: t.Answer}}})
	}
	return out, cited
}

// contextWords is what a person asks about, as the model reads it.
func contextWords(c *AITurnContext) string {
	if c == nil {
		return ""
	}
	words := "the " + strings.ReplaceAll(c.Kind, "_", " ") + " " + c.Label
	if c.ID != "" {
		words += " (" + c.ID + ")"
	}
	return words
}

// askService asks the AI service to answer q, passing its status on as it goes, and returns its answer; the AI service's
// refusal in the API's words is a *Problem.
func (a *API) askService(ctx context.Context, q assistant.Question, send sse.Sender) (assistant.Answer, error) {
	s := a.deps.Assistant
	if s == nil || s.URL == "" {
		return assistant.Answer{}, problem(CodeAIService, "The AI service is not set up.", "Ask the platform's operator to set up the AI service")
	}
	token, err := s.Token(ctx)
	if err != nil {
		return assistant.Answer{}, fmt.Errorf("a token for the AI service: %w", err)
	}
	body, err := json.Marshal(q)
	if err != nil {
		return assistant.Answer{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(s.URL, "/")+"/turns", bytes.NewReader(body))
	if err != nil {
		return assistant.Answer{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	h := s.HTTP
	if h == nil {
		h = egress.Public()
	}
	resp, err := h.Do(req)
	if err != nil {
		return assistant.Answer{}, fmt.Errorf("the AI service did not answer: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return assistant.Answer{}, fmt.Errorf("the AI service answered %d", resp.StatusCode)
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64<<10), 16<<20)
	var event string
	var data bytes.Buffer
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		case line == "":
			switch event {
			case assistant.EventStatus, assistant.EventTool:
				var e assistant.Event
				if json.Unmarshal(data.Bytes(), &e) == nil {
					text := e.Text
					if e.Kind == assistant.EventTool {
						text = "Read the books"
					}
					_ = send.Data(AITurnStatus{Text: text, Tool: e.Tool})
				}
			case assistant.EventDone:
				var e assistant.Event
				if err := json.Unmarshal(data.Bytes(), &e); err != nil || e.Answer == nil {
					return assistant.Answer{}, fmt.Errorf("the AI service's answer could not be read: %v", err)
				}
				return *e.Answer, nil
			case "error":
				var p assistant.Problem
				if json.Unmarshal(data.Bytes(), &p) == nil && p.Code != "" {
					return assistant.Answer{}, problem(p.Code, p.Detail, p.Fix)
				}
				return assistant.Answer{}, errors.New("the AI service stopped with an error it did not name")
			}
			event = ""
			data.Reset()
		}
	}
	if err := sc.Err(); err != nil {
		return assistant.Answer{}, fmt.Errorf("the AI service's stream: %w", err)
	}
	return assistant.Answer{}, errors.New("the AI service's stream ended without an answer")
}
